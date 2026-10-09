package pdf

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
	"gorm.io/gorm"

	"kupol/internal/documents"
	"kupol/internal/i18n"
	"kupol/internal/uploads"
)

// KindPDFRender — тип джобы очереди: печать одного документа глазами одного читателя.
const KindPDFRender = "pdf_render"

// Args — аргументы печати. Читатель зашит в джобу: документ читается заново в момент сборки,
// уже по этому допуску — так закрытые блоки не утекают, а чужой уровень не подставишь.
type Args struct {
	Ref         string // что печатаем: шифр или slug
	UserID      int64  // кто запросил печать (владелец джобы)
	UserLevel   int
	Directorate bool
	Lang        string // язык подписей листа
	SiteOrigin  string // origin сайта — для ссылок на другие документы
}

// Kind — тип джобы для River.
func (Args) Kind() string { return KindPDFRender }

// JobState — состояние печати для API: queued (ждёт или идёт повтор) → running → ready | failed.
type JobState string

const (
	StateQueued  JobState = "queued"
	StateRunning JobState = "running"
	StateReady   JobState = "ready"
	StateFailed  JobState = "failed"
)

// Status — состояние джобы для читателя.
type Status struct {
	ID     int64    `json:"id"`
	State  JobState `json:"state"`
	Ref    string   `json:"ref"`
	UserID int64    `json:"-"` // владелец: чужим — 404, чтобы не перебирать чужие идентификаторы
}

// ErrJobNotFound — джобы нет (или её строка уже вычищена ретеншеном River).
var ErrJobNotFound = errors.New("задача печати не найдена")

// QueueConfig — что нужно очереди.
type QueueConfig struct {
	DB         *gorm.DB
	Dir        string // где лежат готовые PDF; пусто — ./pdfs
	Typst      string // путь к typst; пусто — ищется в PATH (KUPOL_TYPST имеет приоритет в Runner)
	Documents  *documents.Service
	Uploads    *uploads.Service
	SiteOrigin string
	Log        *slog.Logger
}

// Queue — очередь печати на River (этап 6.2): POST создаёт джобу, worker собирает PDF typst'ом,
// готовый файл лежит в Dir по номеру джобы. Состояние читается из River — своя таблица не нужна.
type Queue struct {
	client *river.Client[*sql.Tx]
	dir    string
	docs   *documents.Service
	ups    *uploads.Service
	origin string
	log    *slog.Logger

	mu     sync.Mutex
	runner *Runner
}

// NewQueue поднимает очередь: миграции River (идемпотентно), воркер печати, ретеншен как у PDF — неделя.
// typst может отсутствовать: очередь поднимется, а джобы будут падать с ErrNoTypst — так обновление
// можно раскатывать до установки typst (см. docs/deploy.md).
func NewQueue(ctx context.Context, cfg QueueConfig) (*Queue, error) {
	if cfg.DB == nil || cfg.Documents == nil || cfg.Uploads == nil {
		return nil, errors.New("pdf: очередь нуждается в DB, Documents и Uploads")
	}
	dir := cfg.Dir
	if dir == "" {
		dir = "pdfs"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("pdf: каталог %s: %w", dir, err)
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	sqlDB, err := cfg.DB.DB()
	if err != nil {
		return nil, fmt.Errorf("pdf: %w", err)
	}
	driver := riverdatabasesql.New(sqlDB)
	migrator, err := rivermigrate.New(driver, nil)
	if err != nil {
		return nil, fmt.Errorf("pdf: rivermigrate: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return nil, fmt.Errorf("pdf: миграции River: %w", err)
	}

	q := &Queue{dir: dir, docs: cfg.Documents, ups: cfg.Uploads, origin: cfg.SiteOrigin, log: log}
	if cfg.Typst != "" || os.Getenv("KUPOL_TYPST") != "" {
		bin := cfg.Typst
		if bin == "" {
			bin = os.Getenv("KUPOL_TYPST")
		}
		if r, err := NewRunner(bin); err == nil {
			q.runner = r
		} else {
			log.Warn("typst не найден — печать будет падать", "ошибка", err)
		}
	} else if r, err := DefaultRunner(); err == nil {
		q.runner = r
	} else {
		log.Warn("typst не найден — печать будет падать", "ошибка", err)
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, river.WorkFunc[Args](q.work))
	client, err := river.NewClient[*sql.Tx](driver, &river.Config{
		Queues:                      map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 2}},
		Workers:                     workers,
		CompletedJobRetentionPeriod: 7 * 24 * time.Hour,
		DiscardedJobRetentionPeriod: 7 * 24 * time.Hour,
		CancelledJobRetentionPeriod: 7 * 24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("pdf: river client: %w", err)
	}
	q.client = client
	q.sweep()
	return q, nil
}

// Start запускает разбор очереди (в фоне; ждать нечего — River сам ходит за джобами).
func (q *Queue) Start(ctx context.Context) error {
	return q.client.Start(ctx)
}

// Stop гасит очередь мягко: текущие сборки доедут, новые разбираться не будут.
func (q *Queue) Stop(ctx context.Context) error {
	return q.client.Stop(ctx)
}

// Enqueue ставит печать документа в очередь и возвращает номер джобы.
func (q *Queue) Enqueue(ctx context.Context, args Args) (int64, error) {
	res, err := q.client.Insert(ctx, args, &river.InsertOpts{MaxAttempts: 2})
	if err != nil {
		return 0, fmt.Errorf("pdf: постановка в очередь: %w", err)
	}
	return res.Job.ID, nil
}

// Status — состояние печати по номеру джобы.
func (q *Queue) Status(ctx context.Context, id int64) (*Status, error) {
	row, err := q.client.JobGet(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %d", ErrJobNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("pdf: состояние джобы: %w", err)
	}
	var args Args
	_ = json.Unmarshal(row.EncodedArgs, &args)
	st := StateQueued
	switch row.State {
	case rivertype.JobStateRunning:
		st = StateRunning
	case rivertype.JobStateCompleted:
		st = StateReady
	case rivertype.JobStateDiscarded, rivertype.JobStateCancelled:
		st = StateFailed
	}
	return &Status{ID: id, State: st, Ref: args.Ref, UserID: args.UserID}, nil
}

// File — путь к файлу печати; существование файла проверяет вызывающий (готов или ещё нет).
func (q *Queue) File(id int64) string {
	return filepath.Join(q.dir, strconv.FormatInt(id, 10)+".pdf")
}

// work — сборка одного листа: документ читается заново глазами читателя из аргументов джобы.
func (q *Queue) work(ctx context.Context, job *river.Job[Args]) error {
	a := job.Args
	v := documents.Viewer{
		UserID: a.UserID, UserLevel: a.UserLevel, Directorate: a.Directorate,
		Lang: i18n.Lang(a.Lang),
	}
	doc, err := q.docs.Get(ctx, v, a.Ref)
	if err != nil {
		return fmt.Errorf("pdf: чтение %q: %w", a.Ref, err)
	}
	runner, err := q.getRunner()
	if err != nil {
		return err
	}
	open := func(key string) (string, error) {
		path, _, err := q.ups.Open(ctx, v.Level(), key, false)
		return path, err
	}
	data, err := runner.Render(ctx, doc, Options{
		Lang: v.Lang, SiteOrigin: a.SiteOrigin, ViewerLevel: v.Level(), OpenImage: open,
	})
	if err != nil {
		return fmt.Errorf("pdf: сборка %q: %w", a.Ref, err)
	}
	// готовый файл — атомарно: читатель не должен увидеть недописанный PDF
	tmp, err := os.CreateTemp(q.dir, ".part-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	final := filepath.Join(q.dir, strconv.FormatInt(job.ID, 10)+".pdf")
	if err := os.Rename(tmp.Name(), final); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	q.log.Info("документ напечатан", "джоба", job.ID, "шифр", a.Ref, "байт", len(data))
	return nil
}

// getRunner — компилятор typst: если его не было при старте (установили позже), подхватим при первой печати.
func (q *Queue) getRunner() (*Runner, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.runner != nil {
		return q.runner, nil
	}
	bin := os.Getenv("KUPOL_TYPST")
	if bin == "" {
		bin = "typst"
	}
	r, err := NewRunner(bin)
	if err != nil {
		return nil, err
	}
	q.runner = r
	return r, nil
}

// sweep убирает осиротевшие PDF старше недели (строки джоб River вычищает сам тем же ретеншеном).
func (q *Queue) sweep() {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return
	}
	deadline := time.Now().Add(-7 * 24 * time.Hour)
	for _, e := range entries {
		if e.IsDir() || e.Name() == "" || e.Name()[0] == '.' {
			continue
		}
		info, err := e.Info()
		if err == nil && info.ModTime().Before(deadline) {
			os.Remove(filepath.Join(q.dir, e.Name()))
		}
	}
}

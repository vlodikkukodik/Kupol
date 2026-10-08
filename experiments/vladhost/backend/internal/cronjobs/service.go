package cronjobs

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"vladhost/internal/apperr"
	"vladhost/internal/sites"
)

// Виды задач.
const (
	KindHTTP    = "http"
	KindCommand = "command"
)

// Состояния запуска.
const (
	RunRunning = "running"
	RunOK      = "ok"
	RunFailed  = "failed"
	RunTimeout = "timeout"
	RunSkipped = "skipped"
)

// FailNotifyStreak — сколько неудач подряд считается поломкой, о которой пишем на почту.
const FailNotifyStreak = 3

var (
	ErrNotFound    = apperr.New(http.StatusNotFound, "cron_not_found", "cron job not found")
	ErrLimit       = apperr.New(http.StatusForbidden, "cron_limit", "cron job limit reached")
	ErrName        = apperr.Validation("name", "cron_name", "invalid job name")
	ErrSchedule    = apperr.Validation("schedule", "cron_schedule", "invalid schedule")
	ErrInterval    = apperr.Validation("schedule", "cron_interval", "schedule is too frequent")
	ErrURL         = apperr.Validation("url", "cron_url", "invalid url")
	ErrCommand     = apperr.Validation("command", "cron_command", "invalid command")
	ErrSite        = apperr.Validation("site_id", "cron_site", "site is required")
	ErrKind        = apperr.Validation("kind", "cron_kind", "invalid job kind")
	ErrCommandsOff = apperr.New(http.StatusConflict, "cron_commands_off", "commands are not available")
	ErrCooldown    = apperr.New(http.StatusTooManyRequests, "cron_run_cooldown", "run too often")
	ErrBusy        = apperr.New(http.StatusConflict, "cron_busy", "job is already running")
)

// Job — задача.
type Job struct {
	ID           int64      `gorm:"primaryKey" json:"id"`
	UserID       int64      `json:"-"`
	Name         string     `json:"name"`
	Kind         string     `json:"kind"`
	Schedule     string     `json:"schedule"`
	URL          string     `json:"url"`
	Command      string     `json:"command"`
	SiteID       *int64     `json:"site_id"`
	Enabled      bool       `json:"enabled"`
	NextRunAt    *time.Time `json:"next_run_at"`
	LastRunAt    *time.Time `json:"last_run_at"`
	LastStatus   string     `json:"last_status"`
	FailStreak   int        `json:"fail_streak"`
	FailingSince *time.Time `json:"failing_since"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (Job) TableName() string { return "cron_jobs" }

// Run — один запуск задачи.
type Run struct {
	ID         int64      `gorm:"primaryKey" json:"id"`
	JobID      int64      `json:"job_id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Status     string     `json:"status"`
	Code       int        `json:"code"`
	DurationMS int        `json:"duration_ms"`
	Reason     string     `json:"reason"` // код системной причины (перевод — в интерфейсе)
	Output     string     `json:"output"`
}

func (Run) TableName() string { return "cron_runs" }

// Input — поля задачи, которые задаёт пользователь.
type Input struct {
	Name     string
	Kind     string
	Schedule string
	URL      string
	Command  string
	SiteID   int64
	Enabled  bool
}

// Config — настройки планировщика.
type Config struct {
	MaxJobs       int           // задач на аккаунт
	MinIntervalMi int           // минимальный промежуток между запусками, минуты
	Timeout       time.Duration // предел выполнения одного запуска
	KeepRuns      int           // сколько запусков хранить на задачу
	Concurrency   int           // сколько запусков одновременно на весь сервер
	AllowPrivate  bool          // только для тестов: разрешить HTTP во внутреннюю сеть
	Commands      CmdRunner     // nil — команды выключены
}

// Info — сведения для интерфейса.
type Info struct {
	MaxJobs         int  `json:"max_jobs"`
	MinIntervalMin  int  `json:"min_interval_min"`
	TimeoutSec      int  `json:"timeout_sec"`
	KeepRuns        int  `json:"keep_runs"`
	CommandsEnabled bool `json:"commands_enabled"`
}

// Service — планировщик.
type Service struct {
	db    *gorm.DB
	sites *sites.Service
	cfg   Config
	http  *http.Client
	now   func() time.Time
	sem   chan struct{}
	wg    sync.WaitGroup
}

// New создаёт планировщик; нулевые значения настроек заменяются умолчаниями (5 задач, раз в 5 минут, 60 секунд, 50 запусков).
func New(db *gorm.DB, siteSvc *sites.Service, cfg Config) *Service {
	if cfg.MaxJobs <= 0 {
		cfg.MaxJobs = 5
	}
	if cfg.MinIntervalMi <= 0 {
		cfg.MinIntervalMi = 5
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.KeepRuns <= 0 {
		cfg.KeepRuns = 50
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	return &Service{db: db, sites: siteSvc, cfg: cfg, http: newHTTPClient(cfg.Timeout, cfg.AllowPrivate), now: time.Now, sem: make(chan struct{}, cfg.Concurrency)}
}

// Info возвращает лимиты.
func (s *Service) Info() Info {
	return Info{MaxJobs: s.cfg.MaxJobs, MinIntervalMin: s.cfg.MinIntervalMi, TimeoutSec: int(s.cfg.Timeout.Seconds()), KeepRuns: s.cfg.KeepRuns, CommandsEnabled: s.cfg.Commands != nil}
}

// Wait ждёт завершения запущенных запусков (для тестов и остановки).
func (s *Service) Wait() { s.wg.Wait() }

func (s *Service) validate(ctx context.Context, userID int64, in *Input) (cron.Schedule, error) {
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 60 || strings.ContainsAny(in.Name, "\n\r\x00") {
		return nil, ErrName
	}
	sch, err := ParseSchedule(in.Schedule, s.cfg.MinIntervalMi)
	switch {
	case errors.Is(err, errInterval):
		return nil, ErrInterval.With(s.cfg.MinIntervalMi)
	case err != nil:
		return nil, ErrSchedule
	}
	in.Schedule = strings.Join(strings.Fields(in.Schedule), " ")
	switch in.Kind {
	case KindHTTP:
		if _, err := checkURL(in.URL, s.cfg.AllowPrivate); err != nil {
			return nil, ErrURL
		}
		in.URL = strings.TrimSpace(in.URL)
		in.Command, in.SiteID = "", 0
	case KindCommand:
		if s.cfg.Commands == nil {
			return nil, ErrCommandsOff
		}
		in.Command = strings.TrimSpace(in.Command)
		if in.Command == "" || len(in.Command) > 2000 || !utf8.ValidString(in.Command) || strings.ContainsRune(in.Command, 0) {
			return nil, ErrCommand
		}
		if in.SiteID <= 0 {
			return nil, ErrSite
		}
		if _, err := s.sites.Get(ctx, userID, in.SiteID); err != nil {
			return nil, ErrSite
		}
		in.URL = ""
	default:
		return nil, ErrKind
	}
	return sch, nil
}

func nextAfter(sch cron.Schedule, t time.Time) *time.Time {
	n := sch.Next(t.UTC())
	if n.IsZero() {
		return nil
	}
	return &n
}

// Create заводит задачу. Лимит проверяется под блокировкой строки пользователя: параллельные запросы его не обойдут.
func (s *Service) Create(ctx context.Context, userID int64, in Input) (*Job, error) {
	sch, err := s.validate(ctx, userID, &in)
	if err != nil {
		return nil, err
	}
	now := s.now()
	job := &Job{UserID: userID, Name: in.Name, Kind: in.Kind, Schedule: in.Schedule, URL: in.URL, Command: in.Command,
		Enabled: in.Enabled, CreatedAt: now}
	if in.SiteID > 0 {
		job.SiteID = &in.SiteID
	}
	if in.Enabled {
		job.NextRunAt = nextAfter(sch, now)
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw("SELECT id FROM users WHERE id = ? FOR UPDATE", userID).Scan(new(int64)).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&Job{}).Where("user_id = ?", userID).Count(&n).Error; err != nil {
			return err
		}
		if int(n) >= s.cfg.MaxJobs {
			return ErrLimit.With(s.cfg.MaxJobs)
		}
		return tx.Create(job).Error
	})
	if err != nil {
		return nil, err
	}
	return job, nil
}

// Update заменяет поля задачи и пересчитывает следующий запуск.
func (s *Service) Update(ctx context.Context, userID, id int64, in Input) (*Job, error) {
	job, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	sch, err := s.validate(ctx, userID, &in)
	if err != nil {
		return nil, err
	}
	up := map[string]any{"name": in.Name, "kind": in.Kind, "schedule": in.Schedule, "url": in.URL, "command": in.Command,
		"enabled": in.Enabled, "next_run_at": nil}
	if in.SiteID > 0 {
		up["site_id"] = in.SiteID
	} else {
		up["site_id"] = nil
	}
	if in.Enabled {
		up["next_run_at"] = nextAfter(sch, s.now())
	}
	if err := s.db.WithContext(ctx).Model(&Job{}).Where("id = ?", job.ID).Updates(up).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, userID, id)
}

// Get возвращает задачу пользователя.
func (s *Service) Get(ctx context.Context, userID, id int64) (*Job, error) {
	var j Job
	err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&j).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &j, err
}

// List возвращает задачи пользователя.
func (s *Service) List(ctx context.Context, userID int64) ([]Job, error) {
	var out []Job
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("id").Find(&out).Error
	return out, err
}

// Delete удаляет задачу вместе с журналом.
func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	res := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&Job{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Runs возвращает последние запуски задачи, новые первыми.
func (s *Service) Runs(ctx context.Context, userID, id int64) ([]Run, error) {
	if _, err := s.Get(ctx, userID, id); err != nil {
		return nil, err
	}
	var out []Run
	err := s.db.WithContext(ctx).Where("job_id = ?", id).Order("id DESC").Limit(s.cfg.KeepRuns).Find(&out).Error
	return out, err
}

// RunNow запускает задачу сейчас, не чаще раза в минуту. Возвращает запись запуска (он ещё идёт).
func (s *Service) RunNow(ctx context.Context, userID, id int64) (*Run, error) {
	job, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if s.running(ctx, job.ID) {
		return nil, ErrBusy
	}
	if job.LastRunAt != nil && s.now().Sub(*job.LastRunAt) < time.Minute {
		return nil, ErrCooldown
	}
	run, err := s.start(ctx, job)
	if err != nil {
		return nil, err
	}
	return run, nil
}

// running: идёт ли запуск этой задачи. Запуск старше предела с запасом считается брошенным (панель перезапускали).
func (s *Service) running(ctx context.Context, jobID int64) bool {
	var n int64
	s.db.WithContext(ctx).Model(&Run{}).
		Where("job_id = ? AND status = ? AND started_at > ?", jobID, RunRunning, s.now().Add(-s.cfg.Timeout-2*time.Minute)).Count(&n)
	return n > 0
}

// start записывает запуск и отдаёт выполнение в фон. Отметка последнего запуска ставится сразу: по ней работает пауза RunNow.
func (s *Service) start(ctx context.Context, job *Job) (*Run, error) {
	run := &Run{JobID: job.ID, StartedAt: s.now(), Status: RunRunning}
	if err := s.db.WithContext(ctx).Create(run).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&Job{}).Where("id = ?", job.ID).Update("last_run_at", run.StartedAt).Error; err != nil {
		return nil, err
	}
	s.wg.Go(func() {
		s.sem <- struct{}{}
		defer func() { <-s.sem }()
		s.execute(context.Background(), job, run)
	})
	return run, nil
}

func clean(out string) string {
	out = strings.ToValidUTF8(strings.ReplaceAll(out, "\x00", ""), "�")
	if len(out) > maxOutput {
		out = strings.ToValidUTF8(out[:maxOutput], "") + "\n…"
	}
	return out
}

// execute выполняет запуск и записывает итог в журнал и в задачу.
func (s *Service) execute(ctx context.Context, job *Job, run *Run) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout+50*time.Second)
	defer cancel()
	begin := time.Now()
	status, code, out, reason := RunFailed, 0, "", ""
	switch job.Kind {
	case KindHTTP:
		hctx, hcancel := context.WithTimeout(ctx, s.cfg.Timeout)
		ok, c, o, r := runHTTP(hctx, s.http, job.URL, s.cfg.AllowPrivate)
		reason = r
		expired := hctx.Err() != nil
		hcancel()
		code, out = c, o
		if ok {
			status = RunOK
		} else if expired {
			status = RunTimeout
		}
	case KindCommand:
		out, code, status, reason = s.execCommand(ctx, job, run.ID)
	}
	fin := s.now()
	dur := int(time.Since(begin).Milliseconds())
	if err := s.db.Model(&Run{}).Where("id = ?", run.ID).Updates(map[string]any{
		"finished_at": fin, "status": status, "code": code, "duration_ms": dur, "reason": reason, "output": clean(out),
	}).Error; err != nil {
		log.Printf("cron: запись запуска %d: %v", run.ID, err)
	}
	s.finishJob(job.ID, status == RunOK, status, fin)
	s.prune(job.ID)
}

func (s *Service) execCommand(ctx context.Context, job *Job, runID int64) (out string, code int, status, reason string) {
	if s.cfg.Commands == nil || job.SiteID == nil {
		return "", 0, RunFailed, "commands_off"
	}
	var site sites.Site
	if err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", *job.SiteID, job.UserID).First(&site).Error; err != nil {
		return "", 0, RunFailed, "site_missing"
	}
	res, err := s.cfg.Commands.Run(ctx, runID, site.Host, job.Command, s.cfg.Timeout)
	switch {
	case err != nil:
		return err.Error(), 0, RunFailed, "runner_failed"
	case res.TimedOut:
		return res.Output, res.Exit, RunTimeout, ""
	case res.Exit != 0:
		return res.Output, res.Exit, RunFailed, ""
	}
	return res.Output, 0, RunOK, ""
}

// finishJob обновляет итог задачи: последний статус и серию неудач (по ней уходит письмо).
func (s *Service) finishJob(id int64, ok bool, status string, at time.Time) {
	up := map[string]any{"last_status": status}
	if ok {
		up["fail_streak"] = 0
		up["failing_since"] = nil
	} else {
		up["fail_streak"] = gorm.Expr("fail_streak + 1")
		up["failing_since"] = gorm.Expr("COALESCE(failing_since, ?)", at)
	}
	if err := s.db.Model(&Job{}).Where("id = ?", id).Updates(up).Error; err != nil {
		log.Printf("cron: итог задачи %d: %v", id, err)
	}
}

// prune оставляет KeepRuns последних запусков.
func (s *Service) prune(jobID int64) {
	err := s.db.Exec(`DELETE FROM cron_runs WHERE job_id = ? AND id NOT IN
		(SELECT id FROM cron_runs WHERE job_id = ? ORDER BY id DESC LIMIT ?)`, jobID, jobID, s.cfg.KeepRuns).Error
	if err != nil {
		log.Printf("cron: очистка журнала задачи %d: %v", jobID, err)
	}
}

// Tick запускает задачи, срок которых пришёл. Каждая задача захватывается одним обновлением next_run_at, поэтому даже
// при двух копиях панели запуск один. Пропущенные за время простоя запуски не накапливаются: задача идёт один раз.
func (s *Service) Tick(ctx context.Context) error {
	now := s.now()
	var due []Job
	if err := s.db.WithContext(ctx).Where("enabled AND next_run_at IS NOT NULL AND next_run_at <= ?", now).Find(&due).Error; err != nil {
		return err
	}
	for i := range due {
		job := &due[i]
		sch, err := parser.Parse(job.Schedule)
		if err != nil {
			continue
		}
		res := s.db.WithContext(ctx).Model(&Job{}).Where("id = ? AND next_run_at = ?", job.ID, job.NextRunAt).
			Update("next_run_at", nextAfter(sch, now))
		if res.Error != nil || res.RowsAffected != 1 {
			continue
		}
		if s.running(ctx, job.ID) {
			s.db.WithContext(ctx).Create(&Run{JobID: job.ID, StartedAt: now, FinishedAt: &now, Status: RunSkipped,
				Reason: "overlap"})
			continue
		}
		if _, err := s.start(ctx, job); err != nil {
			log.Printf("cron: запуск задачи %d: %v", job.ID, err)
		}
	}
	return nil
}

// Cleanup помечает брошенные запуски (панель перезапустили посреди выполнения) как неудачные.
func (s *Service) Cleanup(ctx context.Context) error {
	return s.db.WithContext(ctx).Model(&Run{}).
		Where("status = ? AND started_at < ?", RunRunning, s.now().Add(-s.cfg.Timeout-2*time.Minute)).
		Updates(map[string]any{"status": RunFailed, "finished_at": s.now(), "reason": "aborted"}).Error
}

// Watch раз в every секунд запускает наступившие задачи, пока жив ctx.
func (s *Service) Watch(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.Cleanup(ctx); err != nil && ctx.Err() == nil {
			log.Printf("cron: очистка: %v", err)
		}
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			log.Printf("cron: планировщик: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

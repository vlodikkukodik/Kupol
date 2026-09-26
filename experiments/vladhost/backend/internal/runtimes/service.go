// Package runtimes — среды выполнения сайтов: PHP (несколько версий), Node.js и Python. Панель хранит выбор (таблица site_runtimes),
// пишет runtime.json для веб-шлюза и просит исполнитель от root (deploy/bin/runtime.sh) создать пользователя сайта, пул PHP-FPM или
// службу приложения в песочнице systemd.
package runtimes

import (
	"context"
	"errors"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vladhost/internal/apperr"
	"vladhost/internal/runtimecfg"
	"vladhost/internal/sites"
)

var (
	ErrRuntime        = apperr.Validation("runtime", "rt_runtime", "unknown runtime")
	ErrRuntimeOff     = apperr.Validation("runtime", "rt_not_installed", "runtime is not installed on the server")
	ErrVersion        = apperr.Validation("version", "rt_version", "unsupported PHP version")
	ErrCommand        = apperr.Validation("command", "rt_command", "invalid start command")
	ErrPortsBusy      = apperr.New(http.StatusServiceUnavailable, "rt_no_ports", "no free application ports")
	ErrHelperDown     = apperr.New(http.StatusServiceUnavailable, "rt_helper_down", "runtime helper is not available")
	ErrApplyFailed    = apperr.New(http.StatusBadGateway, "rt_apply_failed", "runtime helper refused the request")
	ErrNotApp         = apperr.New(http.StatusConflict, "rt_not_app", "site does not run an application")
	ErrRestartTooSoon = apperr.New(http.StatusTooManyRequests, "rt_restart_cooldown", "restart too often")
)

// Row — выбор среды сайта.
type Row struct {
	SiteID    int64 `gorm:"primaryKey"`
	Runtime   string
	Version   string
	Command   string
	Port      *int
	UpdatedAt time.Time
}

func (Row) TableName() string { return "site_runtimes" }

// Caps — что установлено на сервере (файл caps, который пишет prepare-runtime.sh).
type Caps struct {
	PHP    []string `json:"php"`    // установленные версии PHP-FPM
	Node   string   `json:"node"`   // версия Node.js; пусто — не установлен
	Python string   `json:"python"` // версия Python; пусто — не установлен
}

// Available: включён ли хоть один вид среды.
func (c Caps) Available() bool { return len(c.PHP) > 0 || c.Node != "" || c.Python != "" }

// View — среда сайта для интерфейса.
type View struct {
	Runtime string `json:"runtime"`
	Version string `json:"version"`
	Command string `json:"command"`
	Port    int    `json:"port"`
	State   string `json:"state"` // active, failed, inactive, unknown; у статики пусто
	Caps    Caps   `json:"caps"`
}

// Input — выбор пользователя.
type Input struct {
	Runtime string
	Version string
	Command string
}

// Service — управление средами.
type Service struct {
	db      *gorm.DB
	sites   *sites.Service
	applier Applier
	dir     string // папка обмена с исполнителем; в ней же файл caps
	locks   sync.Map
	now     func() time.Time

	// shellOn говорит, включён ли у сайта доступ к оболочке: у такого сайта тоже есть файлы, созданные от имени пользователя сайта.
	shellOn func(ctx context.Context, siteID int64) bool

	restartMu sync.Mutex
	restarted map[int64]time.Time
}

// New создаёт службу. dir — папка обмена с исполнителем (queue/, results/, caps).
func New(db *gorm.DB, siteSvc *sites.Service, applier Applier, dir string) *Service {
	return &Service{db: db, sites: siteSvc, applier: applier, dir: dir, now: time.Now, restarted: map[int64]time.Time{}}
}

// SetShellCheck подключает проверку «у сайта включён доступ к оболочке» для исправления прав.
func (s *Service) SetShellCheck(f func(ctx context.Context, siteID int64) bool) { s.shellOn = f }

// Enabled сообщает, есть ли на сервере хотя бы одна среда.
func (s *Service) Enabled() bool { return s != nil && s.applier != nil && s.Caps().Available() }

// Caps читает файл caps заново при каждом обращении: администратор может доустановить среду без перезапуска панели.
func (s *Service) Caps() Caps {
	data, err := os.ReadFile(filepath.Join(s.dir, "caps"))
	if err != nil {
		return Caps{PHP: []string{}}
	}
	c := Caps{PHP: []string{}}
	for line := range strings.SplitSeq(string(data), "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch k {
		case "php":
			for ver := range strings.SplitSeq(v, ",") {
				ver = strings.TrimSpace(ver)
				if runtimecfg.ValidVersion(ver) {
					c.PHP = append(c.PHP, ver)
				}
			}
		case "node":
			c.Node = strings.TrimSpace(v)
		case "python":
			c.Python = strings.TrimSpace(v)
		}
	}
	return c
}

func (s *Service) lock(id int64) *sync.Mutex {
	m, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func (s *Service) row(ctx context.Context, siteID int64) (*Row, error) {
	var r Row
	err := s.db.WithContext(ctx).Where("site_id = ?", siteID).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &r, err
}

func cfgOf(site sites.Site, r *Row) runtimecfg.Config {
	if r == nil {
		return runtimecfg.Config{Runtime: runtimecfg.Static}
	}
	c := runtimecfg.Config{Runtime: r.Runtime, ID: site.ID, Version: r.Version, Command: r.Command}
	if r.Port != nil {
		c.Port = *r.Port
	}
	return c
}

// Get возвращает среду сайта и её состояние.
func (s *Service) Get(ctx context.Context, userID, siteID int64) (*View, error) {
	site, err := s.sites.Get(ctx, userID, siteID)
	if err != nil {
		return nil, err
	}
	r, err := s.row(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	v := &View{Runtime: runtimecfg.Static, Caps: s.Caps()}
	if r == nil {
		return v, nil
	}
	v.Runtime, v.Version, v.Command = r.Runtime, r.Version, r.Command
	if r.Port != nil {
		v.Port = *r.Port
	}
	v.State = "unknown"
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if res, err := s.applier.Do(sctx, Request{Action: ActionStatus, Host: site.Host, ID: site.ID, Runtime: r.Runtime, Version: r.Version}, 10*time.Second); err == nil && res.State != "" {
		v.State = res.State
	}
	return v, nil
}

func (s *Service) req(action string, site sites.Site, c runtimecfg.Config) Request {
	return Request{Action: action, Host: site.Host, ID: site.ID, Runtime: c.Runtime, Version: c.Version, Port: c.Port, Command: c.Command}
}

func (s *Service) do(ctx context.Context, action string, site sites.Site, c runtimecfg.Config, timeout time.Duration) (Result, error) {
	res, err := s.applier.Do(ctx, s.req(action, site, c), timeout)
	if err != nil {
		log.Printf("среды выполнения: %s %s: %v", action, site.Host, err)
		return res, ErrHelperDown
	}
	if !res.OK {
		log.Printf("среды выполнения: %s %s отклонено: %s: %s", action, site.Host, res.Error, strings.TrimSpace(res.Output))
		return res, ErrApplyFailed.With(res.Error)
	}
	return res, nil
}

// Set меняет среду сайта. Порядок такой, чтобы посетитель не увидел полусобранный сайт: сначала исполнитель поднимает пул или
// приложение, и только потом шлюз получает runtime.json; при обратном переходе к статике — наоборот.
func (s *Service) Set(ctx context.Context, userID, siteID int64, in Input) (*View, error) {
	site, err := s.sites.Get(ctx, userID, siteID)
	if err != nil {
		return nil, err
	}
	mu := s.lock(site.ID)
	mu.Lock()
	defer mu.Unlock()

	caps := s.Caps()
	next := runtimecfg.Config{Runtime: in.Runtime, ID: site.ID}
	switch in.Runtime {
	case runtimecfg.Static:
	case runtimecfg.PHP:
		if len(caps.PHP) == 0 {
			return nil, ErrRuntimeOff
		}
		if !slices.Contains(caps.PHP, in.Version) {
			return nil, ErrVersion
		}
		next.Version = in.Version
	case runtimecfg.Node, runtimecfg.Python:
		if (in.Runtime == runtimecfg.Node && caps.Node == "") || (in.Runtime == runtimecfg.Python && caps.Python == "") {
			return nil, ErrRuntimeOff
		}
		if !runtimecfg.ValidCommand(in.Command) {
			return nil, ErrCommand
		}
		next.Command = strings.TrimSpace(in.Command)
	default:
		return nil, ErrRuntime
	}

	old, err := s.row(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	oldCfg := cfgOf(*site, old)
	dir := s.sites.SiteDir(site.Host)

	if next.Runtime == runtimecfg.Static {
		if old == nil {
			return s.Get(ctx, userID, siteID)
		}
		if err := runtimecfg.Save(dir, next); err != nil { // шлюз перестаёт отправлять запросы в среду
			return nil, err
		}
		if _, err := s.do(ctx, ActionStop, *site, oldCfg, 60*time.Second); err != nil {
			_ = runtimecfg.Save(dir, oldCfg) // не удалось остановить: сайт остаётся в прежнем режиме
			return nil, err
		}
		if err := s.db.WithContext(ctx).Delete(&Row{}, "site_id = ?", site.ID).Error; err != nil {
			return nil, err
		}
		return s.Get(ctx, userID, siteID)
	}

	// Порт приложения сохраняется, пока сайт остаётся приложением.
	if next.Runtime == runtimecfg.Node || next.Runtime == runtimecfg.Python {
		if old != nil && old.Port != nil {
			next.Port = *old.Port
		}
	}
	row, err := s.save(ctx, site.ID, next, old)
	if err != nil {
		return nil, err
	}
	next.Port = 0
	if row.Port != nil {
		next.Port = *row.Port
	}
	if _, err := s.do(ctx, ActionApply, *site, next, 90*time.Second); err != nil {
		s.rollback(ctx, *site, old, oldCfg)
		return nil, err
	}
	if err := runtimecfg.Save(dir, next); err != nil {
		s.rollback(ctx, *site, old, oldCfg)
		return nil, err
	}
	return s.Get(ctx, userID, siteID)
}

// rollback возвращает прежнее состояние после неудачной смены: запись в БД и настройку исполнителя.
func (s *Service) rollback(ctx context.Context, site sites.Site, old *Row, oldCfg runtimecfg.Config) {
	c := context.WithoutCancel(ctx)
	if old == nil {
		_ = s.db.WithContext(c).Delete(&Row{}, "site_id = ?", site.ID).Error
		_, _ = s.applier.Do(c, s.req(ActionStop, site, runtimecfg.Config{Runtime: runtimecfg.Static, ID: site.ID}), 60*time.Second)
		return
	}
	_ = s.db.WithContext(c).Save(old).Error
	_, _ = s.applier.Do(c, s.req(ActionApply, site, oldCfg), 90*time.Second)
}

// save записывает выбор; для приложений выдаёт порт (уникальный на сервер) с повторами при гонке.
func (s *Service) save(ctx context.Context, siteID int64, c runtimecfg.Config, old *Row) (*Row, error) {
	row := &Row{SiteID: siteID, Runtime: c.Runtime, Version: c.Version, Command: c.Command, UpdatedAt: s.now()}
	app := c.Runtime == runtimecfg.Node || c.Runtime == runtimecfg.Python
	if !app {
		return row, s.upsert(ctx, row)
	}
	if c.Port != 0 {
		p := c.Port
		row.Port = &p
		return row, s.upsert(ctx, row)
	}
	for attempt := 0; attempt < 8; attempt++ {
		port, err := s.freePort(ctx)
		if err != nil {
			return nil, err
		}
		row.Port = &port
		err = s.upsert(ctx, row)
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			continue // порт заняли между проверкой и записью
		}
		return row, err
	}
	return nil, ErrPortsBusy
}

func (s *Service) upsert(ctx context.Context, row *Row) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "site_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"runtime", "version", "command", "port", "updated_at"}),
	}).Create(row).Error
}

func (s *Service) freePort(ctx context.Context) (int, error) {
	var used []int
	if err := s.db.WithContext(ctx).Model(&Row{}).Where("port IS NOT NULL").Pluck("port", &used).Error; err != nil {
		return 0, err
	}
	taken := make(map[int]bool, len(used))
	for _, p := range used {
		taken[p] = true
	}
	span := runtimecfg.PortMax - runtimecfg.PortMin + 1
	start := rand.IntN(span)
	for i := 0; i < span; i++ {
		p := runtimecfg.PortMin + (start+i)%span
		if !taken[p] {
			return p, nil
		}
	}
	return 0, ErrPortsBusy
}

// Restart перезапускает приложение (или перечитывает пул PHP). Не чаще раза в 10 секунд на сайт.
func (s *Service) Restart(ctx context.Context, userID, siteID int64) (*View, error) {
	site, err := s.sites.Get(ctx, userID, siteID)
	if err != nil {
		return nil, err
	}
	r, err := s.row(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrNotApp
	}
	s.restartMu.Lock()
	if t, ok := s.restarted[site.ID]; ok && s.now().Sub(t) < 10*time.Second {
		s.restartMu.Unlock()
		return nil, ErrRestartTooSoon
	}
	s.restarted[site.ID] = s.now()
	s.restartMu.Unlock()
	mu := s.lock(site.ID)
	mu.Lock()
	defer mu.Unlock()
	if _, err := s.do(ctx, ActionRestart, *site, cfgOf(*site, r), 60*time.Second); err != nil {
		return nil, err
	}
	return s.Get(ctx, userID, siteID)
}

// Logs возвращает конец журнала приложения или ошибок PHP.
func (s *Service) Logs(ctx context.Context, userID, siteID int64) (string, error) {
	site, err := s.sites.Get(ctx, userID, siteID)
	if err != nil {
		return "", err
	}
	r, err := s.row(ctx, site.ID)
	if err != nil {
		return "", err
	}
	if r == nil {
		return "", ErrNotApp
	}
	res, err := s.do(ctx, ActionLogs, *site, cfgOf(*site, r), 20*time.Second)
	if err != nil {
		return "", err
	}
	return res.Output, nil
}

// FixPerms вызывается перед и после деплоя и перед удалением сайта: у сайта со средой выполнения возвращает панели доступ к файлам,
// которые создал PHP или приложение. Ошибки не мешают операции: они только попадают в журнал.
func (s *Service) FixPerms(ctx context.Context, site sites.Site) {
	if s == nil || s.applier == nil {
		return
	}
	r, err := s.row(ctx, site.ID)
	if err != nil {
		return
	}
	if r == nil && (s.shellOn == nil || !s.shellOn(ctx, site.ID)) {
		return
	}
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	if _, err := s.applier.Do(pctx, s.req(ActionPerms, site, cfgOf(site, r)), 45*time.Second); err != nil {
		log.Printf("среды выполнения: права сайта %s: %v", site.Host, err)
	}
}

// Purge убирает всё, что исполнитель создал для сайта (вызывается при удалении сайта, после FixPerms).
func (s *Service) Purge(ctx context.Context, site sites.Site) {
	if s == nil || s.applier == nil {
		return
	}
	r, err := s.row(ctx, site.ID)
	if err != nil || r == nil {
		return
	}
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	if _, err := s.applier.Do(pctx, s.req(ActionPurge, site, cfgOf(site, r)), 60*time.Second); err != nil {
		log.Printf("среды выполнения: удаление среды сайта %s: %v", site.Host, err)
	}
}

package cms

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"vladhost/internal/apperr"
	"vladhost/internal/auth"
	"vladhost/internal/runtimes"
	"vladhost/internal/sites"
	"vladhost/internal/userdb"
)

var (
	ErrNotFound       = apperr.New(http.StatusNotFound, "cms_not_found", "cms is not available")
	ErrUnknownApp     = apperr.Validation("cms", "cms_app", "unknown application")
	ErrTitle          = apperr.Validation("title", "cms_title", "invalid site title")
	ErrAdminUser      = apperr.Validation("admin_user", "cms_admin_user", "invalid admin login")
	ErrAdminEmail     = apperr.Validation("admin_email", "cms_admin_email", "invalid admin email")
	ErrLocale         = apperr.Validation("locale", "cms_locale", "unknown language")
	ErrInstalled      = apperr.New(http.StatusConflict, "cms_installed", "an application is already installed")
	ErrBusy           = apperr.New(http.StatusConflict, "cms_busy", "an installation is already running")
	ErrSiteNotEmpty   = apperr.New(http.StatusConflict, "cms_site_not_empty", "site is not empty")
	ErrNoDatabase     = apperr.New(http.StatusConflict, "cms_no_database", "no MariaDB available")
	ErrNoPHP          = apperr.New(http.StatusConflict, "cms_no_php", "PHP is not available")
	ErrTooManyRunning = apperr.New(http.StatusServiceUnavailable, "cms_overloaded", "too many installations are running")
)

// Locales — языки, на которых можно установить WordPress.
var Locales = []string{"en_US", "ru_RU", "it_IT"}

var adminUserRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{2,59}$`)

// Databases — то, что установщику нужно от пользовательских баз (реализует *userdb.Service).
type Databases interface {
	Enabled() bool
	Info() userdb.Info
	Create(ctx context.Context, u auth.User, engine userdb.Engine, name string) (*userdb.Database, string, error)
	Delete(ctx context.Context, userID, id int64) error
	List(ctx context.Context, userID int64) ([]userdb.Database, error)
}

// Runtimes — среды выполнения сайта (реализует *runtimes.Service).
type Runtimes interface {
	Enabled() bool
	Caps() runtimes.Caps
	Get(ctx context.Context, userID, siteID int64) (*runtimes.View, error)
	Set(ctx context.Context, userID, siteID int64, in runtimes.Input) (*runtimes.View, error)
}

// Config — настройки установщика.
type Config struct {
	Catalog []App
	// CacheDir — где хранятся скачанные архивы (проверенные по sha256). Пусто — временный каталог на каждую установку.
	CacheDir string
	// GatewayURL — адрес веб-шлюза, через который выполняется установка самого приложения (http://127.0.0.1:8091).
	GatewayURL string
	// DBHost — как PHP подключается к MariaDB (localhost — через сокет).
	DBHost string
	// Concurrency — сколько установок одновременно на весь сервер (по умолчанию 2).
	Concurrency int
	// InstallerWait — сколько ждать, пока только что включённый пул PHP отзовётся (по умолчанию 40 с), PollEvery — как часто спрашивать.
	InstallerWait time.Duration
	PollEvery     time.Duration
	// HTTP — клиент для скачивания; nil — обычный с таймаутом.
	HTTP *http.Client
}

// Row — что установлено на сайте.
type Row struct {
	SiteID      int64 `gorm:"primaryKey"`
	CMS         string
	Version     string
	DBName      string
	InstalledAt time.Time
}

func (Row) TableName() string { return "site_cms" }

// Input — выбор пользователя при установке.
type Input struct {
	CMS        string
	Title      string
	AdminUser  string
	AdminEmail string
	Locale     string
}

// Service — установка приложений.
type Service struct {
	db    *gorm.DB
	sites *sites.Service
	dbs   Databases
	rt    Runtimes
	cfg   Config
	now   func() time.Time
	sem   chan struct{}
	wg    sync.WaitGroup

	mu   sync.Mutex
	jobs map[int64]*Job // последняя установка сайта
}

// New создаёт службу.
func New(db *gorm.DB, siteSvc *sites.Service, dbs Databases, rt Runtimes, cfg Config) *Service {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 2
	}
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = "http://127.0.0.1:8091"
	}
	if cfg.DBHost == "" {
		cfg.DBHost = "localhost"
	}
	if cfg.InstallerWait <= 0 {
		cfg.InstallerWait = 40 * time.Second
	}
	if cfg.PollEvery <= 0 {
		cfg.PollEvery = time.Second
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 10 * time.Minute}
	}
	return &Service{db: db, sites: siteSvc, dbs: dbs, rt: rt, cfg: cfg, now: time.Now, sem: make(chan struct{}, cfg.Concurrency), jobs: map[int64]*Job{}}
}

// Wait ждёт завершения фоновых установок (для тестов и остановки).
func (s *Service) Wait() { s.wg.Wait() }

func (s *Service) app(id string) (App, bool) {
	for _, a := range s.cfg.Catalog {
		if a.ID == id {
			return a, true
		}
	}
	return App{}, false
}

// Enabled: установщик доступен, только если есть и среды выполнения, и базы MariaDB, и приложения в каталоге.
func (s *Service) Enabled() bool {
	return s != nil && len(s.cfg.Catalog) > 0 && s.rt != nil && s.rt.Enabled() && len(s.rt.Caps().PHP) > 0 && s.hasMaria()
}

func (s *Service) hasMaria() bool {
	if s.dbs == nil || !s.dbs.Enabled() {
		return false
	}
	return slices.Contains(s.dbs.Info().Engines, userdb.MariaDB)
}

// Installed — сведения об установленном приложении.
type Installed struct {
	CMS         string    `json:"cms"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	DBName      string    `json:"db_name"`
	InstalledAt time.Time `json:"installed_at"`
	URL         string    `json:"url"`
	AdminURL    string    `json:"admin_url"`
}

// Requirements — что нужно для установки и что уже выполнено.
type Requirements struct {
	PHP      bool `json:"php"`      // на сервере есть PHP (среда включится сама)
	Database bool `json:"database"` // MariaDB доступна и лимит баз не исчерпан
	Empty    bool `json:"empty"`    // папка сайта пуста
}

// Status — состояние раздела для сайта.
type Status struct {
	Available    bool         `json:"available"`
	Catalog      []App        `json:"catalog"`
	Locales      []string     `json:"locales"`
	Installed    *Installed   `json:"installed"`
	Requirements Requirements `json:"requirements"`
	Job          *JobView     `json:"job"`
}

// Status возвращает состояние установщика для сайта пользователя.
func (s *Service) Status(ctx context.Context, userID, siteID int64) (*Status, error) {
	site, err := s.sites.Get(ctx, userID, siteID)
	if err != nil {
		return nil, err
	}
	st := &Status{Available: s.Enabled(), Catalog: []App{}, Locales: Locales}
	if !st.Available {
		return st, nil
	}
	for _, a := range s.cfg.Catalog {
		a.URL, a.SHA256 = "", "" // пользователю адрес и сумма не нужны
		st.Catalog = append(st.Catalog, a)
	}
	row, err := s.row(ctx, site)
	if err != nil {
		return nil, err
	}
	if row != nil {
		app, _ := s.app(row.CMS)
		name := app.Name
		if name == "" {
			name = row.CMS
		}
		st.Installed = &Installed{CMS: row.CMS, Name: name, Version: row.Version, DBName: row.DBName, InstalledAt: row.InstalledAt,
			URL: "https://" + site.Host + "/", AdminURL: "https://" + site.Host + "/wp-admin/"}
	}
	empty, _ := s.publicEmpty(ctx, userID, site.ID)
	st.Requirements = Requirements{PHP: true, Database: s.databaseFree(ctx, userID), Empty: empty}
	st.Job = s.jobView(site.ID)
	return st, nil
}

// row возвращает запись об установке; если файлы приложения удалены (нет wp-config.php), запись забывается и сайт можно ставить заново.
func (s *Service) row(ctx context.Context, site *sites.Site) (*Row, error) {
	var r Row
	err := s.db.WithContext(ctx).Where("site_id = ?", site.ID).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.sites.ReadFile(ctx, site.UserID, site.ID, "wp-config.php"); err != nil {
		if errors.Is(err, sites.ErrFileNotFound) {
			_ = s.db.WithContext(ctx).Delete(&Row{}, "site_id = ?", site.ID).Error
			return nil, nil
		}
	}
	return &r, nil
}

func (s *Service) publicEmpty(ctx context.Context, userID, siteID int64) (bool, error) {
	entries, err := s.sites.ListDir(ctx, userID, siteID, "")
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

// databaseFree: есть MariaDB и пользователь не исчерпал лимит баз в ней.
func (s *Service) databaseFree(ctx context.Context, userID int64) bool {
	if !s.hasMaria() {
		return false
	}
	list, err := s.dbs.List(ctx, userID)
	if err != nil {
		return false
	}
	n := 0
	for _, d := range list {
		if d.Engine == userdb.MariaDB {
			n++
		}
	}
	return n < s.dbs.Info().PerEngine
}

func validate(in *Input) error {
	in.Title = strings.TrimSpace(in.Title)
	if n := utf8.RuneCountInString(in.Title); n < 1 || n > 80 || strings.ContainsAny(in.Title, "\n\r\x00") {
		return ErrTitle
	}
	in.AdminUser = strings.TrimSpace(in.AdminUser)
	if !adminUserRe.MatchString(in.AdminUser) {
		return ErrAdminUser
	}
	in.AdminEmail = strings.TrimSpace(in.AdminEmail)
	if a, err := mail.ParseAddress(in.AdminEmail); err != nil || a.Address != in.AdminEmail || len(in.AdminEmail) > 120 {
		return ErrAdminEmail
	}
	if !slices.Contains(Locales, in.Locale) {
		return ErrLocale
	}
	return nil
}

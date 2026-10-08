package shellaccess

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"vladhost/internal/apperr"
	"vladhost/internal/runtimes"
	"vladhost/internal/shellclient"
	"vladhost/internal/sites"
)

var (
	ErrHelperDown  = apperr.New(http.StatusServiceUnavailable, "shell_helper_down", "runtime helper is not available")
	ErrApplyFailed = apperr.New(http.StatusBadGateway, "shell_apply_failed", "runtime helper refused the request")
	ErrDisabled    = apperr.New(http.StatusConflict, "shell_disabled", "shell access is not enabled for this site")
)

// Config — настройки.
type Config struct {
	Applier    runtimes.Applier
	Broker     shellclient.Client
	BaseDomain string
	// SSHHost и SSHPort — как пользователь подключается снаружи (ssh.vladinc.ru:2222); HostFingerprint — отпечаток ключа сервера.
	SSHHost         string
	SSHPort         int
	HostFingerprint func() string
}

// Grant — итог проверки входа по ключу.
type Grant struct {
	UserID   int64
	Username string
	SiteID   int64
	Host     string
	KeyID    int64
}

// Row — сайт с включённым доступом.
type Row struct {
	SiteID    int64 `gorm:"primaryKey"`
	EnabledAt time.Time
}

func (Row) TableName() string { return "site_shell" }

// Service — доступ к оболочке.
type Service struct {
	db    *gorm.DB
	sites *sites.Service
	cfg   Config
	now   func() time.Time
	locks sync.Map
}

// New создаёт службу.
func New(db *gorm.DB, siteSvc *sites.Service, cfg Config) *Service {
	if cfg.SSHPort == 0 {
		cfg.SSHPort = 2222
	}
	return &Service{db: db, sites: siteSvc, cfg: cfg, now: time.Now}
}

func (s *Service) lock(id int64) *sync.Mutex {
	m, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// Enabled: есть исполнитель, который настраивает доступ.
func (s *Service) Enabled() bool { return s != nil && s.cfg.Applier != nil }

// SiteEnabled: включён ли у сайта доступ к оболочке (без проверки владельца — для внутренних вызовов панели).
func (s *Service) SiteEnabled(ctx context.Context, siteID int64) bool {
	if s == nil {
		return false
	}
	var n int64
	return s.db.WithContext(ctx).Model(&Row{}).Where("site_id = ?", siteID).Count(&n).Error == nil && n > 0
}

// HostFingerprint — отпечаток ключа SSH-сервера панели.
func (s *Service) HostFingerprint() string {
	if s.cfg.HostFingerprint == nil {
		return ""
	}
	return s.cfg.HostFingerprint()
}

// Status — состояние доступа к оболочке сайта.
type Status struct {
	Enabled     bool       `json:"enabled"`
	EnabledAt   *time.Time `json:"enabled_at"`
	Login       string     `json:"login"`
	Host        string     `json:"host"`
	Port        int        `json:"port"`
	Fingerprint string     `json:"host_fingerprint"`
	Keys        int        `json:"keys"`
}

func (s *Service) login(site *sites.Site) string {
	return strings.TrimSuffix(site.Host, "."+s.cfg.BaseDomain)
}

// Status возвращает состояние доступа.
func (s *Service) Status(ctx context.Context, userID, siteID int64) (*Status, error) {
	site, err := s.sites.Get(ctx, userID, siteID)
	if err != nil {
		return nil, err
	}
	st := &Status{Login: s.login(site), Host: s.cfg.SSHHost, Port: s.cfg.SSHPort}
	if s.cfg.HostFingerprint != nil {
		st.Fingerprint = s.cfg.HostFingerprint()
	}
	var row Row
	err = s.db.WithContext(ctx).Where("site_id = ?", siteID).First(&row).Error
	switch {
	case err == nil:
		st.Enabled, st.EnabledAt = true, &row.EnabledAt
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, err
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&Key{}).Where("user_id = ?", userID).Count(&n).Error; err != nil {
		return nil, err
	}
	st.Keys = int(n)
	return st, nil
}

func (s *Service) do(ctx context.Context, action string, site *sites.Site, timeout time.Duration) error {
	res, err := s.cfg.Applier.Do(ctx, runtimes.Request{Action: action, Host: site.Host, ID: site.ID, Runtime: "static"}, timeout)
	if err != nil {
		log.Printf("оболочка: %s %s: %v", action, site.Host, err)
		return ErrHelperDown
	}
	if !res.OK {
		log.Printf("оболочка: %s %s отклонено: %s: %s", action, site.Host, res.Error, strings.TrimSpace(res.Output))
		return ErrApplyFailed.With(res.Error)
	}
	return nil
}

// Enable включает доступ: исполнитель заводит пользователя сайта и права, потом запись сохраняется (посредник впускает только сайты с меткой).
func (s *Service) Enable(ctx context.Context, userID, siteID int64) (*Status, error) {
	site, err := s.sites.Get(ctx, userID, siteID)
	if err != nil {
		return nil, err
	}
	mu := s.lock(siteID)
	mu.Lock()
	defer mu.Unlock()
	if err := s.do(ctx, runtimes.ActionShellOn, site, 60*time.Second); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Where("site_id = ?", siteID).FirstOrCreate(&Row{SiteID: siteID, EnabledAt: s.now()}).Error; err != nil {
		return nil, err
	}
	return s.Status(ctx, userID, siteID)
}

// Disable выключает доступ: открытые сеансы закрываются, метка снимается.
func (s *Service) Disable(ctx context.Context, userID, siteID int64) (*Status, error) {
	site, err := s.sites.Get(ctx, userID, siteID)
	if err != nil {
		return nil, err
	}
	mu := s.lock(siteID)
	mu.Lock()
	defer mu.Unlock()
	if err := s.do(ctx, runtimes.ActionShellOff, site, 60*time.Second); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Delete(&Row{}, "site_id = ?", siteID).Error; err != nil {
		return nil, err
	}
	s.KillSessions(ctx, siteID)
	return s.Status(ctx, userID, siteID)
}

// KillSessions закрывает открытые сеансы сайта; недоступный посредник не мешает (сеансов тогда и нет).
func (s *Service) KillSessions(ctx context.Context, siteID int64) {
	if s.cfg.Broker.Socket == "" {
		return
	}
	kctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, err := s.cfg.Broker.Kill(kctx, siteID); err != nil {
		log.Printf("оболочка: закрытие сеансов сайта %d: %v", siteID, err)
	}
}

// Purge вызывается при удалении сайта: сеансы закрываются, а исполнитель убирает пользователя и права.
func (s *Service) Purge(ctx context.Context, site sites.Site) {
	if s == nil || s.cfg.Applier == nil {
		return
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&Row{}).Where("site_id = ?", site.ID).Count(&n).Error; err != nil || n == 0 {
		return
	}
	s.KillSessions(ctx, site.ID)
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	// purge убирает всё, что создано для сайта: метку доступа, пользователя, права (среду выполнения при её наличии убирает она же).
	if _, err := s.cfg.Applier.Do(pctx, runtimes.Request{Action: runtimes.ActionPurge, Host: site.Host, ID: site.ID, Runtime: "static"}, 60*time.Second); err != nil {
		log.Printf("оболочка: отключение удаляемого сайта %s: %v", site.Host, err)
	}
}

var loginRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,60}[a-z0-9])?\.[a-z0-9]([a-z0-9-]{0,60}[a-z0-9])?$`)

// ErrAuth — вход не разрешён (причину клиенту не сообщаем).
var ErrAuth = errors.New("shellaccess: access denied")

// Authenticate проверяет вход по ключу: логин — «сайт.пользователь» (как у FTP), ключ принадлежит владельцу сайта, доступ включён.
func (s *Service) Authenticate(ctx context.Context, login string, key ssh.PublicKey) (*Grant, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	if !loginRe.MatchString(login) {
		return nil, ErrAuth
	}
	var k Key
	if err := s.db.WithContext(ctx).Where("fingerprint = ?", ssh.FingerprintSHA256(key)).First(&k).Error; err != nil {
		return nil, ErrAuth
	}
	// Ключ мог быть подменён в базе на другой с тем же отпечатком? Отпечаток уникален, но сверяем и саму запись.
	if k.PublicKey != strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) {
		return nil, ErrAuth
	}
	host := login + "." + s.cfg.BaseDomain
	var site sites.Site
	if err := s.db.WithContext(ctx).Where("host = ? AND user_id = ?", host, k.UserID).First(&site).Error; err != nil {
		return nil, ErrAuth
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&Row{}).Where("site_id = ?", site.ID).Count(&n).Error; err != nil || n == 0 {
		return nil, ErrAuth
	}
	var username string
	if err := s.db.WithContext(ctx).Table("users").Select("username").Where("id = ?", k.UserID).Scan(&username).Error; err != nil {
		return nil, ErrAuth
	}
	return &Grant{UserID: k.UserID, Username: username, SiteID: site.ID, Host: site.Host, KeyID: k.ID}, nil
}

// Grant для веб-терминала: пользователь уже вошёл в панель, поэтому проверяется только владение сайтом и включённый доступ.
func (s *Service) WebGrant(ctx context.Context, userID, siteID int64) (*Grant, error) {
	site, err := s.sites.Get(ctx, userID, siteID)
	if err != nil {
		return nil, err
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&Row{}).Where("site_id = ?", siteID).Count(&n).Error; err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrDisabled
	}
	return &Grant{UserID: userID, SiteID: siteID, Host: site.Host}, nil
}

func (g Grant) String() string {
	return fmt.Sprintf("site=%d host=%s user=%d", g.SiteID, g.Host, g.UserID)
}

// Touch отмечает, что ключ использован для входа (вызывается после проверки подписи, а не при пробном запросе клиента).
func (s *Service) Touch(ctx context.Context, keyID int64) {
	if err := s.db.WithContext(ctx).Model(&Key{}).Where("id = ?", keyID).Update("last_used_at", s.now()).Error; err != nil {
		log.Printf("оболочка: отметка входа ключа %d: %v", keyID, err)
	}
}

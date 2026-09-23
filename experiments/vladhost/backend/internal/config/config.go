// Package config читает настройки сервера из переменных окружения.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr         string
	DatabaseURL  string
	JWTSecret    []byte
	CookieSecure bool
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
	// Лимит запросов к /api/auth/* с одного IP; 0 — значения по умолчанию (20 в минуту, всплеск 10).
	AuthPerMinute, AuthBurst int

	SitesRoot      string // где лежат файлы сайтов: {SitesRoot}/{host}/public
	BaseDomain     string // сайты живут на {site}.{user}.{BaseDomain}
	FTP            FTPConfig
	CertsDir       string // обмен с выпускателем сертификатов (queue/ и status/); пусто — выключено
	PanelOrigin    string // https://app.vladinc.ru — для проверки Origin у cookie-эндпоинтов; пусто — не проверять
	MaxSites       int    // сайтов на пользователя
	DiskQuotaBytes int64  // диск на пользователя (все его сайты вместе)
}

const minSecretLen = 32

// FTPConfig — встроенный FTP-сервер (только FTPS). Пустой Addr выключает FTP.
type FTPConfig struct {
	Addr         string // например ":2121"
	Host         string // имя, которое видят клиенты (ftp.vladinc.ru)
	PublicIP     string // IP для пассивного режима
	PassiveStart int    // диапазон портов пассивного режима
	PassiveEnd   int
	CertFile     string // пусто — на старте создаётся временный самоподписанный сертификат (только dev)
	KeyFile      string
}

// Load собирает конфиг; обязательные значения без умолчаний, чтобы не запуститься с небезопасным секретом.
func Load() (Config, error) {
	cfg := Config{
		Addr:         env("VLADHOST_ADDR", "127.0.0.1:8090"),
		DatabaseURL:  os.Getenv("VLADHOST_DATABASE_URL"),
		JWTSecret:    []byte(os.Getenv("VLADHOST_JWT_SECRET")),
		CookieSecure: env("VLADHOST_COOKIE_SECURE", "true") != "false",
		AccessTTL:    15 * time.Minute,
		RefreshTTL:   30 * 24 * time.Hour,

		SitesRoot:      env("VLADHOST_SITES_ROOT", "/data/vladhost/sites"),
		BaseDomain:     env("VLADHOST_BASE_DOMAIN", "vladinc.ru"),
		CertsDir:       os.Getenv("VLADHOST_CERTS_DIR"),
		PanelOrigin:    os.Getenv("VLADHOST_PANEL_ORIGIN"),
		MaxSites:       1,
		DiskQuotaBytes: 500 << 20,
	}
	if cfg.DatabaseURL == "" {
		return cfg, errors.New("VLADHOST_DATABASE_URL не задан")
	}
	if len(cfg.JWTSecret) < minSecretLen {
		return cfg, fmt.Errorf("VLADHOST_JWT_SECRET должен быть не короче %d символов", minSecretLen)
	}
	if err := loadFTP(&cfg.FTP); err != nil {
		return cfg, err
	}
	if v := os.Getenv("VLADHOST_ACCESS_TTL_MINUTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return cfg, errors.New("VLADHOST_ACCESS_TTL_MINUTES: нужно положительное число")
		}
		cfg.AccessTTL = time.Duration(n) * time.Minute
	}
	return cfg, nil
}

func loadFTP(f *FTPConfig) error {
	f.Addr = os.Getenv("VLADHOST_FTP_ADDR")
	if f.Addr == "" {
		return nil
	}
	f.Host = env("VLADHOST_FTP_HOST", "ftp.vladinc.ru")
	f.PublicIP = os.Getenv("VLADHOST_FTP_PUBLIC_IP")
	f.CertFile = os.Getenv("VLADHOST_FTP_CERT")
	f.KeyFile = os.Getenv("VLADHOST_FTP_KEY")
	if (f.CertFile == "") != (f.KeyFile == "") {
		return errors.New("VLADHOST_FTP_CERT и VLADHOST_FTP_KEY задаются вместе")
	}
	ports := env("VLADHOST_FTP_PASSIVE_PORTS", "50000-50100")
	lo, hi, ok := strings.Cut(ports, "-")
	var err1, err2 error
	f.PassiveStart, err1 = strconv.Atoi(lo)
	f.PassiveEnd, err2 = strconv.Atoi(hi)
	if !ok || err1 != nil || err2 != nil || f.PassiveStart < 1024 || f.PassiveEnd < f.PassiveStart || f.PassiveEnd > 65535 {
		return fmt.Errorf("VLADHOST_FTP_PASSIVE_PORTS: ожидается диапазон вида 50000-50100, получено %q", ports)
	}
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

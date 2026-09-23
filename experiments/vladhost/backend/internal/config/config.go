// Package config читает настройки сервера из переменных окружения.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
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
	CertsDir       string // обмен с выпускателем сертификатов (queue/ и status/); пусто — выключено
	PanelOrigin    string // https://app.vladinc.ru — для проверки Origin у cookie-эндпоинтов; пусто — не проверять
	MaxSites       int    // сайтов на пользователя
	DiskQuotaBytes int64  // диск на пользователя (все его сайты вместе)
}

const minSecretLen = 32

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
	if v := os.Getenv("VLADHOST_ACCESS_TTL_MINUTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return cfg, errors.New("VLADHOST_ACCESS_TTL_MINUTES: нужно положительное число")
		}
		cfg.AccessTTL = time.Duration(n) * time.Minute
	}
	return cfg, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

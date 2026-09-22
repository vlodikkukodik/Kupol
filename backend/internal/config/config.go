// Package config читает настройки Go API из переменных окружения.
package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// MinSecretBytes — минимальная длина общего секрета PHP-прокси и Go API.
const MinSecretBytes = 32

// Limits — ограничения частоты. Значения по умолчанию взяты из спецификации (§2);
// когда появится админка (этап 7), они переедут в её настройки.
type Limits struct {
	LoginAttempts     int           // неудачных входов на пару IP+логин за LoginWindow
	LoginLockAttempts int           // неудачных входов на один логин с любых IP за LoginWindow
	LoginWindow       time.Duration // окно для двух предыдущих лимитов
	RegisterPerHour   int           // регистраций с одного IP в час
	APIPerMinute      int           // запросов к API в минуту на пользователя (или IP, если не вошёл)
}

// DefaultLimits — значения из спецификации.
func DefaultLimits() Limits {
	return Limits{
		LoginAttempts:     5,
		LoginLockAttempts: 15,
		LoginWindow:       15 * time.Minute,
		RegisterPerHour:   3,
		APIPerMinute:      120,
	}
}

// SMTP — настройки почты для писем читателям (подтверждение адреса, уведомления). Пустой Host — почта выключена:
// вызывающий код (accounts.Service) должен сам это проверять и просто не отправлять письмо.
type SMTP struct {
	Host, Port         string
	Username, Password string
	From, FromName     string
}

// Enabled — задан ли SMTP-сервер.
func (s SMTP) Enabled() bool { return s.Host != "" }

type Config struct {
	Env            string // "dev" | "prod"
	LogLevel       slog.Level
	HTTPAddr       string
	DatabaseURL    string
	ProxySecret    []byte
	TrustedProxies []string // CIDR/IP обратных прокси (nginx/caddy) перед Go
	// SiteOrigin — origin сайта (схема://хост[:порт], без пути). Запросы с изменяющими методами
	// принимаются только с этим Origin (защита от CSRF).
	SiteOrigin string
	Limits     Limits
	SMTP       SMTP
}

func (c Config) IsProd() bool { return c.Env == "prod" }

// CookieSecure — ставить ли куки с флагом Secure. В проде всегда; в dev сайт ходит по http.
func (c Config) CookieSecure() bool { return c.IsProd() }

// Load читает и валидирует конфигурацию. Все найденные ошибки возвращаются разом.
func Load() (Config, error) {
	return load(os.Getenv)
}

func load(get func(string) string) (Config, error) {
	var errs []error
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	c := Config{
		Env:      strings.ToLower(orDefault(get("KUPOL_ENV"), "dev")),
		HTTPAddr: orDefault(get("KUPOL_HTTP_ADDR"), "127.0.0.1:8080"),
	}
	if c.Env != "dev" && c.Env != "prod" {
		fail("KUPOL_ENV: ожидается dev или prod, получено %q", c.Env)
	}

	if err := c.LogLevel.UnmarshalText([]byte(orDefault(get("KUPOL_LOG_LEVEL"), "info"))); err != nil {
		fail("KUPOL_LOG_LEVEL: %v", err)
	}

	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		fail("KUPOL_HTTP_ADDR: %v", err)
	}

	c.DatabaseURL = get("KUPOL_DATABASE_URL")
	if c.DatabaseURL == "" {
		fail("KUPOL_DATABASE_URL: не задан")
	}

	secret, err := decodeSecret(get("KUPOL_PROXY_SECRET"))
	if err != nil {
		fail("KUPOL_PROXY_SECRET: %v", err)
	}
	c.ProxySecret = secret

	for _, item := range strings.Split(get("KUPOL_TRUSTED_PROXIES"), ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, perr := netip.ParsePrefix(item); perr != nil {
			if _, aerr := netip.ParseAddr(item); aerr != nil {
				fail("KUPOL_TRUSTED_PROXIES: %q не CIDR и не IP", item)
				continue
			}
		}
		c.TrustedProxies = append(c.TrustedProxies, item)
	}

	origin := strings.TrimRight(strings.TrimSpace(get("KUPOL_SITE_ORIGIN")), "/")
	if origin == "" && !c.IsProd() {
		origin = "http://127.0.0.1:5173" // адрес Vite в dev
	}
	if err := validateOrigin(origin); err != nil {
		fail("KUPOL_SITE_ORIGIN: %v", err)
	}
	c.SiteOrigin = origin

	c.Limits = DefaultLimits()
	for _, l := range []struct {
		env string
		dst *int
	}{
		{"KUPOL_LIMIT_LOGIN_ATTEMPTS", &c.Limits.LoginAttempts},
		{"KUPOL_LIMIT_LOGIN_LOCK_ATTEMPTS", &c.Limits.LoginLockAttempts},
		{"KUPOL_LIMIT_REGISTER_PER_HOUR", &c.Limits.RegisterPerHour},
		{"KUPOL_LIMIT_API_PER_MINUTE", &c.Limits.APIPerMinute},
	} {
		v := strings.TrimSpace(get(l.env))
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1_000_000 {
			fail("%s: ожидается целое от 1 до 1000000, получено %q", l.env, v)
			continue
		}
		*l.dst = n
	}

	c.SMTP = SMTP{
		Host:     strings.TrimSpace(get("KUPOL_SMTP_HOST")),
		Port:     orDefault(strings.TrimSpace(get("KUPOL_SMTP_PORT")), "587"),
		Username: get("KUPOL_SMTP_USERNAME"),
		Password: get("KUPOL_SMTP_PASSWORD"),
		From:     strings.TrimSpace(get("KUPOL_SMTP_FROM")),
		FromName: orDefault(strings.TrimSpace(get("KUPOL_SMTP_FROM_NAME")), "КУПОЛ"),
	}
	if c.SMTP.Enabled() {
		if _, err := strconv.Atoi(c.SMTP.Port); err != nil {
			fail("KUPOL_SMTP_PORT: ожидается номер порта, получено %q", c.SMTP.Port)
		}
		if c.SMTP.From == "" {
			fail("KUPOL_SMTP_FROM: не задан (нужен вместе с KUPOL_SMTP_HOST)")
		}
	}

	return c, errors.Join(errs...)
}

// validateOrigin: схема://хост[:порт] без пути, запроса, фрагмента и учётных данных.
func validateOrigin(origin string) error {
	if origin == "" {
		return errors.New("не задан (например, https://kupol.vladinc.ru)")
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("%q не похож на origin: нужно схема://хост[:порт] без пути", origin)
	}
	return nil
}

// decodeSecret принимает секрет в hex или base64 (std/url, с паддингом или без).
func decodeSecret(v string) ([]byte, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, errors.New("не задан")
	}
	var raw []byte
	if b, err := hex.DecodeString(v); err == nil {
		raw = b
	} else if b, err := base64.StdEncoding.DecodeString(v); err == nil {
		raw = b
	} else if b, err := base64.RawStdEncoding.DecodeString(v); err == nil {
		raw = b
	} else if b, err := base64.URLEncoding.DecodeString(v); err == nil {
		raw = b
	} else if b, err := base64.RawURLEncoding.DecodeString(v); err == nil {
		raw = b
	} else {
		return nil, errors.New("не hex и не base64")
	}
	if len(raw) < MinSecretBytes {
		return nil, fmt.Errorf("слишком короткий: %d байт, нужно не меньше %d", len(raw), MinSecretBytes)
	}
	return raw, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

package config

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func validEnv() map[string]string {
	return map[string]string{
		"KUPOL_DATABASE_URL": "postgres://u:p@localhost/db",
		"KUPOL_PROXY_SECRET": strings.Repeat("ab", 32),
	}
}

func TestLoadDefaults(t *testing.T) {
	c, err := load(env(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != "dev" || c.HTTPAddr != "127.0.0.1:8080" || c.LogLevel.String() != "INFO" {
		t.Fatalf("неверные значения по умолчанию: %+v", c)
	}
	if len(c.ProxySecret) != 32 {
		t.Fatalf("секрет: %d байт", len(c.ProxySecret))
	}
}

func TestLoadCollectsAllErrors(t *testing.T) {
	_, err := load(env(map[string]string{
		"KUPOL_ENV":             "staging",
		"KUPOL_LOG_LEVEL":       "loud",
		"KUPOL_HTTP_ADDR":       "nonsense",
		"KUPOL_TRUSTED_PROXIES": "10.0.0.0/8, bogus",
	}))
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	for _, want := range []string{"KUPOL_ENV", "KUPOL_LOG_LEVEL", "KUPOL_HTTP_ADDR", "KUPOL_DATABASE_URL", "KUPOL_PROXY_SECRET", "KUPOL_TRUSTED_PROXIES"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в ошибке нет %s: %v", want, err)
		}
	}
}

func TestSecretFormats(t *testing.T) {
	raw := make([]byte, 40)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	for name, v := range map[string]string{
		"hex":       hex.EncodeToString(raw),
		"std":       base64.StdEncoding.EncodeToString(raw),
		"raw":       base64.RawStdEncoding.EncodeToString(raw),
		"url":       base64.URLEncoding.EncodeToString(raw),
		"url-nopad": base64.RawURLEncoding.EncodeToString(raw),
	} {
		got, err := decodeSecret(v)
		if err != nil || string(got) != string(raw) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

func TestSiteOrigin(t *testing.T) {
	// dev: значение по умолчанию — адрес Vite
	c, err := load(env(validEnv()))
	if err != nil || c.SiteOrigin != "http://127.0.0.1:5173" {
		t.Fatalf("dev по умолчанию: %q, %v", c.SiteOrigin, err)
	}

	// хвостовой слеш и пробелы убираются
	e := validEnv()
	e["KUPOL_SITE_ORIGIN"] = " https://kupol.vladinc.ru/ "
	c, err = load(env(e))
	if err != nil || c.SiteOrigin != "https://kupol.vladinc.ru" {
		t.Fatalf("нормализация: %q, %v", c.SiteOrigin, err)
	}

	// prod: обязателен
	e = validEnv()
	e["KUPOL_ENV"] = "prod"
	if _, err = load(env(e)); err == nil || !strings.Contains(err.Error(), "KUPOL_SITE_ORIGIN") {
		t.Fatalf("в prod origin обязателен: %v", err)
	}

	for _, bad := range []string{"kupol.vladinc.ru", "ftp://kupol.vladinc.ru", "https://kupol.vladinc.ru/app", "https://u:p@kupol.vladinc.ru", "https://kupol.vladinc.ru?x=1", "https://"} {
		e = validEnv()
		e["KUPOL_SITE_ORIGIN"] = bad
		if _, err = load(env(e)); err == nil || !strings.Contains(err.Error(), "KUPOL_SITE_ORIGIN") {
			t.Errorf("%q должен отвергаться: %v", bad, err)
		}
	}
}

func TestLimits(t *testing.T) {
	c, err := load(env(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	// значения по умолчанию — из спецификации
	if c.Limits.LoginAttempts != 5 || c.Limits.RegisterPerHour != 3 || c.Limits.APIPerMinute != 120 ||
		c.Limits.LoginLockAttempts != 15 || c.Limits.LoginWindow != 15*time.Minute {
		t.Fatalf("лимиты по умолчанию: %+v", c.Limits)
	}

	e := validEnv()
	e["KUPOL_LIMIT_LOGIN_ATTEMPTS"] = "50"
	e["KUPOL_LIMIT_REGISTER_PER_HOUR"] = "1000"
	c, err = load(env(e))
	if err != nil || c.Limits.LoginAttempts != 50 || c.Limits.RegisterPerHour != 1000 || c.Limits.APIPerMinute != 120 {
		t.Fatalf("переопределение: %+v, %v", c.Limits, err)
	}

	for _, bad := range []string{"0", "-1", "abc", "1.5", "2000000"} {
		e = validEnv()
		e["KUPOL_LIMIT_API_PER_MINUTE"] = bad
		if _, err = load(env(e)); err == nil || !strings.Contains(err.Error(), "KUPOL_LIMIT_API_PER_MINUTE") {
			t.Errorf("%q должен отвергаться: %v", bad, err)
		}
	}
}

func TestCookieSecureOnlyInProd(t *testing.T) {
	if (Config{Env: "dev"}).CookieSecure() || !(Config{Env: "prod"}).CookieSecure() {
		t.Fatal("Secure-куки: только в prod")
	}
}

func TestSecretTooShort(t *testing.T) {
	if _, err := decodeSecret(hex.EncodeToString(make([]byte, 31))); err == nil {
		t.Fatal("31 байт должно отвергаться")
	}
	if _, err := decodeSecret("не-секрет!"); err == nil {
		t.Fatal("мусор должен отвергаться")
	}
}

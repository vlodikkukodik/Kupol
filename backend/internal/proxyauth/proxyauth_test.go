package proxyauth

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

var secret = []byte("0123456789abcdef0123456789abcdef")

func TestVerifyOK(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ts := now.Unix()
	sig := Sign(secret, ts, "203.0.113.7", "post", "/api/x?a=1")
	ip, err := Verify(secret, now, "203.0.113.7", strconv.FormatInt(ts, 10), sig, "POST", "/api/x?a=1")
	if err != nil || ip.String() != "203.0.113.7" {
		t.Fatalf("ip=%v err=%v", ip, err)
	}
}

func TestVerifyIPv6AndMapped(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	for in, want := range map[string]string{
		"2001:db8::1":        "2001:db8::1",
		"::ffff:203.0.113.7": "203.0.113.7",
	} {
		sig := Sign(secret, now.Unix(), in, "GET", "/api/health")
		ip, err := Verify(secret, now, in, ts, sig, "GET", "/api/health")
		if err != nil || ip.String() != want {
			t.Errorf("%s: ip=%v err=%v", in, ip, err)
		}
	}
}

func TestVerifyRejects(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ts := now.Unix()
	tsS := strconv.FormatInt(ts, 10)
	good := Sign(secret, ts, "203.0.113.7", "GET", "/api/health")

	cases := []struct {
		name                     string
		ip, ts, sig, method, uri string
		now                      time.Time
		want                     error
	}{
		{"нет заголовков", "", "", "", "GET", "/api/health", now, ErrMissing},
		{"нет подписи", "203.0.113.7", tsS, "", "GET", "/api/health", now, ErrMissing},
		{"чужой IP", "198.51.100.1", tsS, good, "GET", "/api/health", now, ErrSignature},
		{"чужой метод", "203.0.113.7", tsS, good, "POST", "/api/health", now, ErrSignature},
		{"чужой путь", "203.0.113.7", tsS, good, "GET", "/api/other", now, ErrSignature},
		{"другой секрет", "203.0.113.7", tsS, Sign([]byte("another-secret-another-secret-1234"), ts, "203.0.113.7", "GET", "/api/health"), "GET", "/api/health", now, ErrSignature},
		{"не hex", "203.0.113.7", tsS, "zz", "GET", "/api/health", now, ErrSignature},
		{"мусорный IP", "not-an-ip", tsS, good, "GET", "/api/health", now, ErrBadIP},
		{"мусорное время", "203.0.113.7", "yesterday", good, "GET", "/api/health", now, ErrBadTime},
		{"просрочено", "203.0.113.7", tsS, good, "GET", "/api/health", now.Add(MaxSkew + time.Second), ErrStale},
		{"из будущего", "203.0.113.7", tsS, good, "GET", "/api/health", now.Add(-MaxSkew - time.Second), ErrStale},
	}
	for _, c := range cases {
		_, err := Verify(secret, c.now, c.ip, c.ts, c.sig, c.method, c.uri)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: получено %v, ожидалось %v", c.name, err, c.want)
		}
	}
}

func TestVerifyEdgeOfSkewAccepted(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ts := now.Unix()
	sig := Sign(secret, ts, "203.0.113.7", "GET", "/")
	if _, err := Verify(secret, now.Add(MaxSkew), "203.0.113.7", strconv.FormatInt(ts, 10), sig, "GET", "/"); err != nil {
		t.Fatalf("на границе допуска должно проходить: %v", err)
	}
}

// Эталонный вектор посчитан независимо (openssl dgst -sha256 -hmac и PHP hash_hmac).
// Тот же вектор проверяет PHP-прокси в frontend/public/api/tests/. Если меняется
// формат подписи — обновить обе стороны и этот вектор.
func TestSignKnownVector(t *testing.T) {
	got := Sign(secret, 1_800_000_000, "203.0.113.7", "GET", "/api/health?x=1")
	const want = "cdc60051dec3164a78e4c4620cb720255b57da5068f1f8e269793c0310e9b777"
	if got != want {
		t.Fatalf("подпись %s, ожидалась %s", got, want)
	}
}

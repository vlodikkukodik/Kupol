package sites

import (
	"strings"
	"testing"
	"time"
)

func TestParseCertInfo(t *testing.T) {
	good := "not_before=2026-09-01T00:00:00Z\nnot_after=2026-11-30T00:00:00Z\nissuer=Let's Encrypt (R11)\nnames=a.example.com,www.a.example.com\n"
	c := parseCertInfo([]byte(good))
	if c == nil || c.Issuer != "Let's Encrypt (R11)" || len(c.Names) != 2 || c.NotAfter.Year() != 2026 || c.NotAfter.Month() != 11 {
		t.Fatalf("%+v", c)
	}
	for name, data := range map[string]string{
		"пусто":            "",
		"мусор":            "\x00\x01 not a file",
		"нет окончания":    "not_before=2026-09-01T00:00:00Z\n",
		"нет начала":       "not_after=2026-11-30T00:00:00Z\n",
		"плохая дата":      "not_before=yesterday\nnot_after=tomorrow\n",
		"окончание раньше": "not_before=2026-11-30T00:00:00Z\nnot_after=2026-09-01T00:00:00Z\n",
	} {
		if parseCertInfo([]byte(data)) != nil {
			t.Errorf("%s: файл принят", name)
		}
	}
	// Недоверенные значения очищаются, а списки ограничены.
	evil := "not_before=2026-09-01T00:00:00Z\nnot_after=2026-11-30T00:00:00Z\nissuer=Evil\x1b[31m Org=x\x00\nnames=" + strings.Repeat("a.example.com,", 40) + "\n"
	e := parseCertInfo([]byte(evil))
	if e == nil || strings.ContainsAny(e.Issuer, "\x1b\x00=") || len(e.Names) != 20 {
		t.Fatalf("%+v", e)
	}
	if e.Names == nil || parseCertInfo([]byte(good)).Names == nil {
		t.Fatal("names должен быть списком, а не null")
	}
	long := "not_before=2026-09-01T00:00:00Z\nnot_after=2026-11-30T00:00:00Z\nissuer=" + strings.Repeat("я", 500) + "\n"
	if l := parseCertInfo([]byte(long)); l == nil || len(l.Issuer) > 130 {
		t.Fatalf("издатель не обрезан: %d", len(l.Issuer))
	}
}

func TestDaysLeft(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	for off, want := range map[time.Duration]int{
		30 * 24 * time.Hour: 30, 30*24*time.Hour + 23*time.Hour: 30, 23 * time.Hour: 0, 0: 0, -1 * time.Hour: -1, -25 * time.Hour: -2, -48 * time.Hour: -2,
	} {
		if got := (CertInfo{NotAfter: now.Add(off)}).DaysLeft(now); got != want {
			t.Errorf("через %v: %d, ожидали %d", off, got, want)
		}
	}
}

func TestRenewAvailableAt(t *testing.T) {
	if RenewAvailableAt(nil) != nil {
		t.Fatal("нет заявки — можно сразу")
	}
	old := time.Now().Add(-RenewCooldown - time.Minute)
	if RenewAvailableAt(&old) != nil {
		t.Fatal("после паузы можно")
	}
	recent := time.Now().Add(-time.Hour)
	at := RenewAvailableAt(&recent)
	if at == nil || !at.After(time.Now()) || at.After(time.Now().Add(RenewCooldown)) {
		t.Fatalf("время следующей возможности: %v", at)
	}
}

func TestCertInfoRefusesHostileNames(t *testing.T) {
	s := &Service{certsDir: t.TempDir()}
	for _, h := range []string{"", "../x", "a/b", `a\b`, ".hidden", "a\x00b"} {
		if s.CertInfo(h) != nil {
			t.Errorf("%q", h)
		}
	}
	if (&Service{}).CertInfo("a.example.com") != nil {
		t.Fatal("без каталога сертификатов сведений нет")
	}
}

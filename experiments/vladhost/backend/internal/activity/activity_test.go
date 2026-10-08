package activity

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"vladhost/internal/auth"
	"vladhost/internal/testdb"
)

var bg = context.Background()

type env struct {
	t    *testing.T
	svc  *Service
	auth *auth.Service
	john auth.User
	mary auth.User
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db := testdb.Open(t)
	e := &env{t: t, svc: New(db), auth: auth.NewService(db, []byte(strings.Repeat("s", 32)), time.Minute, time.Hour)}
	mk := func(prefix string) auth.User {
		name := fmt.Sprintf("%s%d", prefix, time.Now().UnixNano()%1_000_000_000)
		u, err := e.auth.CreateAdmin(bg, name+"@example.com", name, "password123")
		if err != nil {
			t.Fatal(err)
		}
		return *u
	}
	e.john, e.mary = mk("jo"), mk("ma")
	return e
}

func (e *env) list(u auth.User, category string) []Event {
	e.t.Helper()
	ev, _, err := e.svc.List(bg, u.ID, category, 0, MaxLimit)
	if err != nil {
		e.t.Fatal(err)
	}
	return ev
}

func TestCategories(t *testing.T) {
	for kind, want := range map[string]string{
		"auth.login": CatSecurity, "auth.password_change": CatSecurity, "profile.update": CatSecurity, "ssh.key_add": CatSecurity, "shell.change": CatSecurity, "admin.invite": CatSecurity,
		"site.create": CatSites, "files.change": CatSites, "domain.add": CatSites, "backup.restore": CatSites, "cert.renew": CatSites, "runtime.set": CatSites, "cms.install": CatSites, "cron.create": CatSites,
		"ftp.enable": CatAccess, "ftp.password": CatAccess,
		"db.create": CatServices, "mail.domain_add": CatServices, "dns.zone_add": CatServices,
		"other": CatOther, "weird.thing": CatOther,
	} {
		if got := CategoryOf(kind); got != want {
			t.Errorf("%s: %s, ждали %s", kind, got, want)
		}
	}
}

func TestRecordAndList(t *testing.T) {
	e := newEnv(t)
	e.svc.Record(bg, Input{UserID: e.john.ID, Kind: "auth.login", IP: "203.0.113.5", UserAgent: "Mozilla/5.0"})
	e.svc.Record(bg, Input{UserID: e.john.ID, Kind: "site.create", Target: "blog.john.vladinc.ru", IP: "203.0.113.5"})
	e.svc.Record(bg, Input{UserID: e.mary.ID, Kind: "ftp.enable", Target: "shop.mary.vladinc.ru", IP: "198.51.100.7"})

	ev := e.list(e.john, "")
	if len(ev) != 2 || ev[0].Kind != "site.create" || ev[1].Kind != "auth.login" {
		t.Fatalf("свежие сверху: %+v", ev)
	}
	if ev[0].Target != "blog.john.vladinc.ru" || ev[0].Category != CatSites || ev[0].Count != 1 || ev[0].IP != "203.0.113.5" || ev[1].UserAgent != "Mozilla/5.0" {
		t.Fatalf("%+v", ev)
	}
	// пользователь видит только своё
	if m := e.list(e.mary, ""); len(m) != 1 || m[0].Kind != "ftp.enable" {
		t.Fatalf("%+v", m)
	}
	// фильтр по виду
	if s := e.list(e.john, CatSecurity); len(s) != 1 || s[0].Kind != "auth.login" {
		t.Fatalf("%+v", s)
	}
	if a := e.list(e.john, CatAccess); len(a) != 0 {
		t.Fatalf("%+v", a)
	}
}

func TestInvalidAndOversizedInputIsHandled(t *testing.T) {
	e := newEnv(t)
	for _, kind := range []string{"", "Auth.Login", "auth login", "auth.login;drop", "1abc", "a.b.c", "x'y"} {
		e.svc.Record(bg, Input{UserID: e.john.ID, Kind: kind})
	}
	e.svc.Record(bg, Input{UserID: 0, Kind: "auth.login"})
	e.svc.Record(bg, Input{UserID: -5, Kind: "auth.login"})
	if ev := e.list(e.john, ""); len(ev) != 0 {
		t.Fatalf("некорректные события не пишутся: %+v", ev)
	}
	var nilSvc *Service
	nilSvc.Record(bg, Input{UserID: e.john.ID, Kind: "auth.login"}) // выключенный журнал не падает

	e.svc.Record(bg, Input{UserID: e.john.ID, Kind: "site.create", Target: strings.Repeat("я", 500) + "\n\x00тест", IP: strings.Repeat("1", 100), UserAgent: strings.Repeat("a", 500) + "\r\n"})
	ev := e.list(e.john, "")
	if len(ev) != 1 || len([]rune(ev[0].Target)) != 200 || len(ev[0].IP) != 45 || len(ev[0].UserAgent) != 200 || strings.ContainsAny(ev[0].Target+ev[0].UserAgent, "\n\r\x00") {
		t.Fatalf("длина и управляющие символы: %d %d %d", len([]rune(ev[0].Target)), len(ev[0].IP), len(ev[0].UserAgent))
	}
}

func TestFrequentEventsAreCoalesced(t *testing.T) {
	e := newEnv(t)
	rec := func(kind, target, ip string) {
		e.svc.Record(bg, Input{UserID: e.john.ID, Kind: kind, Target: target, IP: ip})
	}
	for range 5 {
		rec("files.change", "blog.john.vladinc.ru", "203.0.113.5")
	}
	ev := e.list(e.john, "")
	if len(ev) != 1 || ev[0].Count != 5 || !ev[0].UpdatedAt.After(ev[0].CreatedAt.Add(-time.Second)) {
		t.Fatalf("одно слияние: %+v", ev)
	}
	// другой сайт, другой адрес или другой вид — отдельные строки
	rec("files.change", "shop.john.vladinc.ru", "203.0.113.5")
	rec("files.change", "blog.john.vladinc.ru", "198.51.100.9")
	rec("auth.login_failed", "", "203.0.113.5")
	rec("auth.login_failed", "", "203.0.113.5")
	rec("auth.login_failed", "", "203.0.113.5")
	if ev = e.list(e.john, ""); len(ev) != 4 {
		t.Fatalf("%+v", ev)
	}
	// обычные события не сливаются никогда
	for range 3 {
		rec("auth.login", "", "203.0.113.5")
	}
	if ev = e.list(e.john, CatSecurity); len(ev) != 4 { // три входа и одна строка неудачных
		t.Fatalf("%+v", ev)
	}
	// после окна слияния — новая строка
	e.svc.db.Exec("UPDATE account_events SET updated_at = ? WHERE kind = 'files.change'", time.Now().Add(-2*CoalesceWin))
	rec("files.change", "blog.john.vladinc.ru", "203.0.113.5")
	if ev = e.list(e.john, CatSites); len(ev) != 4 {
		t.Fatalf("новая строка после окна: %+v", ev)
	}
	// слияние не пересекает пользователей
	e.svc.Record(bg, Input{UserID: e.mary.ID, Kind: "files.change", Target: "blog.john.vladinc.ru", IP: "203.0.113.5"})
	if m := e.list(e.mary, ""); len(m) != 1 || m[0].Count != 1 {
		t.Fatalf("%+v", m)
	}
}

func TestPagination(t *testing.T) {
	e := newEnv(t)
	for i := range 25 {
		e.svc.Record(bg, Input{UserID: e.john.ID, Kind: "site.create", Target: fmt.Sprintf("s%02d", i)})
	}
	var seen []string
	var before int64
	pages := 0
	for {
		ev, next, err := e.svc.List(bg, e.john.ID, "", before, 10)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, x := range ev {
			seen = append(seen, x.Target)
		}
		if next == 0 {
			break
		}
		before = next
	}
	if pages != 3 || len(seen) != 25 || seen[0] != "s24" || seen[24] != "s00" {
		t.Fatalf("страниц %d, записей %d, %v", pages, len(seen), seen)
	}
	if ev, next, _ := e.svc.List(bg, e.john.ID, "", 0, 1000); len(ev) != 25 || next != 0 {
		t.Fatalf("предел размера страницы: %d %d", len(ev), next)
	}
	if ev, _, _ := e.svc.List(bg, e.john.ID, "", 0, 0); len(ev) != 25 {
		t.Fatalf("размер по умолчанию: %d", len(ev))
	}
}

func TestPruneRemovesOldAndCapsPerUser(t *testing.T) {
	e := newEnv(t)
	e.svc.Record(bg, Input{UserID: e.john.ID, Kind: "auth.login"})
	e.svc.Record(bg, Input{UserID: e.john.ID, Kind: "site.create", Target: "old"})
	e.svc.db.Exec("UPDATE account_events SET created_at = ? WHERE target = 'old'", time.Now().Add(-Retention-time.Hour))
	// у Mary слишком много строк
	if err := e.svc.db.Exec(`INSERT INTO account_events (user_id, kind, category, target, created_at, updated_at)
		SELECT ?, 'site.create', 'sites', 'n' || g, now(), now() FROM generate_series(1, ?) g`, e.mary.ID, MaxPerUser+37).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Prune(bg); err != nil {
		t.Fatal(err)
	}
	if ev := e.list(e.john, ""); len(ev) != 1 || ev[0].Kind != "auth.login" {
		t.Fatalf("устаревшее удалено: %+v", ev)
	}
	var n int64
	e.svc.db.Model(&Event{}).Where("user_id = ?", e.mary.ID).Count(&n)
	if n != MaxPerUser {
		t.Fatalf("осталось %d, предел %d", n, MaxPerUser)
	}
	// остались самые свежие
	var oldest string
	e.svc.db.Model(&Event{}).Where("user_id = ?", e.mary.ID).Order("id").Limit(1).Pluck("target", &oldest)
	if oldest != "n38" {
		t.Fatalf("самая старая из оставшихся: %s", oldest)
	}
}

func TestEventsDisappearWithTheUser(t *testing.T) {
	e := newEnv(t)
	e.svc.Record(bg, Input{UserID: e.john.ID, Kind: "auth.login"})
	if err := e.svc.db.Exec("DELETE FROM users WHERE id = ?", e.john.ID).Error; err != nil {
		t.Fatal(err)
	}
	var n int64
	e.svc.db.Model(&Event{}).Where("user_id = ?", e.john.ID).Count(&n)
	if n != 0 {
		t.Fatalf("события удалённого пользователя: %d", n)
	}
}

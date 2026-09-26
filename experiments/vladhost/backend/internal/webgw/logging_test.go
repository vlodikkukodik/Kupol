package webgw_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vladhost/internal/sitelog"
	"vladhost/internal/sitestats"
	"vladhost/internal/webgw"
)

// withLogs включает журналы у шлюза фикстуры и возвращает каталог журналов.
func (f *fixture) withLogs() string {
	f.t.Helper()
	dir := filepath.Join(f.t.TempDir(), "logs")
	dom := filepath.Join(f.base, "_domains")
	if err := os.MkdirAll(dom, 0o755); err != nil {
		f.t.Fatal(err)
	}
	f.h = webgw.New(webgw.Options{Root: f.base, BaseDomain: "vladinc.ru", DomainsDir: dom, LogDir: dir})
	return dir
}

func readLog(t *testing.T, dir, kind string) []sitelog.Entry {
	t.Helper()
	p, err := sitelog.Read(dir, host, sitelog.Query{Kind: kind, Limit: sitelog.MaxLimit})
	if err != nil {
		t.Fatal(err)
	}
	return p.Entries
}

func TestAccessLogRecordsRequests(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "<h1>hello</h1>", ".htaccess": "Redirect 301 /old /index.html\n"})
	dir := f.withLogs()

	f.get("/", "User-Agent", "test-agent/1.0", "Referer", "https://example.org/from")
	f.get("/missing?x=1")
	f.get("/old")
	f.do("POST", "/")
	f.get("/.htpasswd")

	got := readLog(t, dir, sitelog.KindAccess) // новые сверху
	if len(got) != 5 {
		t.Fatalf("записей %d: %+v", len(got), got)
	}
	want := []struct {
		method, path string
		status       int
	}{{"GET", "/.htpasswd", 403}, {"POST", "/", 405}, {"GET", "/old", 301}, {"GET", "/missing?x=1", 404}, {"GET", "/", 200}}
	for i, w := range want {
		e := got[i]
		if e.Method != w.method || e.Path != w.path || e.Status != w.status || e.Host != host || e.IP != "203.0.113.7" {
			t.Errorf("запись %d: %+v, ожидали %+v", i, e, w)
		}
	}
	first := got[4]
	if first.UA != "test-agent/1.0" || first.Referer != "https://example.org/from" || first.Bytes != int64(len("<h1>hello</h1>")) {
		t.Errorf("заголовки и размер: %+v", first)
	}
}

func TestErrorLogRecordsRefusals(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "x"})
	dir := f.withLogs()
	f.get("/nope.html")
	f.get("/.git/config")
	f.do("DELETE", "/")
	f.get("/index.html") // успех в журнал ошибок не попадает

	got := readLog(t, dir, sitelog.KindError)
	codes := map[string]string{}
	for _, e := range got {
		codes[e.Code] = e.Detail
	}
	if len(got) != 3 || codes["not_found"] == "" || codes["method_not_allowed"] != "/" {
		t.Fatalf("ошибки: %+v", got)
	}
}

func TestLogUsesSiteAddressForCustomDomain(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "x"})
	dir := f.withLogs()
	bind(t, filepath.Join(f.base, "_domains"), "example.com", host)
	f.doHost("example.com", "GET", "/")
	got := readLog(t, dir, sitelog.KindAccess)
	if len(got) != 1 || got[0].Host != "example.com" {
		t.Fatalf("журнал сайта должен содержать запрос по своему домену: %+v", got)
	}
}

func TestUnknownHostsAreNotLogged(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "x"})
	dir := f.withLogs()
	// Сканеры стучатся по чужим именам: журналы под них не создаются (иначе каталог разрастался бы без предела).
	for _, h := range []string{"nosuch.john.vladinc.ru", "evil.example.com", "../..", "a/b.john.vladinc.ru"} {
		f.doHost(h, "GET", "/")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 0 {
		t.Fatalf("созданы журналы: %v", ents)
	}
}

func TestNoLogDirMeansNoLogs(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "x"})
	mustStatus(t, f.get("/"), 200) // без LogDir шлюз работает как раньше
}

func TestMaintainLogsRemovesGoneSites(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "x"})
	dir := f.withLogs()
	f.get("/")
	gone := "old.john.vladinc.ru"
	w, err := sitelog.NewWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(gone, sitelog.KindAccess, sitelog.Entry{T: time.Now(), Path: "/"})
	w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.h.MaintainLogs(ctx, time.Hour)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, gone)); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("журнал удалённого сайта не убран")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(dir, host, "access.log")); err != nil {
		t.Fatal("журнал живого сайта удалён")
	}
}

func TestMaintainLogsKeepsEverythingWhenSitesRootIsMissing(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "x"})
	dir := f.withLogs()
	f.get("/")
	if err := os.RemoveAll(f.base); err != nil { // как если бы том с сайтами не смонтировался
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.h.MaintainLogs(ctx, time.Hour)
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, host, "access.log")); err != nil {
		t.Fatalf("журналы удалены при недоступном каталоге сайтов: %v", err)
	}
}

func TestStatsAreCountedAndSavedForSites(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "<h1>hi</h1>", "about.html": "a", "s.css": "b{}"})
	dir := f.withLogs()
	human := []string{"User-Agent", "Mozilla/5.0 Firefox/130", "Referer", "https://news.example.org/post"}
	f.get("/", human...)
	f.get("/about.html", human...)
	f.get("/s.css", human...)
	f.get("/nope", human...)
	f.get("/", "User-Agent", "Googlebot/2.1")

	done := make(chan struct{})
	finished := make(chan struct{})
	go func() { f.h.RunStats(done, time.Hour); close(finished) }()
	close(done) // при остановке счётчики сохраняются на диск
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("RunStats не завершился")
	}

	s, err := sitestats.Read(dir, host, 7, time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tot := s.Total
	if tot.Hits != 5 || tot.Bots != 1 || tot.Pages != 2 || tot.Visitors != 1 || tot.S2 != 4 || tot.S4 != 1 {
		t.Fatalf("итоги: %+v", tot)
	}
	if len(s.TopRefs) != 1 || s.TopRefs[0].Key != "news.example.org" {
		t.Fatalf("источники: %+v", s.TopRefs)
	}
	if tot.Bytes < int64(len("<h1>hi</h1>")) {
		t.Fatalf("трафик: %d", tot.Bytes)
	}
}

func TestMaintainLogsForgetsStatsOfGoneSites(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "x"})
	dir := f.withLogs()
	f.get("/", "User-Agent", "Mozilla/5.0")
	done := make(chan struct{})
	go f.h.RunStats(done, 20*time.Millisecond)
	defer close(done)
	time.Sleep(100 * time.Millisecond) // счётчики сохранены
	if err := os.RemoveAll(filepath.Join(f.base, host)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.h.MaintainLogs(ctx, time.Hour)
	time.Sleep(300 * time.Millisecond) // очистка убрала каталог, а новые сбросы его не воссоздают
	if _, err := os.Stat(filepath.Join(dir, host)); err == nil {
		t.Fatal("каталог статистики удалённого сайта остался или воссоздан")
	}
}

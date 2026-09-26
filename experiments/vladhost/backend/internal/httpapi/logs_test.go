package httpapi_test

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"vladhost/internal/sitelog"
)

type logsBody struct {
	Entries []struct {
		T      time.Time `json:"t"`
		Path   string    `json:"p"`
		Status int       `json:"s"`
		Code   string    `json:"code"`
	} `json:"entries"`
	HasMore bool `json:"has_more"`
}

func TestLogsDisabledWithoutConfig(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")
	w := e.do("GET", "/api/sites/"+itoa(id)+"/logs", nil, john)
	if w.Code != 409 || decode[errBody](t, w).Error.Code != "logs_unavailable" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if !strings.Contains(e.do("GET", "/api/sites", nil, john).Body.String(), `"logs_available":false`) {
		t.Fatal("список сайтов должен сообщать, что журналы недоступны")
	}
}

func TestLogsEndpoints(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	e.sites.ConfigureLogs(dir)
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	id, host := e.createSite(john, "blog")
	base := "/api/sites/" + itoa(id) + "/logs"

	if !strings.Contains(e.do("GET", "/api/sites", nil, john).Body.String(), `"logs_available":true`) {
		t.Fatal("список сайтов должен сообщать, что журналы доступны")
	}

	w, err := sitelog.NewWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	now := time.Now().UTC()
	acc := func(off time.Duration, status int, path string) sitelog.Entry {
		return sitelog.Entry{T: now.Add(off), IP: "203.0.113.7", Host: host, Method: "GET", Path: path, Status: status}
	}
	w.Write(host, sitelog.KindAccess, acc(-time.Hour, 200, "/before-site-existed")) // до создания сайта: прежний владелец адреса
	w.Write(host, sitelog.KindAccess, acc(time.Second, 200, "/index.html"))
	w.Write(host, sitelog.KindAccess, acc(2*time.Second, 404, "/missing"))
	w.Write(host, sitelog.KindAccess, acc(3*time.Second, 500, "/boom"))
	w.Write(host, sitelog.KindError, sitelog.Entry{T: now.Add(2 * time.Second), IP: "203.0.113.7", Host: host, Path: "/missing", Code: "not_found", Detail: "/missing"})

	got := decode[logsBody](t, e.do("GET", base, nil, john))
	if len(got.Entries) != 3 || got.Entries[0].Path != "/boom" || got.Entries[2].Path != "/index.html" {
		t.Fatalf("новые сверху и без событий до создания сайта: %+v", got)
	}

	if got := decode[logsBody](t, e.do("GET", base+"?status=4xx", nil, john)); len(got.Entries) != 1 || got.Entries[0].Status != 404 {
		t.Fatalf("фильтр 4xx: %+v", got)
	}
	if got := decode[logsBody](t, e.do("GET", base+"?q=INDEX", nil, john)); len(got.Entries) != 1 {
		t.Fatalf("поиск без учёта регистра: %+v", got)
	}
	if got := decode[logsBody](t, e.do("GET", base+"?kind=error", nil, john)); len(got.Entries) != 1 || got.Entries[0].Code != "not_found" {
		t.Fatalf("журнал ошибок: %+v", got)
	}

	// Постраничная выдача: limit + before.
	page1 := decode[logsBody](t, e.do("GET", base+"?limit=2", nil, john))
	if len(page1.Entries) != 2 || !page1.HasMore {
		t.Fatalf("страница 1: %+v", page1)
	}
	next := base + "?limit=2&before=" + url.QueryEscape(page1.Entries[1].T.Format(time.RFC3339Nano))
	page2 := decode[logsBody](t, e.do("GET", next, nil, john))
	if len(page2.Entries) != 1 || page2.HasMore || page2.Entries[0].Path != "/index.html" {
		t.Fatalf("страница 2: %+v", page2)
	}

	// Скачивание: обычный текст от старых событий к новым.
	d := e.do("GET", base+"/download", nil, john)
	if d.Code != 200 || !strings.HasPrefix(d.Header().Get("Content-Type"), "text/plain") ||
		!strings.Contains(d.Header().Get("Content-Disposition"), `attachment; filename="site-`+itoa(id)+`-access.log"`) {
		t.Fatalf("скачивание: %d %v", d.Code, d.Header())
	}
	lines := strings.Split(strings.TrimSpace(d.Body.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "/index.html") || !strings.Contains(lines[2], "/boom") || strings.Contains(d.Body.String(), "before-site") {
		t.Fatalf("содержимое: %q", d.Body)
	}

	// Некорректные параметры.
	for _, q := range []string{"?kind=other", "?status=404", "?status=6xx", "?limit=abc", "?limit=-1", "?before=yesterday", "?q=" + strings.Repeat("a", 101)} {
		w := e.do("GET", base+q, nil, john)
		if w.Code != 422 || decode[errBody](t, w).Error.Code != "validation.log_query" {
			t.Errorf("%s → %d %s", q, w.Code, w.Body)
		}
	}
	if w := e.do("GET", base+"/download?kind=other", nil, john); w.Code != 422 || strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("скачивание с неверным видом: %d %v", w.Code, w.Header())
	}

	// Чужой сайт и вход без токена.
	if w := e.do("GET", base, nil, mary); w.Code != 404 {
		t.Errorf("чужой сайт: %d", w.Code)
	}
	if w := e.do("GET", base+"/download", nil, mary); w.Code != 404 {
		t.Errorf("скачивание чужого: %d", w.Code)
	}
	if w := e.do("GET", base, nil, ""); w.Code != 401 {
		t.Errorf("без токена: %d", w.Code)
	}
}

func TestLogsDoNotLeakBetweenSitesWithLimit(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	e.sites.ConfigureLogs(dir)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, host := e.createSite(john, "blog")
	w, _ := sitelog.NewWriter(dir)
	defer w.Close()
	// Журнал другого адреса лежит рядом, но сайт читает только свой каталог.
	w.Write("other.john.vladinc.ru", sitelog.KindAccess, sitelog.Entry{T: time.Now().Add(time.Second), Path: "/secret", Status: 200})
	w.Write(host, sitelog.KindAccess, sitelog.Entry{T: time.Now().Add(time.Second), Path: "/mine", Status: 200})
	got := decode[logsBody](t, e.do("GET", "/api/sites/"+itoa(id)+"/logs?limit=9999", nil, john))
	if len(got.Entries) != 1 || got.Entries[0].Path != "/mine" {
		t.Fatalf("%+v", got)
	}
}

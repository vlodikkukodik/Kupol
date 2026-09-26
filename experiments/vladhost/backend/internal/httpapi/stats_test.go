package httpapi_test

import (
	"testing"
	"time"

	"vladhost/internal/sitestats"
)

type statsBody struct {
	Days  []struct{ Date string } `json:"days"`
	Total struct {
		Hits, Pages, Visitors, Bots int
		Bytes                       int64
		S2, S4                      int
	} `json:"total"`
	TopPages []struct {
		Key   string
		Count int
	} `json:"top_pages"`
	TopRefs []struct{ Key string } `json:"top_refs"`
}

func TestStatsEndpoint(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	id, host := e.createSite(john, "blog")
	url := "/api/sites/" + itoa(id) + "/stats"

	// Без настроенных журналов статистики нет.
	if w := e.do("GET", url, nil, john); w.Code != 409 || decode[errBody](t, w).Error.Code != "logs_unavailable" {
		t.Fatalf("без настройки: %d %s", w.Code, w.Body)
	}

	dir := t.TempDir()
	e.sites.ConfigureLogs(dir)
	// Пустая статистика: нули, а не ошибка; по умолчанию 30 суток.
	got := decode[statsBody](t, e.do("GET", url, nil, john))
	if len(got.Days) != 30 || got.Total.Hits != 0 {
		t.Fatalf("пусто: %+v", got)
	}

	agg, err := sitestats.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ua := "Mozilla/5.0 Firefox/130"
	for _, h := range []sitestats.Hit{
		{T: now, IP: "1.1.1.1", Method: "GET", Path: "/", UA: ua, Host: host, Status: 200, Bytes: 500, Referer: "https://example.org/x"},
		{T: now, IP: "2.2.2.2", Method: "GET", Path: "/", UA: ua, Host: host, Status: 200, Bytes: 500},
		{T: now, IP: "2.2.2.2", Method: "GET", Path: "/gone", UA: ua, Host: host, Status: 404, Bytes: 10},
		{T: now, IP: "9.9.9.9", Method: "GET", Path: "/", UA: "Googlebot/2.1", Host: host, Status: 200, Bytes: 500},
		{T: now.AddDate(0, 0, -400), IP: "1.1.1.1", Method: "GET", Path: "/ancient", UA: ua, Host: host, Status: 200}, // вне периода
	} {
		agg.Record(host, h)
	}
	agg.Close()

	for _, q := range []string{"", "?days=7", "?days=30", "?days=90"} {
		got = decode[statsBody](t, e.do("GET", url+q, nil, john))
		if got.Total.Hits != 4 || got.Total.Pages != 2 || got.Total.Visitors != 2 || got.Total.Bots != 1 || got.Total.S4 != 1 || got.Total.Bytes != 1510 {
			t.Fatalf("%q итоги: %+v", q, got.Total)
		}
	}
	if len(got.TopPages) != 1 || got.TopPages[0].Key != "/" || got.TopPages[0].Count != 2 || len(got.TopRefs) != 1 || got.TopRefs[0].Key != "example.org" {
		t.Fatalf("рейтинги: %+v", got)
	}
	if n := len(decode[statsBody](t, e.do("GET", url+"?days=7", nil, john)).Days); n != 7 {
		t.Fatalf("дней %d", n)
	}

	for _, q := range []string{"?days=14", "?days=0", "?days=-7", "?days=abc", "?days=365"} {
		if w := e.do("GET", url+q, nil, john); w.Code != 422 || decode[errBody](t, w).Error.Code != "validation.stats_period" {
			t.Errorf("%s → %d %s", q, w.Code, w.Body)
		}
	}
	if w := e.do("GET", url, nil, mary); w.Code != 404 {
		t.Errorf("чужой сайт: %d", w.Code)
	}
	if w := e.do("GET", url, nil, ""); w.Code != 401 {
		t.Errorf("без токена: %d", w.Code)
	}
}

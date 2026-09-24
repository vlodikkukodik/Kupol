package sitestats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const site = "blog.john.vladinc.ru"

var (
	day1  = time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	human = "Mozilla/5.0 (X11; Linux x86_64) Firefox/130.0"
)

func newAgg(t *testing.T) (*Aggregator, string) {
	t.Helper()
	dir := t.TempDir()
	a, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a, dir
}

func hit(at time.Time, ip, path string, status int) Hit {
	return Hit{T: at, IP: ip, Method: "GET", Path: path, UA: human, Host: site, Status: status, Bytes: 100}
}

func read(t *testing.T, dir string, days int, since, now time.Time) Summary {
	t.Helper()
	s, err := Read(dir, site, days, since, now)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCountsRequestsPagesAndStatuses(t *testing.T) {
	a, dir := newAgg(t)
	a.Record(site, hit(day1, "1.1.1.1", "/", 200))
	a.Record(site, hit(day1, "1.1.1.1", "/about.html", 200))
	a.Record(site, hit(day1, "1.1.1.1", "/style.css", 200)) // не страница
	a.Record(site, hit(day1, "2.2.2.2", "/missing", 404))
	a.Record(site, hit(day1, "2.2.2.2", "/old", 301))
	a.Record(site, hit(day1, "3.3.3.3", "/boom", 500))
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	s := read(t, dir, 7, day1.AddDate(0, 0, -30), day1)
	tot := s.Total
	if tot.Hits != 6 || tot.Pages != 2 || tot.Visitors != 3 || tot.S2 != 3 || tot.S3 != 1 || tot.S4 != 1 || tot.S5 != 1 || tot.Bytes != 600 {
		t.Fatalf("итоги: %+v", tot)
	}
	if len(s.Days) != 7 || s.Days[6].Date != "2026-09-20" || s.Days[6].Hits != 6 || s.Days[0].Hits != 0 {
		t.Fatalf("дни: %+v", s.Days)
	}
	if len(s.TopPages) != 2 {
		t.Fatalf("страницы: %+v", s.TopPages)
	}
}

func TestBotsAreCountedSeparately(t *testing.T) {
	a, dir := newAgg(t)
	for _, ua := range []string{"", "Googlebot/2.1", "curl/8.5", "python-requests/2.31", "Mozilla/5.0 (compatible; AhrefsBot/7.0)"} {
		h := hit(day1, "9.9.9.9", "/", 200)
		h.UA = ua
		a.Record(site, h)
	}
	a.Record(site, hit(day1, "1.1.1.1", "/", 200))
	_ = a.Flush()
	tot := read(t, dir, 7, day1.AddDate(0, 0, -1), day1).Total
	if tot.Hits != 6 || tot.Bots != 5 || tot.Pages != 1 || tot.Visitors != 1 {
		t.Fatalf("боты не должны попадать в посетителей и страницы: %+v", tot)
	}
	if IsBot(human) || !IsBot("") {
		t.Fatal("IsBot")
	}
}

func TestIndexPagesAreMerged(t *testing.T) {
	for in, want := range map[string]string{
		"/": "/", "/index.html": "/", "/docs/index.html": "/docs/", "/docs/": "/docs/", "/docs/INDEX.HTM": "/docs/",
		"/a.html?x=1": "/a.html", "noslash": "/noslash",
	} {
		if got := pageKey(in); got != want {
			t.Errorf("pageKey(%q) = %q, ожидали %q", in, got, want)
		}
	}
	a, dir := newAgg(t)
	a.Record(site, hit(day1, "1.1.1.1", "/", 200))
	a.Record(site, hit(day1, "1.1.1.1", "/index.html", 200))
	_ = a.Flush()
	s := read(t, dir, 7, day1.AddDate(0, 0, -1), day1)
	if len(s.TopPages) != 1 || s.TopPages[0] != (Item{"/", 2}) {
		t.Fatalf("%+v", s.TopPages)
	}
}

func TestReferrersUseHostOnlyAndSkipOwn(t *testing.T) {
	a, dir := newAgg(t)
	for _, ref := range []string{"https://Example.org/some/page?q=1", "https://example.org/", "https://" + site + "/", "https://mysite.com/x", "javascript:alert(1)", "ftp://x.org/", "", "%%%"} {
		h := hit(day1, "1.1.1.1", "/", 200)
		h.Referer = ref
		h.Host = "mysite.com" // запрос пришёл по своему домену
		a.Record(site, h)
	}
	_ = a.Flush()
	s := read(t, dir, 7, day1.AddDate(0, 0, -1), day1)
	got := map[string]int{}
	for _, it := range s.TopRefs {
		got[it.Key] = it.Count
	}
	// Свой домен исключён; адрес сайта на нашем домене — это тоже «внешний» источник для запроса по своему домену.
	if got["example.org"] != 2 || got["mysite.com"] != 0 || len(got) != 2 {
		t.Fatalf("источники: %+v", s.TopRefs)
	}
}

func TestUniqueVisitorsPerDayAndAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	a, _ := New(dir)
	a.Record(site, hit(day1, "1.1.1.1", "/", 200))
	a.Record(site, hit(day1, "1.1.1.1", "/x.html", 200))
	a.Record(site, hit(day1, "2.2.2.2", "/", 200))
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	a.Close()

	// Перезапуск шлюза посреди суток: тот же посетитель не считается снова.
	b, _ := New(dir)
	b.Record(site, hit(day1.Add(time.Hour), "1.1.1.1", "/", 200))
	b.Record(site, hit(day1.Add(time.Hour), "3.3.3.3", "/", 200))
	// Следующие сутки: посетитель считается заново.
	day2 := day1.AddDate(0, 0, 1)
	b.Record(site, hit(day2, "1.1.1.1", "/", 200))
	b.Close()

	s := read(t, dir, 7, day1.AddDate(0, 0, -1), day2)
	if s.Days[5].Visitors != 3 || s.Days[6].Visitors != 1 {
		t.Fatalf("посетители по дням: %+v", s.Days)
	}
}

func TestNoIPsOnDisk(t *testing.T) {
	a, dir := newAgg(t)
	a.Record(site, hit(day1, "203.0.113.77", "/", 200))
	_ = a.Flush()
	raw, err := os.ReadFile(filepath.Join(dir, site, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "203.0.113.77") || strings.Contains(string(raw), "203.0.113") {
		t.Fatalf("IP-адрес попал в файл статистики: %s", raw)
	}
}

func TestSetOfVisitorsIsDroppedAtDayChange(t *testing.T) {
	a, dir := newAgg(t)
	a.Record(site, hit(day1, "1.1.1.1", "/", 200))
	a.Record(site, hit(day1.AddDate(0, 0, 1), "2.2.2.2", "/", 200))
	_ = a.Flush()
	raw, _ := os.ReadFile(filepath.Join(dir, site, fileName))
	if strings.Contains(string(raw), `"seen_day":"2026-09-20"`) {
		t.Fatalf("набор прошлых суток должен быть удалён: %s", raw)
	}
	if !strings.Contains(string(raw), `"seen_day":"2026-09-21"`) {
		t.Fatalf("нет набора текущих суток: %s", raw)
	}
}

func TestCapsKeepMapsBounded(t *testing.T) {
	a, dir := newAgg(t)
	for i := 0; i < maxPaths+50; i++ {
		h := hit(day1, "1.1.1.1", fmt.Sprintf("/p%d", i), 200)
		h.Referer = fmt.Sprintf("https://r%d.example.org/", i)
		a.Record(site, h)
	}
	_ = a.Flush()
	raw, _ := os.ReadFile(filepath.Join(dir, site, fileName))
	var f siteFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	d := f.Days["2026-09-20"]
	if len(d.Paths) != maxPaths+1 || d.Paths[OtherKey] != 50 || len(d.Refs) != maxRefs+1 {
		t.Fatalf("страниц %d (прочее %d), источников %d", len(d.Paths), d.Paths[OtherKey], len(d.Refs))
	}
	// «Прочее» в рейтинге идёт последним, даже если оно крупнее остальных.
	a.Record(site, hit(day1, "1.1.1.1", "/p0", 200))
	_ = a.Flush()
	s := read(t, dir, 7, day1.AddDate(0, 0, -1), day1)
	if len(s.TopPages) != topN {
		t.Fatalf("топ %d", len(s.TopPages))
	}
}

func TestRetentionDropsOldDays(t *testing.T) {
	a, dir := newAgg(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	a.Record(site, hit(now.AddDate(0, 0, -RetentionDays-5), "1.1.1.1", "/", 200))
	a.Record(site, hit(now.AddDate(0, 0, -10), "1.1.1.1", "/", 200))
	_ = a.Flush()
	raw, _ := os.ReadFile(filepath.Join(dir, site, fileName))
	if strings.Contains(string(raw), "2026-06-") || !strings.Contains(string(raw), "2026-09-14") {
		t.Fatalf("устаревшие сутки должны быть удалены, свежие нет: %s", raw)
	}
}

func TestReadFillsGapsAndSkipsBeforeSiteCreation(t *testing.T) {
	a, dir := newAgg(t)
	a.Record(site, hit(day1, "1.1.1.1", "/", 200))                     // до создания текущего сайта
	a.Record(site, hit(day1.AddDate(0, 0, 3), "1.1.1.1", "/new", 200)) // после
	_ = a.Flush()
	created := day1.AddDate(0, 0, 2)
	s := read(t, dir, 7, created, day1.AddDate(0, 0, 4))
	if s.Total.Hits != 1 || s.TopPages[0].Key != "/new" {
		t.Fatalf("прежний владелец адреса не должен попадать в статистику: %+v", s)
	}
	if len(s.Days) != 7 {
		t.Fatalf("дней %d", len(s.Days))
	}
}

func TestReadWithoutDataIsZeroNotError(t *testing.T) {
	s := read(t, t.TempDir(), 30, day1, day1)
	if len(s.Days) != 30 || s.Total.Hits != 0 || s.TopPages == nil || s.TopRefs == nil {
		t.Fatalf("%+v", s)
	}
	if _, err := Read(filepath.Join(t.TempDir(), "нет"), site, 7, day1, day1); err != nil {
		t.Fatalf("нет каталога: %v", err)
	}
	if _, err := Read(t.TempDir(), "../evil", 7, day1, day1); err == nil {
		t.Fatal("недопустимый адрес должен отклоняться")
	}
	if !ValidPeriod(7) || !ValidPeriod(30) || !ValidPeriod(90) || ValidPeriod(14) || ValidPeriod(0) || ValidPeriod(365) {
		t.Fatal("ValidPeriod")
	}
}

func TestCorruptFileStartsFresh(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, site), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, site, fileName), []byte(`{"days": {"2026-09-20": `), 0o640); err != nil {
		t.Fatal(err)
	}
	a, _ := New(dir)
	a.Record(site, hit(day1, "1.1.1.1", "/", 200)) // повреждённый файл не должен останавливать счёт
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	a.Close()
	if s := read(t, dir, 7, day1.AddDate(0, 0, -1), day1); s.Total.Hits != 1 {
		t.Fatalf("%+v", s.Total)
	}
}

func TestForgetPreventsRecreatingDeletedSite(t *testing.T) {
	a, dir := newAgg(t)
	a.Record(site, hit(day1, "1.1.1.1", "/", 200))
	_ = a.Flush()
	if err := os.RemoveAll(filepath.Join(dir, site)); err != nil {
		t.Fatal(err)
	}
	a.Forget(site)
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, site)); err == nil {
		t.Fatal("каталог удалённого сайта создан заново")
	}
}

func TestInvalidSiteNamesAreIgnored(t *testing.T) {
	a, dir := newAgg(t)
	for _, bad := range []string{"", "..", "../evil", "a/b", "UPPER.example.com"} {
		a.Record(bad, hit(day1, "1.1.1.1", "/", 200))
	}
	_ = a.Flush()
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("созданы каталоги: %v", ents)
	}
}

func TestConcurrentRecords(t *testing.T) {
	a, dir := newAgg(t)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 200 {
				a.Record(site, hit(day1, fmt.Sprintf("10.0.%d.%d", g, i%50), "/", 200))
				if i%50 == 0 {
					_ = a.Flush()
				}
			}
		})
	}
	wg.Wait()
	_ = a.Flush()
	s := read(t, dir, 7, day1.AddDate(0, 0, -1), day1)
	if s.Total.Hits != 1600 || s.Total.Visitors != 400 {
		t.Fatalf("запросов %d, посетителей %d", s.Total.Hits, s.Total.Visitors)
	}
}

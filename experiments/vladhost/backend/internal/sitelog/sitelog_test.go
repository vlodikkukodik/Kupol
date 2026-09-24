package sitelog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const site = "blog.john.vladinc.ru"

var t0 = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

func acc(i int, status int, path string) Entry {
	return Entry{T: t0.Add(time.Duration(i) * time.Second), IP: "203.0.113.7", Host: site, Method: "GET", Path: path, Status: status, Bytes: 10, Ms: 1, UA: "curl/8"}
}

func newW(t *testing.T) (*Writer, string) {
	t.Helper()
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return w, dir
}

func TestWriteReadNewestFirst(t *testing.T) {
	w, dir := newW(t)
	for i := 0; i < 5; i++ {
		w.Write(site, KindAccess, acc(i, 200, fmt.Sprintf("/p%d", i)))
	}
	page, err := Read(dir, site, Query{Kind: KindAccess, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 3 || !page.HasMore || page.Entries[0].Path != "/p4" || page.Entries[2].Path != "/p2" {
		t.Fatalf("страница: %+v", page)
	}
	// Следующая страница — через Before последнего события.
	next, _ := Read(dir, site, Query{Kind: KindAccess, Limit: 3, Before: page.Entries[2].T})
	if len(next.Entries) != 2 || next.HasMore || next.Entries[0].Path != "/p1" {
		t.Fatalf("вторая страница: %+v", next)
	}
}

func TestAccessAndErrorAreSeparate(t *testing.T) {
	w, dir := newW(t)
	w.Write(site, KindAccess, acc(1, 200, "/"))
	w.Write(site, KindError, Entry{T: t0, IP: "1.1.1.1", Host: site, Path: "/x", Code: "not_found", Detail: "/x"})
	a, _ := Read(dir, site, Query{Kind: KindAccess})
	e, _ := Read(dir, site, Query{Kind: KindError})
	if len(a.Entries) != 1 || len(e.Entries) != 1 || e.Entries[0].Code != "not_found" {
		t.Fatalf("access=%+v error=%+v", a, e)
	}
}

func TestFilters(t *testing.T) {
	w, dir := newW(t)
	w.Write(site, KindAccess, acc(1, 200, "/index.html"))
	w.Write(site, KindAccess, acc(2, 404, "/missing"))
	w.Write(site, KindAccess, acc(3, 301, "/old"))
	w.Write(site, KindAccess, acc(4, 500, "/Boom"))
	count := func(q Query) int {
		q.Kind = KindAccess
		p, err := Read(dir, site, q)
		if err != nil {
			t.Fatal(err)
		}
		return len(p.Entries)
	}
	if n := count(Query{Status: "4xx"}); n != 1 {
		t.Errorf("4xx: %d", n)
	}
	if n := count(Query{Status: "2xx"}); n != 1 {
		t.Errorf("2xx: %d", n)
	}
	if n := count(Query{Text: "boom"}); n != 1 { // без учёта регистра
		t.Errorf("текст: %d", n)
	}
	if n := count(Query{Text: "curl"}); n != 4 { // ищет и по браузеру
		t.Errorf("текст по UA: %d", n)
	}
	if n := count(Query{Since: t0.Add(3 * time.Second)}); n != 2 {
		t.Errorf("since: %d", n)
	}
	if !ValidStatus("") || !ValidStatus("5xx") || ValidStatus("6xx") || ValidStatus("404") {
		t.Error("ValidStatus")
	}
}

func TestRotationKeepsLimitedCopies(t *testing.T) {
	w, dir := newW(t)
	w.SetMaxSize(300)
	for i := 0; i < 60; i++ {
		w.Write(site, KindAccess, acc(i, 200, fmt.Sprintf("/page-%02d", i)))
	}
	names, _ := os.ReadDir(filepath.Join(dir, site))
	if len(names) > Keep+1 {
		t.Fatalf("файлов %d, должно быть не больше %d", len(names), Keep+1)
	}
	for _, n := range names {
		fi, _ := n.Info()
		if fi.Size() > 600 {
			t.Errorf("%s слишком большой: %d", n.Name(), fi.Size())
		}
	}
	// Самое свежее читается первым, порядок между файлами сохранён.
	page, _ := Read(dir, site, Query{Kind: KindAccess, Limit: MaxLimit})
	if page.Entries[0].Path != "/page-59" {
		t.Fatalf("первым должно быть новейшее: %s", page.Entries[0].Path)
	}
	for i := 1; i < len(page.Entries); i++ {
		if !page.Entries[i].T.Before(page.Entries[i-1].T) {
			t.Fatalf("порядок нарушен на %d", i)
		}
	}
	if len(page.Entries) >= 60 {
		t.Fatal("самые старые записи должны быть удалены")
	}
}

func TestInvalidNamesAreRejected(t *testing.T) {
	w, dir := newW(t)
	for _, bad := range []string{"", "..", "../evil", "a/b", ".hidden", "UPPER.example.com", "a..b", "x y", "a\x00b", strings.Repeat("a", 300)} {
		w.Write(bad, KindAccess, acc(1, 200, "/"))
		if _, err := Read(dir, bad, Query{Kind: KindAccess}); err == nil {
			t.Errorf("чтение %q должно быть отклонено", bad)
		}
	}
	w.Write(site, "other", acc(1, 200, "/"))
	if _, err := Read(dir, site, Query{Kind: "other"}); err == nil {
		t.Error("неизвестный вид журнала должен отклоняться")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 0 {
		t.Fatalf("недопустимые имена создали файлы: %v", ents)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil")); err == nil {
		t.Fatal("запись вышла за каталог журналов")
	}
}

func TestNoLogsIsEmptyPageNotError(t *testing.T) {
	dir := t.TempDir()
	p, err := Read(dir, site, Query{Kind: KindAccess})
	if err != nil || len(p.Entries) != 0 || p.Entries == nil {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = Read(filepath.Join(dir, "нет-такого"), site, Query{Kind: KindError})
	if err != nil || p.Entries == nil {
		t.Fatalf("нет каталога: %+v %v", p, err)
	}
}

func TestTornLineIsSkipped(t *testing.T) {
	w, dir := newW(t)
	w.Write(site, KindAccess, acc(1, 200, "/ok"))
	f, err := os.OpenFile(filepath.Join(dir, site, "access.log"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"t":"2026-09-24T12:00:09Z","p":"/tor`) // оборванная запись
	_ = f.Close()
	p, err := Read(dir, site, Query{Kind: KindAccess})
	if err != nil || len(p.Entries) != 1 || p.Entries[0].Path != "/ok" {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestLongFieldsAreClipped(t *testing.T) {
	w, dir := newW(t)
	e := acc(1, 200, "/"+strings.Repeat("я", 2000))
	e.UA = strings.Repeat("u", 5000)
	w.Write(site, KindAccess, e)
	p, _ := Read(dir, site, Query{Kind: KindAccess})
	if len(p.Entries[0].Path) > maxPath || len(p.Entries[0].UA) > maxUA {
		t.Fatalf("длинные поля не обрезаны: %d %d", len(p.Entries[0].Path), len(p.Entries[0].UA))
	}
}

func TestConcurrentWritesDoNotCorrupt(t *testing.T) {
	w, dir := newW(t)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				w.Write(site, KindAccess, acc(i, 200, "/x"))
			}
		}()
	}
	wg.Wait()
	p, _ := Read(dir, site, Query{Kind: KindAccess, Limit: MaxLimit})
	if len(p.Entries) != 500 || !p.HasMore {
		t.Fatalf("прочитано %d, has_more=%v", len(p.Entries), p.HasMore)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, site, "access.log"))
	if n := strings.Count(string(raw), "\n"); n != 800 {
		t.Fatalf("строк в файле %d, ожидалось 800 (каждая запись целой строкой)", n)
	}
}

func TestSweepRemovesOnlyGoneSites(t *testing.T) {
	w, dir := newW(t)
	w.Write("live.john.vladinc.ru", KindAccess, acc(1, 200, "/"))
	w.Write("gone.john.vladinc.ru", KindAccess, acc(1, 200, "/"))
	w.Write("gone.john.vladinc.ru", KindError, Entry{T: t0, Path: "/", Code: "not_found"})
	removed := w.Sweep(func(s string) bool { return s == "live.john.vladinc.ru" })
	if removed != 1 {
		t.Fatalf("удалено %d", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.john.vladinc.ru")); err == nil {
		t.Fatal("журнал ушедшего сайта остался")
	}
	if _, err := os.Stat(filepath.Join(dir, "live.john.vladinc.ru", "access.log")); err != nil {
		t.Fatal("журнал живого сайта удалён")
	}
	// После очистки запись снова работает (открытые дескрипторы не остались висеть).
	w.Write("gone.john.vladinc.ru", KindAccess, acc(2, 200, "/again"))
	p, _ := Read(dir, "gone.john.vladinc.ru", Query{Kind: KindAccess})
	if len(p.Entries) != 1 || p.Entries[0].Path != "/again" {
		t.Fatalf("%+v", p)
	}
}

func TestWriteTextIsChronologicalAndFiltered(t *testing.T) {
	w, dir := newW(t)
	w.SetMaxSize(250)
	for i := 0; i < 12; i++ {
		w.Write(site, KindAccess, acc(i, 200, fmt.Sprintf("/p%02d", i)))
	}
	var b strings.Builder
	if err := WriteText(dir, site, KindAccess, time.Time{}, &b); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) < 4 || !strings.Contains(lines[len(lines)-1], `"/p11"`) {
		t.Fatalf("последней должна быть новейшая: %v", lines)
	}
	// Время в строках возрастает: первая строка старше последней.
	if !strings.Contains(lines[0], "203.0.113.7") || strings.Index(b.String(), `"/p11"`) < strings.Index(b.String(), `"/p10"`) {
		t.Fatalf("порядок: %v", lines)
	}
	var since strings.Builder
	_ = WriteText(dir, site, KindAccess, t0.Add(11*time.Second), &since)
	if strings.Count(since.String(), "\n") != 1 {
		t.Fatalf("since: %q", since.String())
	}
	e := Entry{T: t0, IP: "1.2.3.4", Host: site, Path: "/a b", Code: "not_found", Detail: `x"y`}
	if got := e.Text(KindError); !strings.Contains(got, "[not_found]") || !strings.Contains(got, `"x\"y"`) {
		t.Fatalf("экранирование в тексте: %s", got)
	}
}

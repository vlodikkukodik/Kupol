package documents

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// seedCatalog загружает набор документов и возвращает окружение. Публикации идут с шагом в час.
func seedCatalog(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	docs := []string{
		// объекты: код, название, статус, уровень, свойства
		`{"code":"О-2","type":"object","title":"Гамма","status":"published","level":0,"composed":{"year":1981,"month":6},
		  "props":{"danger_class":2,"deviation_points":15,"department":"ОТД-2","category":"person","containment_status":"contained"}}`,
		`{"code":"О-10","type":"object","title":"Альфа","status":"published","level":0,"composed":{"year":1979,"month":3,"day":14},
		  "props":{"danger_class":4,"deviation_points":80,"department":"ОТД-2","category":"entity","containment_status":"studying"}}`,
		`{"code":"О-3","type":"object","title":"Бета","status":"published","level":3,"composed":{"year":1979},
		  "props":{"danger_class":3,"department":"ОБ-14","category":"place","containment_status":"lost"}}`,
		`{"code":"О-11","type":"object","title":"Дельта","status":"published","level":5,"composed":{"year":1985,"month":1},
		  "props":{"danger_class":5,"deviation_points":300,"category":"entity","containment_status":"destroyed"}}`,
		`{"code":"О-12","type":"object","title":"Без свойств","status":"published","level":0,"composed":{"year":1990}}`,
		`{"code":"О-0","type":"object","title":"Праисточник","status":"published","level":7,"composed":{"year":1974},"props":{"danger_class":5}}`,
		`{"code":"О-13","type":"object","title":"Черновой","status":"draft","level":0,"composed":{"year":1979},"props":{"danger_class":1}}`,
		`{"code":"О-14","type":"object","title":"В архиве","status":"archived","level":0,"composed":{"year":1979}}`,
		// остальные типы
		`{"code":"ПРИКАЗ-1979-2","type":"order","title":"Приказ второй","status":"published","level":0,"composed":{"year":1979,"month":2}}`,
		`{"code":"ПРИКАЗ-1979-12","type":"order","title":"Приказ двенадцатый","status":"published","level":0,"composed":{"year":1979,"month":12}}`,
		`{"code":"ИНЦ-1982-07","type":"incident","title":"Инцидент","status":"published","level":2,"composed":{"year":1982,"month":7}}`,
		`{"code":"ОТД-2","type":"unit","title":"Отдел научного изучения","status":"published","level":0,"composed":{"year":1975}}`,
		`{"code":"ЛД-0157","type":"personnel","title":"Личное дело 157","status":"published","level":4,"composed":{"year":1980}}`,
	}
	for _, d := range docs {
		e.imp(d)
		e.advance(time.Hour)
	}
	return e
}

func codes(items []Item) string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Code
	}
	return strings.Join(out, ",")
}

func (e *env) list(v Viewer, q ListQuery) *ListResult {
	e.t.Helper()
	res, err := e.svc.List(ctx, v, q)
	if err != nil {
		e.t.Fatalf("List(%+v): %v", q, err)
	}
	return res
}

func TestCatalogShowsOnlyVisibleDocuments(t *testing.T) {
	e := seedCatalog(t)
	all := ListQuery{PerPage: 100}

	// Гражданин: опубликованное уровня 0 (порядок проверяется в TestCatalogSorting, здесь — набор)
	set := func(v Viewer) map[string]bool {
		out := map[string]bool{}
		for _, it := range e.list(v, all).Items {
			out[it.Code] = true
		}
		return out
	}
	guest := set(Guest)
	wantGuest := []string{"О-002", "О-010", "О-012", "ПРИКАЗ-1979-02", "ПРИКАЗ-1979-12", "ОТД-2"}
	if len(guest) != len(wantGuest) {
		t.Errorf("Гражданин видит %d документов: %v", len(guest), guest)
	}
	for _, c := range wantGuest {
		if !guest[c] {
			t.Errorf("Гражданин не видит %s", c)
		}
	}
	for _, c := range []string{"О-003", "О-011", "О-0", "О-013", "О-014", "ИНЦ-1982-07", "ЛД-0157"} {
		if guest[c] {
			t.Errorf("Гражданин видит закрытое %s", c)
		}
	}

	// уровень растёт — видимого больше; неопубликованное недоступно никому, кроме Директората
	prev := len(guest)
	for _, lvl := range []int{1, 2, 3, 4, 5, 6} {
		s := set(viewer(lvl))
		if len(s) < prev {
			t.Errorf("уровень %d видит меньше (%d), чем предыдущий (%d)", lvl, len(s), prev)
		}
		prev = len(s)
		for _, hidden := range []string{"О-0", "О-013", "О-014"} {
			if s[hidden] {
				t.Errorf("уровень %d видит %s", lvl, hidden)
			}
		}
	}
	if s := set(viewer(6)); len(s) != 10 {
		t.Errorf("Особый Совет видит %d документов, ожидалось 10 (все опубликованные, кроме уровня 7): %v", len(s), s)
	}

	dir := set(Viewer{UserID: 1, Directorate: true})
	if len(dir) != 13 {
		t.Errorf("Директорат видит %d, ожидалось 13 (все, включая черновик, архив и уровень 7): %v", len(dir), dir)
	}
	for _, c := range []string{"О-0", "О-013", "О-014"} {
		if !dir[c] {
			t.Errorf("Директорат не видит %s", c)
		}
	}
}

func TestCatalogItemsHideServiceFieldsFromReaders(t *testing.T) {
	e := seedCatalog(t)
	reader, _ := json.Marshal(e.list(viewer(6), ListQuery{PerPage: 100}))
	for _, leak := range []string{`"status"`, `"published_at"`, `"blocks"`, `"author"`} {
		if strings.Contains(string(reader), leak) {
			t.Errorf("читателю каталога отдано служебное поле %s", leak)
		}
	}
	dir := e.list(Viewer{UserID: 1, Directorate: true}, ListQuery{PerPage: 100})
	sawStatus, sawDate := false, false
	for _, it := range dir.Items {
		if it.Status != "" {
			sawStatus = true
		}
		if it.PublishedAt != nil {
			sawDate = true
		}
	}
	if !sawStatus || !sawDate {
		t.Error("Директорату должны быть видны статус и дата публикации")
	}
}

func TestCatalogFilters(t *testing.T) {
	e := seedCatalog(t)
	v := viewer(6)
	intp := func(n int) *int { return &n }

	cases := []struct {
		name string
		q    ListQuery
		want string
	}{
		{"тип: объекты", ListQuery{Type: "object", Sort: "code"}, "О-002,О-003,О-010,О-011,О-012"},
		{"тип: приказы", ListQuery{Type: "order"}, "ПРИКАЗ-1979-02,ПРИКАЗ-1979-12"},
		{"тип: отделы", ListQuery{Type: "unit"}, "ОТД-2"},
		{"класс 4", ListQuery{Class: intp(4)}, "О-010"},
		{"класс 5 (уровень 7 недоступен)", ListQuery{Class: intp(5)}, "О-011"},
		{"с 1980 года", ListQuery{Type: "object", YearFrom: intp(1980)}, "О-002,О-011,О-012"},
		{"по 1979 год", ListQuery{Type: "object", YearTo: intp(1979)}, "О-003,О-010"},
		{"период 1979–1981", ListQuery{Type: "object", YearFrom: intp(1979), YearTo: intp(1981)}, "О-002,О-003,О-010"},
		{"отдел ОТД-2", ListQuery{Department: "otd-2"}, "О-002,О-010"},
		{"отдел ОБ-14", ListQuery{Department: "ОБ-14"}, "О-003"},
		{"категория: существо", ListQuery{Category: "entity"}, "О-010,О-011"},
		{"категория: место", ListQuery{Category: "place"}, "О-003"},
		{"содержится", ListQuery{Containment: "contained"}, "О-002"},
		{"уничтожен", ListQuery{Containment: "destroyed"}, "О-011"},
		{"тип + класс", ListQuery{Type: "object", Class: intp(3)}, "О-003"},
		{"ничего не подходит", ListQuery{Type: "order", Class: intp(3)}, ""},
	}
	for _, c := range cases {
		c.q.PerPage = 100
		got := codes(e.list(v, c.q).Items)
		// порядок по шифру проверяется отдельно; здесь — набор
		if !sameSet(got, c.want) {
			t.Errorf("%s: получено [%s], ожидалось [%s]", c.name, got, c.want)
		}
	}

	// Гражданин фильтрует только по видимому
	if got := codes(e.list(Guest, ListQuery{Class: intp(4), PerPage: 100}).Items); got != "О-010" {
		t.Errorf("Гражданин, класс 4: %q", got)
	}
	if got := codes(e.list(Guest, ListQuery{Class: intp(5), PerPage: 100}).Items); got != "" {
		t.Errorf("Гражданин видит объекты класса 5 (они закрыты): %q", got)
	}

	// Статус — только Директорату
	dir := Viewer{UserID: 1, Directorate: true}
	if got := codes(e.list(dir, ListQuery{Status: "draft", PerPage: 100}).Items); got != "О-013" {
		t.Errorf("Директорат, черновики: %q", got)
	}
	if got := codes(e.list(dir, ListQuery{Status: "archived", PerPage: 100}).Items); got != "О-014" {
		t.Errorf("Директорат, архив: %q", got)
	}
	if _, err := e.svc.List(ctx, v, ListQuery{Status: "draft"}); err == nil {
		t.Error("фильтр по статусу принят у обычного читателя")
	}
}

func sameSet(a, b string) bool {
	as, bs := map[string]bool{}, map[string]bool{}
	for _, s := range strings.Split(a, ",") {
		if s != "" {
			as[s] = true
		}
	}
	for _, s := range strings.Split(b, ",") {
		if s != "" {
			bs[s] = true
		}
	}
	if len(as) != len(bs) {
		return false
	}
	for k := range as {
		if !bs[k] {
			return false
		}
	}
	return true
}

func TestCatalogSorting(t *testing.T) {
	e := seedCatalog(t)
	v := Viewer{UserID: 1, Directorate: true}
	sorted := func(q ListQuery) string {
		q.PerPage = 100
		q.Type = "object"
		return codes(e.list(v, q).Items)
	}

	// шифр — естественная сортировка: О-2 раньше О-10
	if got := sorted(ListQuery{Sort: "code"}); got != "О-0,О-002,О-003,О-010,О-011,О-012,О-013,О-014" {
		t.Errorf("по шифру: %s", got)
	}
	if got := sorted(ListQuery{Sort: "code", Desc: true}); got != "О-014,О-013,О-012,О-011,О-010,О-003,О-002,О-0" {
		t.Errorf("по шифру, обратный порядок: %s", got)
	}
	// у приказов «ПРИКАЗ-1979-2» раньше «ПРИКАЗ-1979-12»
	ord := codes(e.list(v, ListQuery{Type: "order", Sort: "code", PerPage: 100}).Items)
	if ord != "ПРИКАЗ-1979-02,ПРИКАЗ-1979-12" {
		t.Errorf("приказы по шифру: %s", ord)
	}

	// название — по алфавиту
	titles := func(desc bool) string {
		res := e.list(v, ListQuery{Type: "object", Sort: "title", Desc: desc, PerPage: 100})
		out := make([]string, len(res.Items))
		for i, it := range res.Items {
			out[i] = it.Title
		}
		return strings.Join(out, ",")
	}
	if got := titles(false); got != "Альфа,Без свойств,Бета,В архиве,Гамма,Дельта,Праисточник,Черновой" {
		t.Errorf("по названию: %s", got)
	}
	if got := titles(true); !strings.HasPrefix(got, "Черновой,Праисточник") {
		t.Errorf("по названию, обратный порядок: %s", got)
	}

	// год: с учётом месяца и дня; внутри года документы без месяца — после датированных, между собой по шифру
	if got := sorted(ListQuery{Sort: "year"}); got != "О-0,О-010,О-003,О-013,О-014,О-002,О-011,О-012" {
		t.Errorf("по году: %s", got)
	}

	// класс: пустые значения всегда в конце, при равных классах — по шифру
	if got := sorted(ListQuery{Sort: "class"}); got != "О-013,О-002,О-003,О-010,О-0,О-011,О-012,О-014" {
		t.Errorf("по классу: %s", got)
	}
	if got := sorted(ListQuery{Sort: "class", Desc: true}); got != "О-0,О-011,О-010,О-003,О-002,О-013,О-012,О-014" {
		t.Errorf("по классу, обратный порядок: %s", got)
	}

	// п.о.: пустые — в конце в обоих направлениях
	if got := sorted(ListQuery{Sort: "deviation"}); !strings.HasPrefix(got, "О-002,О-010,О-011") || !strings.HasSuffix(got, "О-014") {
		t.Errorf("по п.о.: %s", got)
	}
	if got := sorted(ListQuery{Sort: "deviation", Desc: true}); !strings.HasPrefix(got, "О-011,О-010,О-002") {
		t.Errorf("по п.о., обратный порядок: %s", got)
	}

	// поступление: свежие первыми
	if got := codes(e.list(v, ListQuery{Sort: "published", Desc: true, PerPage: 3}).Items); got != "ЛД-0157,ОТД-2,ИНЦ-1982-07" {
		t.Errorf("по поступлению: %s", got)
	}
}

func TestCatalogPagination(t *testing.T) {
	e := seedCatalog(t)
	v := viewer(6) // 10 видимых документов
	p1 := e.list(v, ListQuery{Type: "object", PerPage: 2, Page: 1, Sort: "code"})
	p2 := e.list(v, ListQuery{Type: "object", PerPage: 2, Page: 2, Sort: "code"})
	p3 := e.list(v, ListQuery{Type: "object", PerPage: 2, Page: 3, Sort: "code"})
	p4 := e.list(v, ListQuery{Type: "object", PerPage: 2, Page: 4, Sort: "code"})

	if p1.Total != 5 || p1.Pages != 3 || p1.Page != 1 || p1.PerPage != 2 {
		t.Errorf("страница 1: %+v", p1)
	}
	if codes(p1.Items) != "О-002,О-003" || codes(p2.Items) != "О-010,О-011" || codes(p3.Items) != "О-012" {
		t.Errorf("содержимое страниц: %s | %s | %s", codes(p1.Items), codes(p2.Items), codes(p3.Items))
	}
	if len(p4.Items) != 0 || p4.Total != 5 {
		t.Errorf("страница за пределами: %+v", p4)
	}

	// значения по умолчанию
	d := e.list(v, ListQuery{})
	if d.Page != 1 || d.PerPage != defaultPerPage || d.Total != 10 || d.Pages != 1 || len(d.Items) != 10 {
		t.Errorf("по умолчанию: page=%d per=%d total=%d pages=%d items=%d", d.Page, d.PerPage, d.Total, d.Pages, len(d.Items))
	}
	// пустая выдача — не nil, чтобы клиент получил []
	empty := e.list(Guest, ListQuery{Type: "personnel"})
	raw, _ := json.Marshal(empty)
	if !strings.Contains(string(raw), `"items":[]`) || empty.Total != 0 || empty.Pages != 0 {
		t.Errorf("пустой каталог: %s", raw)
	}
}

func TestCatalogRejectsBadQueries(t *testing.T) {
	e := seedCatalog(t)
	intp := func(n int) *int { return &n }
	bad := map[string]struct {
		q     ListQuery
		field string
	}{
		"тип":                      {ListQuery{Type: "spaceship"}, "type"},
		"класс 0":                  {ListQuery{Class: intp(0)}, "class"},
		"класс 6":                  {ListQuery{Class: intp(6)}, "class"},
		"год 1800":                 {ListQuery{YearFrom: intp(1800)}, "year_from"},
		"год 2200":                 {ListQuery{YearTo: intp(2200)}, "year_to"},
		"период наоборот":          {ListQuery{YearFrom: intp(1990), YearTo: intp(1980)}, "year_from"},
		"отдел — не отдел":         {ListQuery{Department: "О-041"}, "department"},
		"отдел — мусор":            {ListQuery{Department: "'; DROP TABLE documents; --"}, "department"},
		"категория":                {ListQuery{Category: "ghost"}, "category"},
		"содержание":               {ListQuery{Containment: "escaped"}, "containment"},
		"сортировка":               {ListQuery{Sort: "password_hash"}, "sort"},
		"сортировка — инъекция":    {ListQuery{Sort: "id; DROP TABLE documents"}, "sort"},
		"отрицательная страница":   {ListQuery{Page: -1}, "page"},
		"слишком большая страница": {ListQuery{PerPage: 1000}, "per_page"},
		"отрицательный размер":     {ListQuery{PerPage: -5}, "per_page"},
		"неизвестный статус":       {ListQuery{Status: "hidden"}, "status"},
	}
	for name, c := range bad {
		_, err := e.svc.List(ctx, Viewer{UserID: 1, Directorate: true}, c.q)
		var qe *QueryError
		if !errors.As(err, &qe) || qe.Field != c.field {
			t.Errorf("%s: ожидалась QueryError по полю %q, получено %v", name, c.field, err)
		}
	}
	if n := e.count("SELECT count(*) FROM documents"); n == 0 {
		t.Error("таблица документов пропала")
	}
}

func TestRecentFeed(t *testing.T) {
	e := seedCatalog(t)
	// лента Директората тоже только опубликованное; свежие первыми
	feed, err := e.svc.Recent(ctx, Viewer{UserID: 1, Directorate: true}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if codes(feed) != "ЛД-0157,ОТД-2,ИНЦ-1982-07,ПРИКАЗ-1979-12" {
		t.Errorf("лента Директората: %s", codes(feed))
	}
	for _, it := range feed {
		if it.PublishedAt == nil {
			t.Error("у Директората в ленте должна быть дата поступления")
		}
	}

	// читатель видит ленту по своему допуску: ЛД-0157 (уровень 4) и ИНЦ (уровень 2) — не для Гражданина
	g, _ := e.svc.Recent(ctx, Guest, 3)
	if codes(g) != "ОТД-2,ПРИКАЗ-1979-12,ПРИКАЗ-1979-02" {
		t.Errorf("лента Гражданина: %s", codes(g))
	}
	raw, _ := json.Marshal(g)
	if strings.Contains(string(raw), "published_at") {
		t.Errorf("читателю отдана реальная дата поступления: %s", raw)
	}
	l2, _ := e.svc.Recent(ctx, viewer(2), 3)
	if codes(l2) != "ОТД-2,ИНЦ-1982-07,ПРИКАЗ-1979-12" {
		t.Errorf("лента уровня 2: %s", codes(l2))
	}

	for _, n := range []int{0, -1, maxFeed + 1} {
		var qe *QueryError
		if _, err := e.svc.Recent(ctx, Guest, n); !errors.As(err, &qe) {
			t.Errorf("limit %d принят", n)
		}
	}
	// в ленту не попадают неопубликованные и без даты поступления
	all, _ := e.svc.Recent(ctx, Viewer{UserID: 1, Directorate: true}, maxFeed)
	for _, it := range all {
		if it.Code == "О-013" || it.Code == "О-014" {
			t.Errorf("в ленте неопубликованный документ %s", it.Code)
		}
	}
}

func TestSummary(t *testing.T) {
	e := seedCatalog(t)
	find := func(s *Summary, typ string) int64 {
		for _, tc := range s.Types {
			if tc.Type == typ {
				return tc.Count
			}
		}
		return 0
	}

	g, err := e.svc.Summary(ctx, Guest)
	if err != nil {
		t.Fatal(err)
	}
	if g.Total != 6 || find(g, "object") != 3 || find(g, "order") != 2 || find(g, "unit") != 1 || find(g, "incident") != 0 || find(g, "personnel") != 0 {
		t.Errorf("сводка Гражданина: %+v", g)
	}
	// типы без видимых документов не показываются; порядок — как в каталоге
	if len(g.Types) != 3 || g.Types[0].Type != "object" || g.Types[0].Name != "Объект" {
		t.Errorf("типы Гражданина: %+v", g.Types)
	}
	// отделы и классы считаются по видимому: у Гражданина ОТД-2 — два объекта, класса 2 и 4
	if len(g.Departments) != 1 || g.Departments[0] != (DepartmentCount{"ОТД-2", 2}) {
		t.Errorf("отделы Гражданина: %+v", g.Departments)
	}
	if len(g.Classes) != 2 || g.Classes[0] != (ClassCount{2, 1}) || g.Classes[1] != (ClassCount{4, 1}) {
		t.Errorf("классы Гражданина: %+v", g.Classes)
	}

	d, _ := e.svc.Summary(ctx, Viewer{UserID: 1, Directorate: true})
	if d.Total != 13 || find(d, "object") != 8 {
		t.Errorf("сводка Директората: %+v", d)
	}
	l6, _ := e.svc.Summary(ctx, viewer(6))
	if l6.Total != 10 || len(l6.Departments) != 2 || len(l6.Classes) != 4 {
		t.Errorf("сводка уровня 6: %+v", l6)
	}

	// в пустом архиве — пустые массивы, а не null
	empty := newEnv(t)
	s, _ := empty.svc.Summary(ctx, Guest)
	raw, _ := json.Marshal(s)
	for _, want := range []string{`"types":[]`, `"departments":[]`, `"classes":[]`, `"total":0`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("пустая сводка: нет %s в %s", want, raw)
		}
	}
}

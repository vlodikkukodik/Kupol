package httpapi_test

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"kupol/internal/documents"
)

// docStack — стенд с набором документов и пользователями всех уровней.
type docStack struct {
	*stack
	guest      *client
	byLevel    map[int]*client // 1..6
	directorat *client
}

func newDocStack(t *testing.T) *docStack {
	t.Helper()
	st := newStack(t, nil)
	ds := &docStack{stack: st, guest: st.newClient(t), byLevel: map[int]*client{}}

	for lvl := 1; lvl <= 6; lvl++ {
		c := st.newClient(t)
		name := fmt.Sprintf("reader%d", lvl)
		if r := c.register(name, "password-1"); r.Code != 201 {
			t.Fatalf("регистрация %s: %d %s", name, r.Code, r.Body)
		}
		if err := st.svc.SetLevel(t.Context(), name, lvl); err != nil {
			t.Fatal(err)
		}
		ds.byLevel[lvl] = c
	}
	ds.directorat = st.newClient(t)
	if r := ds.directorat.register("boss", "password-1"); r.Code != 201 {
		t.Fatal(r.Body)
	}
	if err := st.svc.SetDirectorate(t.Context(), "boss", true); err != nil {
		t.Fatal(err)
	}
	return ds
}

func (ds *docStack) importDocs(t *testing.T, raw string) {
	t.Helper()
	if _, err := ds.docs.Import(t.Context(), []byte(raw), documents.ImportOptions{}); err != nil {
		t.Fatalf("загрузка: %v", err)
	}
}

// viewers перечисляет всех читателей с уровнем допуска.
func (ds *docStack) viewers() []struct {
	name  string
	level int
	c     *client
} {
	out := []struct {
		name  string
		level int
		c     *client
	}{{"Гражданин", 0, ds.guest}}
	for lvl := 1; lvl <= 6; lvl++ {
		out = append(out, struct {
			name  string
			level int
			c     *client
		}{fmt.Sprintf("уровень %d", lvl), lvl, ds.byLevel[lvl]})
	}
	return append(out, struct {
		name  string
		level int
		c     *client
	}{"Директорат", 7, ds.directorat})
}

func docPath(code string) string { return "/api/documents/" + url.PathEscape(code) }

func TestDocumentBodiesNeverContainSecretsAboveViewerLevel(t *testing.T) {
	ds := newDocStack(t)
	// по абзацу на каждый уровень 0–7 и по фрагменту текста на каждый уровень
	var blocks []string
	for lvl := 0; lvl <= 7; lvl++ {
		blocks = append(blocks, fmt.Sprintf(
			`{"id":"blk%d","type":"paragraph","level":%d,"data":{"text":[{"text":"БЛОК-УРОВНЯ-%d"},{"text":" ФРАГМЕНТ-УРОВНЯ-%d","level":%d}]}}`,
			lvl, lvl, lvl, lvl, lvl))
	}
	ds.importDocs(t, `{"code":"О-1","type":"object","title":"Проверка утечек","status":"published","level":0,"composed":{"year":1979},
		"blocks":[`+strings.Join(blocks, ",")+`]}`)

	for _, vw := range ds.viewers() {
		r := vw.c.do("GET", docPath("О-001"), nil)
		if r.Code != 200 {
			t.Fatalf("%s: %d %s", vw.name, r.Code, r.Body)
		}
		body := r.Body.String()
		for lvl := 0; lvl <= 7; lvl++ {
			block, frag := fmt.Sprintf("БЛОК-УРОВНЯ-%d", lvl), fmt.Sprintf("ФРАГМЕНТ-УРОВНЯ-%d", lvl)
			id := fmt.Sprintf(`"id":"blk%d"`, lvl)
			if lvl > vw.level {
				for _, secret := range []string{block, frag, id} {
					if strings.Contains(body, secret) {
						t.Errorf("%s видит закрытое %q: %s", vw.name, secret, body)
					}
				}
			} else if !strings.Contains(body, block) || !strings.Contains(body, frag) {
				t.Errorf("%s не видит открытый уровень %d: %s", vw.name, lvl, body)
			}
		}
	}
}

func TestDocumentAccessMatrixOverHTTP(t *testing.T) {
	ds := newDocStack(t)
	ds.importDocs(t, `{"documents":[
		{"code":"О-1","type":"object","title":"Открытый","status":"published","level":0,"composed":{"year":1979}},
		{"code":"О-2","type":"object","title":"Уровень 3, 404","status":"published","level":3,"composed":{"year":1979}},
		{"code":"О-3","type":"object","title":"Уровень 3, 403","status":"published","level":3,"direct_link":"forbidden","composed":{"year":1979}},
		{"code":"О-4","type":"object","title":"Черновик, 403","status":"draft","level":0,"direct_link":"forbidden","composed":{"year":1979}},
		{"code":"О-0","type":"object","title":"Праисточник","status":"published","level":7,"direct_link":"forbidden","composed":{"year":1974}}
	]}`)

	for _, vw := range ds.viewers() {
		get := func(code string) response { return vw.c.do("GET", docPath(code), nil) }

		want(t, get("О-1"), 200, "")

		// уровень 3, режим «не найдено»
		r := get("О-2")
		if vw.level >= 3 {
			want(t, r, 200, "")
		} else {
			want(t, r, 404, "not_found")
		}

		// уровень 3, режим «Доступ запрещён»
		r = get("О-3")
		switch {
		case vw.level >= 3:
			want(t, r, 200, "")
		default:
			want(t, r, 403, "access_denied")
			e := r.json()["error"].(map[string]any)
			if e["required_level"] != float64(3) || e["required_level_name"] != "Сотрудник" || e["message"] != "Доступ запрещён" {
				t.Errorf("%s: тело 403: %v", vw.name, e)
			}
		}

		// черновик: 404 всем, кроме Директората; режим «forbidden» существование не раскрывает
		r = get("О-4")
		if vw.level == 7 {
			want(t, r, 200, "")
		} else {
			want(t, r, 404, "not_found")
		}

		// Праисточник (уровень 7)
		r = get("О-0")
		if vw.level == 7 {
			want(t, r, 200, "")
		} else {
			want(t, r, 403, "access_denied")
			if e := r.json()["error"].(map[string]any); e["required_level"] != float64(7) || e["required_level_name"] != "Директорат" {
				t.Errorf("%s: тело 403 для уровня 7: %v", vw.name, e)
			}
		}

		// несуществующее и не-шифр — 404
		want(t, get("О-999"), 404, "not_found")
		want(t, get("чепуха"), 404, "not_found")
	}
}

func TestDocumentCodeInAnyLayoutOverHTTP(t *testing.T) {
	ds := newDocStack(t)
	ds.importDocs(t, `{"code":"ПРИКАЗ-1978-12","type":"order","title":"Приказ","status":"published","composed":{"year":1978}}`)
	for _, ref := range []string{"ПРИКАЗ-1978-12", "PRIKAZ-1978-12", "prikaz-1978-12", "ПРиказ-1978-12"} {
		r := ds.guest.do("GET", docPath(ref), nil)
		want(t, r, 200, "")
		d := r.json()["document"].(map[string]any)
		if d["code"] != "ПРИКАЗ-1978-12" || d["slug"] != "PRIKAZ-1978-12" || d["type_name"] != "Приказ" {
			t.Errorf("%s: %v", ref, d)
		}
	}
}

func TestDocumentResponseShape(t *testing.T) {
	ds := newDocStack(t)
	if _, err := ds.docs.Import(t.Context(), []byte(`{"code":"О-41","type":"object","title":"Объект","status":"published","composed":{"year":1979,"month":3},
		"props":{"danger_class":3,"deviation_points":12,"department":"ОТД-2","category":"entity","containment_status":"studying","discovery_place":"Полигон"},
		"blocks":[{"type":"dossier_header"},{"type":"paragraph","data":{"text":"Текст"}}]}`),
		documents.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	r := ds.guest.do("GET", docPath("О-41"), nil)
	want(t, r, 200, "")
	d := r.json()["document"].(map[string]any)
	for k, v := range map[string]any{
		"code": "О-041", "slug": "O-041", "type": "object", "type_name": "Объект", "title": "Объект",
		"grif": "Форма КУПОЛ-1", "level": float64(0), "danger_class": float64(3), "deviation_points": float64(12),
		"department": "ОТД-2", "category": "entity", "category_name": "Существо или явление",
		"containment_status": "studying", "containment_name": "Изучается", "discovery_place": "Полигон",
	} {
		if d[k] != v {
			t.Errorf("поле %s = %v, ожидалось %v", k, d[k], v)
		}
	}
	if c := d["composed"].(map[string]any); c["year"] != float64(1979) || c["month"] != float64(3) || c["day"] != nil {
		t.Errorf("composed: %v", c)
	}
	if blocks := d["blocks"].([]any); len(blocks) != 2 || blocks[0].(map[string]any)["type"] != "dossier_header" {
		t.Errorf("блоки: %v", d["blocks"])
	}
	body := r.Body.String()
	for _, leak := range []string{`"status"`, "published_at", "created_at", "updated_at", "revision", `"author"`} {
		if strings.Contains(body, leak) {
			t.Errorf("читателю отдано служебное поле %s: %s", leak, body)
		}
	}
	// Директорат видит статус
	if dir := ds.directorat.do("GET", docPath("О-41"), nil).json()["document"].(map[string]any); dir["status"] != "published" {
		t.Errorf("Директорат: статус %v", dir["status"])
	}
}

func TestCatalogEndpoints(t *testing.T) {
	ds := newDocStack(t)
	ds.importDocs(t, `{"documents":[
		{"code":"О-2","type":"object","title":"Гамма","status":"published","level":0,"composed":{"year":1981},"props":{"danger_class":2,"department":"ОТД-2"}},
		{"code":"О-10","type":"object","title":"Альфа","status":"published","level":0,"composed":{"year":1979},"props":{"danger_class":4,"department":"ОТД-2"}},
		{"code":"О-3","type":"object","title":"Бета","status":"published","level":3,"composed":{"year":1980},"props":{"danger_class":3}},
		{"code":"О-4","type":"object","title":"Черновик","status":"draft","level":0,"composed":{"year":1980}},
		{"code":"ПРИКАЗ-1979-2","type":"order","title":"Приказ","status":"published","level":0,"composed":{"year":1979}}
	]}`)

	items := func(r response) []string {
		var out []string
		for _, it := range r.json()["items"].([]any) {
			out = append(out, it.(map[string]any)["code"].(string))
		}
		return out
	}

	g := ds.guest.do("GET", "/api/documents?sort=code", nil)
	want(t, g, 200, "")
	if got := strings.Join(items(g), ","); got != "О-002,О-010,ПРИКАЗ-1979-02" && got != "ПРИКАЗ-1979-02,О-002,О-010" {
		t.Errorf("каталог Гражданина: %s", got)
	}
	if g.json()["total"] != float64(3) || g.json()["page"] != float64(1) || g.json()["per_page"] != float64(50) {
		t.Errorf("метаданные страницы: %v", g.json())
	}

	// уровень 3 видит на один документ больше; черновик — только Директорат
	if l3 := ds.byLevel[3].do("GET", "/api/documents", nil); l3.json()["total"] != float64(4) {
		t.Errorf("уровень 3: %v", l3.json()["total"])
	}
	if dir := ds.directorat.do("GET", "/api/documents", nil); dir.json()["total"] != float64(5) {
		t.Errorf("Директорат: %v", dir.json()["total"])
	}

	// фильтры и сортировка
	r := ds.guest.do("GET", "/api/documents?type=object&department=otd-2&sort=class&order=desc", nil)
	want(t, r, 200, "")
	if got := strings.Join(items(r), ","); got != "О-010,О-002" {
		t.Errorf("фильтр по отделу, сортировка по классу (убывание): %s", got)
	}
	r = ds.guest.do("GET", "/api/documents?year_from=1980", nil)
	if got := strings.Join(items(r), ","); got != "О-002" {
		t.Errorf("с 1980 года (Гражданин): %s", got)
	}
	r = ds.guest.do("GET", "/api/documents?per_page=1&page=2&sort=code&type=object", nil)
	if got := items(r); len(got) != 1 || r.json()["pages"] != float64(2) {
		t.Errorf("пагинация: %v %v", got, r.json())
	}

	// служебные поля читателям не отдаются
	if body := g.Body.String(); strings.Contains(body, `"status"`) || strings.Contains(body, "published_at") {
		t.Errorf("каталог раскрывает служебные поля: %s", body)
	}
	if body := ds.directorat.do("GET", "/api/documents?status=draft", nil).Body.String(); !strings.Contains(body, `"status":"draft"`) {
		t.Errorf("Директорат: фильтр по статусу: %s", body)
	}
}

func TestCatalogRejectsBadParameters(t *testing.T) {
	ds := newDocStack(t)
	for _, q := range []string{
		"type=spaceship", "class=abc", "class=9", "year_from=x", "year_to=2200", "page=-1", "page=x", "per_page=0",
		"per_page=101", "per_page=x", "sort=password", "order=sideways", "department=О-41", "category=ghost",
		"containment=escaped", "status=draft", // status — не для читателей
	} {
		r := ds.guest.do("GET", "/api/documents?"+q, nil)
		if r.Code != 400 || r.errCode() != "bad_request" || len(r.fields()) != 1 {
			t.Errorf("%s: %d %s", q, r.Code, r.Body)
		}
	}
}

func TestRecentAndSummaryEndpoints(t *testing.T) {
	ds := newDocStack(t)
	ds.importDocs(t, `{"documents":[
		{"code":"О-1","type":"object","title":"Один","status":"published","level":0,"composed":{"year":1979},"props":{"danger_class":2,"department":"ОТД-2"}},
		{"code":"О-2","type":"object","title":"Два","status":"published","level":4,"composed":{"year":1979},"props":{"danger_class":3}},
		{"code":"ПРИКАЗ-1979-1","type":"order","title":"Приказ","status":"published","level":0,"composed":{"year":1979}}
	]}`)

	feed := ds.guest.do("GET", "/api/documents/recent?limit=5", nil)
	want(t, feed, 200, "")
	if n := len(feed.json()["items"].([]any)); n != 2 {
		t.Errorf("лента Гражданина: %d элементов", n)
	}
	if body := feed.Body.String(); strings.Contains(body, "published_at") || strings.Contains(body, "Два") {
		t.Errorf("лента раскрывает лишнее: %s", body)
	}
	if n := len(ds.byLevel[4].do("GET", "/api/documents/recent", nil).json()["items"].([]any)); n != 3 {
		t.Errorf("лента уровня 4 (по умолчанию 10): %d", n)
	}
	for _, q := range []string{"limit=0", "limit=51", "limit=x"} {
		if r := ds.guest.do("GET", "/api/documents/recent?"+q, nil); r.Code != 400 {
			t.Errorf("%s: %d", q, r.Code)
		}
	}

	sum := ds.guest.do("GET", "/api/documents/summary", nil)
	want(t, sum, 200, "")
	j := sum.json()
	if j["total"] != float64(2) || len(j["types"].([]any)) != 2 || len(j["departments"].([]any)) != 1 || len(j["classes"].([]any)) != 1 {
		t.Errorf("сводка Гражданина: %v", j)
	}
	if j4 := ds.byLevel[4].do("GET", "/api/documents/summary", nil).json(); j4["total"] != float64(3) {
		t.Errorf("сводка уровня 4: %v", j4)
	}
}

func TestReadsAreRecordedForSignedInReaders(t *testing.T) {
	ds := newDocStack(t)
	ds.importDocs(t, `{"code":"О-1","type":"object","title":"Т","status":"published","composed":{"year":1979}}`)
	ds.guest.do("GET", docPath("О-1"), nil)
	if n := countRows(t, ds.stack, "document_reads"); n != 0 {
		t.Errorf("чтение гостя записано: %d", n)
	}
	ds.byLevel[1].do("GET", docPath("О-1"), nil)
	ds.byLevel[1].do("GET", docPath("О-1"), nil)
	var cnt int
	ds.db.Raw("SELECT read_count FROM document_reads").Scan(&cnt)
	if cnt != 2 {
		t.Errorf("read_count = %d, ожидалось 2", cnt)
	}
}

func countRows(t *testing.T, st *stack, table string) int64 {
	t.Helper()
	var n int64
	if err := st.db.Raw("SELECT count(*) FROM " + table).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDocumentRoutesAreReadOnlyAndSpecificRoutesWin(t *testing.T) {
	ds := newDocStack(t)
	ds.importDocs(t, `{"code":"О-1","type":"object","title":"Т","status":"published","composed":{"year":1979}}`)
	// изменять документы через API нельзя (этап 3)
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		r := ds.directorat.do(m, "/api/documents", map[string]any{"title": "x"})
		if r.Code != 405 {
			t.Errorf("%s /api/documents: %d", m, r.Code)
		}
		r = ds.directorat.do(m, docPath("О-1"), map[string]any{"title": "x"})
		if r.Code != 405 {
			t.Errorf("%s /api/documents/О-1: %d", m, r.Code)
		}
	}
	// «summary» и «recent» — маршруты, а не шифры документов
	if r := ds.guest.do("GET", "/api/documents/summary", nil); r.json()["total"] == nil {
		t.Errorf("summary: %s", r.Body)
	}
	if r := ds.guest.do("GET", "/api/documents/recent", nil); r.json()["items"] == nil {
		t.Errorf("recent: %s", r.Body)
	}
}

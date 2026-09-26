package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// Поиск через HTTP: то, что читатель получает в теле ответа, а не то, что решил сервис внутри. Метка каждого уровня стоит в
// блоке и во фрагменте этого уровня; ни один читатель не должен увидеть чужую метку ни в одном поле ответа.
func TestSearchResponsesNeverContainSecretsAboveViewerLevel(t *testing.T) {
	ds := newDocStack(t)
	var blocks []string
	for lvl := 0; lvl <= 7; lvl++ {
		blocks = append(blocks,
			fmt.Sprintf(`{"id":"b%d","type":"paragraph","level":%d,"data":{"text":"поисковоеслово blk%dmark"}}`, lvl, lvl, lvl),
			fmt.Sprintf(`{"id":"r%d","type":"paragraph","data":{"text":[{"text":"поисковоеслово "},{"text":"run%dmark","level":%d}]}}`, lvl, lvl, lvl))
	}
	ds.importDocs(t, fmt.Sprintf(`{"code":"О-700","type":"object","title":"Проверка поиска","status":"published","level":0,"composed":{"year":1979},"blocks":[%s]}`, strings.Join(blocks, ",")))
	ds.importDocs(t, `{"code":"О-701","type":"object","title":"Закрытый секретдок","status":"published","level":5,"composed":{"year":1979},"blocks":[{"id":"a","type":"paragraph","data":{"text":"поисковоеслово"}}]}`)
	ds.importDocs(t, `{"code":"О-702","type":"object","title":"Черновик секретчерновик","status":"draft","level":0,"composed":{"year":1979},"blocks":[{"id":"a","type":"paragraph","data":{"text":"поисковоеслово"}}]}`)

	get := func(c *client, query string) (int, map[string]any, string) {
		r := c.do("GET", "/api/search?"+query, nil)
		var m map[string]any
		_ = json.Unmarshal(r.Body.Bytes(), &m)
		return r.Code, m, r.Body.String()
	}

	for _, v := range ds.viewers() {
		code, m, raw := get(v.c, "q="+url.QueryEscape("поисковоеслово"))
		if code != 200 {
			t.Fatalf("%s: %d %s", v.name, code, raw)
		}
		want := 1 // О-700 открыт всем
		if v.level >= 5 {
			want++ // О-701
		}
		if v.level == 7 {
			want++ // черновик — только Директорату
		}
		if int(m["total"].(float64)) != want {
			t.Errorf("%s: найдено %v, ожидалось %d: %s", v.name, m["total"], want, raw)
		}
		// в теле ответа нет ни одной метки выше допуска
		for lvl := v.level + 1; lvl <= 7; lvl++ {
			for _, mark := range []string{fmt.Sprintf("blk%dmark", lvl), fmt.Sprintf("run%dmark", lvl)} {
				if strings.Contains(raw, mark) {
					t.Errorf("%s: в ответе метка %s выше допуска", v.name, mark)
				}
			}
		}
		if v.level < 5 && strings.Contains(raw, "секретдок") {
			t.Errorf("%s: в ответе название закрытого документа", v.name)
		}
		if v.level < 7 && (strings.Contains(raw, "секретчерновик") || strings.Contains(raw, "О-702")) {
			t.Errorf("%s: в ответе черновик", v.name)
		}
		// метка выше допуска не находит ничего, а её число не выдаёт существования
		_, hidden, _ := get(v.c, "q="+url.QueryEscape(fmt.Sprintf("blk%dmark", min(v.level+1, 7))))
		if v.level < 7 && hidden["total"].(float64) != 0 {
			t.Errorf("%s: чужая метка нашлась: %v", v.name, hidden["total"])
		}
		if _, m2, _ := get(v.c, "q="+url.QueryEscape(fmt.Sprintf("blk%dmark", v.level))); m2["total"].(float64) != 1 {
			t.Errorf("%s: своя метка не нашлась: %v", v.name, m2["total"])
		}
	}
}

func TestSearchEndpointShapeParametersAndErrors(t *testing.T) {
	ds := newDocStack(t)
	ds.importDocs(t, `{"code":"О-710","type":"object","title":"Сотрудники станции","status":"published","level":0,"composed":{"year":1979},"props":{"danger_class":3},"blocks":[{"id":"a","type":"paragraph","data":{"text":"Дежурный видел тень"}}]}`)
	ds.importDocs(t, `{"code":"МЕМО-71","type":"memo","title":"Записка","status":"published","level":0,"composed":{"year":1990},"blocks":[{"id":"a","type":"paragraph","data":{"text":"Сотрудник опоздал"}}]}`)

	r := ds.guest.do("GET", "/api/search?q="+url.QueryEscape("сотрудник"), nil)
	if r.Code != 200 || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("поиск гостя: %d %v", r.Code, r.Header())
	}
	body := r.json()
	items := body["items"].([]any)
	if len(items) != 2 || body["total"].(float64) != 2 || body["query"] != "сотрудник" {
		t.Fatalf("ответ: %s", r.Body)
	}
	first := items[0].(map[string]any)
	item := first["item"].(map[string]any)
	if item["code"] != "О-710" || item["type_name"] != "Объект" || item["status"] != nil {
		t.Errorf("документ в выдаче: %v", item)
	}
	snip := first["snippets"].([]any)[0].(map[string]any)
	parts := snip["parts"].([]any)
	if snip["kind"] != "title" || !parts[0].(map[string]any)["match"].(bool) || parts[0].(map[string]any)["text"] != "Сотрудники" {
		t.Errorf("сниппет: %v", snip)
	}

	// отбор и страницы
	if r := ds.guest.do("GET", "/api/search?q=сотрудник&type=memo", nil); r.json()["total"].(float64) != 1 {
		t.Errorf("тип: %s", r.Body)
	}
	if r := ds.guest.do("GET", "/api/search?q=сотрудник&year_from=1985", nil); r.json()["total"].(float64) != 1 {
		t.Errorf("год: %s", r.Body)
	}
	if r := ds.guest.do("GET", "/api/search?q=сотрудник&class=3", nil); r.json()["total"].(float64) != 1 {
		t.Errorf("класс: %s", r.Body)
	}
	if r := ds.guest.do("GET", "/api/search?q=сотрудник&per_page=1&page=2", nil); len(r.json()["items"].([]any)) != 1 || r.json()["pages"].(float64) != 2 {
		t.Errorf("страницы: %s", r.Body)
	}
	// Директорат видит статус; читатель фильтровать по нему не вправе
	if r := ds.directorat.do("GET", "/api/search?q=сотрудник", nil); r.json()["items"].([]any)[0].(map[string]any)["item"].(map[string]any)["status"] != "published" {
		t.Errorf("статус для Директората: %s", r.Body)
	}
	// только частые слова
	if r := ds.guest.do("GET", "/api/search?q="+url.QueryEscape("и в на"), nil); r.Code != 200 || r.json()["ignored"] != true {
		t.Errorf("частые слова: %d %s", r.Code, r.Body)
	}

	// ошибки — 400 с полем
	for name, tc := range map[string]struct{ query, field string }{
		"без запроса":     {"", "q"},
		"один знак":       {"q=" + url.QueryEscape("а"), "q"},
		"слишком длинный": {"q=" + strings.Repeat("слово+", 60), "q"},
		"тип":             {"q=слово&type=nope", "type"},
		"класс":           {"q=слово&class=abc", "class"},
		"страница":        {"q=слово&page=x", "page"},
		"размер":          {"q=слово&per_page=500", "per_page"},
		"ноль":            {"q=слово&per_page=0", "per_page"},
		"статус читателю": {"q=слово&status=draft", "status"},
	} {
		r := ds.guest.do("GET", "/api/search?"+tc.query, nil)
		if r.Code != 400 || r.fields()[tc.field] == nil {
			t.Errorf("%s: %d %s", name, r.Code, r.Body)
		}
	}
	// служебные символы запроса — не ошибка сервера
	for _, q := range []string{`"`, `-`, `'; DROP TABLE documents; --`, `слово & | ! ( )`, `:*`, `\`} {
		if r := ds.guest.do("GET", "/api/search?q="+url.QueryEscape(q+" слово"), nil); r.Code != 200 {
			t.Errorf("запрос %q: %d %s", q, r.Code, r.Body)
		}
	}
}

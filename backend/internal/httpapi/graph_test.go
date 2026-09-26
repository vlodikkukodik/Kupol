package httpapi_test

import (
	"strings"
	"testing"
)

func TestGraphEndpointVisibilityAndErrors(t *testing.T) {
	ds := newDocStack(t)
	link := func(id, code string, level int) string {
		return `{"id":"` + id + `","type":"doc_link","level":` + string(rune('0'+level)) + `,"data":{"code":"` + code + `"}}`
	}
	doc := func(code, title, status string, level int, blocks ...string) string {
		return `{"code":"` + code + `","type":"object","title":"` + title + `","status":"` + status + `","level":` + string(rune('0'+level)) + `,"composed":{"year":1979},"blocks":[` + strings.Join(blocks, ",") + `]}`
	}
	ds.importDocs(t, doc("О-830", "Центр", "published", 0, link("a", "О-831", 0), link("b", "О-832", 0)))
	ds.importDocs(t, doc("О-831", "Открытый сосед", "published", 0, link("a", "О-830", 0)))
	ds.importDocs(t, doc("О-832", "Закрытый сосед", "published", 4, link("a", "О-830", 0)))

	r := ds.guest.do("GET", "/api/graph/O-830", nil)
	if r.Code != 200 || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("гость: %d %v", r.Code, r.Header())
	}
	body := r.json()
	nodes := body["nodes"].([]any)
	if body["center"] != "О-830" || len(nodes) != 2 || len(body["edges"].([]any)) != 2 {
		t.Errorf("доска гостя: %s", r.Body)
	}
	if strings.Contains(r.Body.String(), "Закрытый сосед") || strings.Contains(r.Body.String(), "О-832") {
		t.Errorf("в доске гостя закрытое: %s", r.Body)
	}
	first := nodes[0].(map[string]any)
	if first["slug"] != "O-830" || first["type_name"] != "Объект" || first["depth"].(float64) != 0 {
		t.Errorf("карточка: %v", first)
	}

	// читатель с допуском 4 видит и закрытого соседа
	if r := ds.byLevel[4].do("GET", "/api/graph/O-830?depth=2", nil); r.Code != 200 || len(r.json()["nodes"].([]any)) != 3 {
		t.Errorf("уровень 4: %d %s", r.Code, r.Body)
	}
	// закрытый или несуществующий центр — 404, глубина и шифр проверяются
	for name, tc := range map[string]struct {
		path string
		code int
	}{
		"закрытый центр":   {"/api/graph/O-832", 404},
		"нет такого":       {"/api/graph/O-999", 404},
		"не шифр":          {"/api/graph/abc", 404},
		"глубина 3":        {"/api/graph/O-830?depth=3", 400},
		"глубина ноль":     {"/api/graph/O-830?depth=0", 400},
		"глубина не число": {"/api/graph/O-830?depth=x", 400},
	} {
		if r := ds.guest.do("GET", tc.path, nil); r.Code != tc.code {
			t.Errorf("%s: %d %s", name, r.Code, r.Body)
		}
	}
}

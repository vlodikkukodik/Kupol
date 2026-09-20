package httpapi_test

import (
	"fmt"
	"net/url"
	"testing"
)

const glossaryPath = "/api/team/glossary"

func termBody(term, def string, aliases ...string) map[string]any {
	if aliases == nil {
		aliases = []string{}
	}
	return map[string]any{"term": term, "definition": def, "aliases": aliases}
}

func TestGlossaryOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	editor, author := actors["editor"], actors["author"]

	// заводят Редактор, Архивариус, Директорат; остальные и гость — нет
	for name, want := range map[string]int{"guest": 401, "plain": 403, "author": 403, "moderator": 403, "editor": 201, "archivist": 201, "director": 201} {
		if got := actors[name].client.do("POST", glossaryPath, termBody("Термин "+name, "Определение.")).Code; got != want {
			t.Errorf("%s: создание термина = %d, ожидалось %d", name, got, want)
		}
	}

	r := editor.client.do("POST", glossaryPath, termBody("Купол", "Комитет управления паранормальными объектами и локациями.", "КУПОЛ", "Комитет", "Комитет УПОЛ"))
	if r.Code != 201 {
		t.Fatalf("создание: %d %s", r.Code, r.Body)
	}
	term := r.json()["term"].(map[string]any)
	id := int64(term["id"].(float64))
	if term["term"] != "Купол" || term["can_edit"] != true || term["author"] != "user_editor" || len(term["aliases"].([]any)) != 2 { // «КУПОЛ» совпало бы с самим термином и убрано
		t.Errorf("термин: %v", term)
	}

	// читают все члены команды, но без права правки; гость и посторонний — нет
	for name, want := range map[string]int{"guest": 401, "plain": 403, "author": 200, "moderator": 200, "editor": 200} {
		if got := actors[name].client.do("GET", glossaryPath, nil).Code; got != want {
			t.Errorf("%s: чтение глоссария = %d, ожидалось %d", name, got, want)
		}
	}
	items := author.client.do("GET", glossaryPath, nil).json()["items"].([]any)
	if len(items) != 4 || items[0].(map[string]any)["can_edit"] != false { // «Купол» и три термина от ролей
		t.Errorf("глоссарий для Автора: %d %v", len(items), items[0])
	}
	found := author.client.do("GET", glossaryPath+"?q="+url.QueryEscape("комитет"), nil).json()["items"].([]any)
	if len(found) != 1 || found[0].(map[string]any)["term"] != "Купол" {
		t.Errorf("поиск: %v", found)
	}
	if r := author.client.do("GET", glossaryPath+"?q=%25", nil).json()["items"].([]any); len(r) != 0 {
		t.Errorf("«%%» сработал как шаблон: %d", len(r))
	}

	// правка и повтор: 409 с полем term; неверные данные — 422 с путём
	one := fmt.Sprintf("%s/%d", glossaryPath, id)
	if r := author.client.do("PUT", one, termBody("Взлом", "х")); r.Code != 403 {
		t.Errorf("Автор изменил термин: %d", r.Code)
	}
	if r := editor.client.do("PUT", one, termBody("Купол", "Новое определение.")); r.Code != 200 || r.json()["term"].(map[string]any)["definition"] != "Новое определение." {
		t.Errorf("правка: %d %s", r.Code, r.Body)
	}
	if r := editor.client.do("POST", glossaryPath, termBody("  купол ", "Дубль.")); r.Code != 409 || r.errCode() != "name_taken" || r.fields()["term"] == nil {
		t.Errorf("повтор термина: %d %s", r.Code, r.Body)
	}
	if r := editor.client.do("POST", glossaryPath, termBody("  ", "Определение.")); r.Code != 422 || r.errCode() != "validation" {
		t.Errorf("пустой термин: %d %s", r.Code, r.Body)
	}

	// удаление: Автору нельзя, Редактору можно, повторно — 404; мусор в адресе — 404
	if r := author.client.do("DELETE", one, nil); r.Code != 403 {
		t.Errorf("Автор удалил термин: %d", r.Code)
	}
	if r := editor.client.do("DELETE", one, nil); r.Code != 204 {
		t.Errorf("удаление: %d", r.Code)
	}
	if r := editor.client.do("DELETE", one, nil); r.Code != 404 {
		t.Errorf("повторное удаление: %d", r.Code)
	}
	for _, path := range []string{glossaryPath + "/abc", glossaryPath + "/0"} {
		if r := editor.client.do("PUT", path, termBody("х", "у")); r.Code != 404 {
			t.Errorf("%s: %d", path, r.Code)
		}
	}
}

func TestGlossaryRejectsForeignOrigin(t *testing.T) {
	_, actors := teamStackWithActors(t)
	editor := actors["editor"]
	evil := *editor.client
	evil.origin = "https://evil.example"
	for _, call := range []struct{ method, path string }{{"POST", glossaryPath}, {"PUT", glossaryPath + "/1"}, {"DELETE", glossaryPath + "/1"}} {
		if r := evil.do(call.method, call.path, termBody("Взлом", "х")); r.Code != 403 || r.errCode() != "forbidden_origin" {
			t.Errorf("%s %s с чужого сайта: %d %s", call.method, call.path, r.Code, r.errCode())
		}
	}
	if items := editor.client.do("GET", glossaryPath, nil).json()["items"].([]any); len(items) != 0 {
		t.Errorf("запросы с чужого сайта создали термины: %d", len(items))
	}
}

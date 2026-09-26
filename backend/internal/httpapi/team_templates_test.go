package httpapi_test

import (
	"fmt"
	"strings"
	"testing"
)

const templatesPath = "/api/team/templates"

func tplBody(kind, name string) map[string]any {
	blocks := []any{
		map[string]any{"id": "h", "type": "heading", "data": map[string]any{"text": "Общие сведения"}},
		map[string]any{"id": "p", "type": "paragraph", "data": map[string]any{"text": "Описание."}},
	}
	body := map[string]any{"kind": kind, "name": name, "description": "Заготовка", "content": map[string]any{"blocks": blocks}}
	if kind == "document" {
		body["doc_type"] = "object"
		body["content"] = map[string]any{"title": "Объект «…»", "level": 2, "blocks": blocks}
	}
	return body
}

func TestTemplatesOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	editor, author := actors["editor"], actors["author"]

	// создают Редактор, Архивариус и Директорат; Автор, Модератор, посторонний и гость — нет
	for name, want := range map[string]int{"guest": 401, "plain": 403, "author": 403, "moderator": 403, "editor": 201, "archivist": 201, "director": 201} {
		if got := actors[name].client.do("POST", templatesPath, tplBody("blockset", "Набор от "+name)).Code; got != want {
			t.Errorf("%s: создание шаблона = %d, ожидалось %d", name, got, want)
		}
	}

	r := editor.client.do("POST", templatesPath, tplBody("document", "Типовой объект"))
	if r.Code != 201 {
		t.Fatalf("шаблон документа: %d %s", r.Code, r.Body)
	}
	tpl := r.json()["template"].(map[string]any)
	id := int64(tpl["id"].(float64))
	if tpl["kind"] != "document" || tpl["doc_type"] != "object" || tpl["doc_type_name"] != "Объект" || tpl["blocks"].(float64) != 2 || tpl["can_edit"] != true || tpl["author"] != "user_editor" {
		t.Errorf("шаблон: %v", tpl)
	}

	// читать — все члены команды (Автор тоже), но без права правки
	for name, want := range map[string]int{"guest": 401, "plain": 403, "author": 200, "moderator": 200, "editor": 200} {
		if got := actors[name].client.do("GET", fmt.Sprintf("%s/%d", templatesPath, id), nil).Code; got != want {
			t.Errorf("%s: чтение шаблона = %d, ожидалось %d", name, got, want)
		}
	}
	got := author.client.do("GET", fmt.Sprintf("%s/%d", templatesPath, id), nil).json()["template"].(map[string]any)
	if got["can_edit"] != false || len(got["content"].(map[string]any)["blocks"].([]any)) != 2 || got["content"].(map[string]any)["level"].(float64) != 2 {
		t.Errorf("шаблон для Автора: %v", got)
	}

	// список и фильтр по виду
	list := author.client.do("GET", templatesPath, nil).json()["items"].([]any)
	if len(list) != 4 { // три набора (Редактор, Архивариус, Директорат) и один документ
		t.Errorf("шаблонов в списке: %d", len(list))
	}
	docs := author.client.do("GET", templatesPath+"?kind=document", nil).json()["items"].([]any)
	if len(docs) != 1 {
		t.Errorf("шаблонов документа: %d", len(docs))
	}
	if r := author.client.do("GET", templatesPath+"?kind=folder", nil); r.Code != 400 {
		t.Errorf("неизвестный вид: %d", r.Code)
	}

	// правка: название и описание; Автору нельзя; занятое название — 409 с полем
	put := func(a *teamActor, name string) response {
		return a.client.do("PUT", fmt.Sprintf("%s/%d", templatesPath, id), map[string]any{"name": name, "description": "Новое описание"})
	}
	if r := put(author, "Чужое"); r.Code != 403 {
		t.Errorf("Автор изменил шаблон: %d", r.Code)
	}
	if r := put(editor, "Набор от editor"); r.Code != 200 { // такое название у набора — другой вид, можно
		t.Errorf("правка: %d %s", r.Code, r.Body)
	}
	other := editor.client.do("POST", templatesPath, tplBody("document", "Второй объект")).json()["template"].(map[string]any)
	// «Набор от editor» теперь занят шаблоном документа: тем же названием (без учёта регистра и пробелов) второй документ назвать нельзя
	dup := editor.client.do("PUT", fmt.Sprintf("%s/%d", templatesPath, int64(other["id"].(float64))), map[string]any{"name": "  набор от EDITOR  ", "description": ""})
	if dup.Code != 409 || dup.errCode() != "name_taken" || dup.fields()["name"] == nil {
		t.Errorf("переименование в занятое: %d %s", dup.Code, dup.Body)
	}
	if r := editor.client.do("POST", templatesPath, tplBody("document", "второй ОБЪЕКТ")); r.Code != 409 || r.errCode() != "name_taken" {
		t.Errorf("повтор названия при создании: %d %s", r.Code, r.Body)
	}

	// неверное содержимое — 422 с путём блока; ничего не сохранилось
	bad := tplBody("blockset", "Плохой")
	bad["content"] = map[string]any{"blocks": []any{map[string]any{"id": "s", "type": "stamp", "data": map[string]any{"text": ""}}}}
	r = editor.client.do("POST", templatesPath, bad)
	if r.Code != 422 || r.errCode() != "validation" || !strings.Contains(r.Body.String(), "content.blocks[0].data.text") {
		t.Errorf("неверный блок: %d %s", r.Code, r.Body)
	}

	// удаление: Автору нельзя, Редактору можно, повторно — 404; мусор в адресе — 404
	if r := author.client.do("DELETE", fmt.Sprintf("%s/%d", templatesPath, id), nil); r.Code != 403 {
		t.Errorf("Автор удалил шаблон: %d", r.Code)
	}
	if r := editor.client.do("DELETE", fmt.Sprintf("%s/%d", templatesPath, id), nil); r.Code != 204 {
		t.Errorf("удаление: %d", r.Code)
	}
	if r := editor.client.do("DELETE", fmt.Sprintf("%s/%d", templatesPath, id), nil); r.Code != 404 {
		t.Errorf("повторное удаление: %d", r.Code)
	}
	for _, path := range []string{templatesPath + "/abc", templatesPath + "/0", templatesPath + "/999999"} {
		if r := author.client.do("GET", path, nil); r.Code != 404 || !strings.Contains(r.Body.String(), "Шаблон не найден") {
			t.Errorf("%s: %d %s", path, r.Code, r.Body)
		}
	}
}

func TestTemplatesRejectForeignOrigin(t *testing.T) {
	_, actors := teamStackWithActors(t)
	editor := actors["editor"]
	evil := *editor.client
	evil.origin = "https://evil.example"
	for _, call := range []struct{ method, path string }{{"POST", templatesPath}, {"PUT", templatesPath + "/1"}, {"DELETE", templatesPath + "/1"}} {
		if r := evil.do(call.method, call.path, tplBody("blockset", "Взлом")); r.Code != 403 || r.errCode() != "forbidden_origin" {
			t.Errorf("%s %s с чужого сайта: %d %s", call.method, call.path, r.Code, r.errCode())
		}
	}
	if items := editor.client.do("GET", templatesPath, nil).json()["items"].([]any); len(items) != 0 {
		t.Errorf("запросы с чужого сайта создали шаблоны: %d", len(items))
	}
}

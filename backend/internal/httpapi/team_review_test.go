package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Ход документа и рецензия по HTTP: права, коды ошибок, форма ответов. Правила переходов проверены в пакете documents;
// здесь — что до клиента доходит именно то, на что рассчитывает интерфейс.

func objectBody(title string, blocks ...any) map[string]any {
	if blocks == nil {
		blocks = []any{map[string]any{"id": "b1", "type": "paragraph", "data": map[string]any{"text": "Описание."}}}
	}
	return map[string]any{"type": "object", "code": "", "title": title, "composed": map[string]any{"year": 1979}, "blocks": blocks}
}

func (a *teamActor) createObject(t *testing.T, title string, blocks ...any) (id int64, revision int) {
	t.Helper()
	r := a.client.do("POST", docsPath, objectBody(title, blocks...))
	if r.Code != http.StatusCreated {
		t.Fatalf("создание объекта: %d %s", r.Code, r.Body)
	}
	doc := r.json()["document"].(map[string]any)
	return int64(doc["id"].(float64)), int(doc["revision"].(float64))
}

func teamDocPath(id int64, tail string) string { return fmt.Sprintf("%s/%d%s", docsPath, id, tail) }

func statusOf(r response) string {
	return r.json()["document"].(map[string]any)["status"].(string)
}

func TestReviewFlowOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	author, editor := actors["author"], actors["editor"]
	id, rev := author.createObject(t, "Объект по HTTP")

	// права на черновике приходят готовыми
	wf := author.client.do("GET", teamDocPath(id, ""), nil).json()["document"].(map[string]any)["workflow"].(map[string]any)
	if wf["submit"] != true || wf["review"] != false {
		t.Errorf("права автора на черновик: %v", wf)
	}

	// вердикт по черновику — 409 «не тот статус»; отправка с чужой редакцией — 409 conflict
	if r := author.client.do("POST", teamDocPath(id, "/verdict"), map[string]any{"verdict": "approve", "base_revision": rev}); r.Code != 403 {
		t.Errorf("Автор вынес вердикт: %d %s", r.Code, r.Body)
	}
	if r := actors["director"].client.do("POST", teamDocPath(id, "/verdict"), map[string]any{"verdict": "approve", "base_revision": rev}); r.Code != 409 || r.errCode() != "invalid_state" {
		t.Errorf("вердикт по черновику: %d %s", r.Code, r.errCode())
	}
	if r := author.client.do("POST", teamDocPath(id, "/submit"), map[string]any{"base_revision": rev + 5}); r.Code != 409 || r.errCode() != "conflict" {
		t.Errorf("отправка чужой редакции: %d %s", r.Code, r.errCode())
	} else if int(r.json()["error"].(map[string]any)["current_revision"].(float64)) != rev {
		t.Errorf("текущая редакция в ответе: %s", r.Body)
	}

	// отправка
	r := author.client.do("POST", teamDocPath(id, "/submit"), map[string]any{"base_revision": rev})
	if r.Code != 200 || statusOf(r) != "review" {
		t.Fatalf("отправка: %d %s", r.Code, r.Body)
	}
	if r := author.client.do("POST", teamDocPath(id, "/submit"), map[string]any{"base_revision": rev}); r.Code != 409 || r.errCode() != "invalid_state" {
		t.Errorf("повторная отправка: %d %s", r.Code, r.errCode())
	}

	// Редактор комментирует блок и возвращает документ; причина обязательна
	if r := editor.client.do("POST", teamDocPath(id, "/comments"), map[string]any{"block_id": "b1", "body": "Уточните место"}); r.Code != 201 {
		t.Fatalf("комментарий: %d %s", r.Code, r.Body)
	}
	if r := editor.client.do("POST", teamDocPath(id, "/verdict"), map[string]any{"verdict": "return", "comment": " ", "base_revision": rev}); r.Code != 422 || r.errCode() != "validation" {
		t.Errorf("возврат без причины: %d %s", r.Code, r.Body)
	}
	if r := editor.client.do("POST", teamDocPath(id, "/verdict"), map[string]any{"verdict": "return", "comment": "См. комментарий", "base_revision": rev}); r.Code != 200 || statusOf(r) != "draft" {
		t.Fatalf("возврат: %d %s", r.Code, r.Body)
	}

	// автор видит причину и комментарий, отмечает исправленным и отправляет снова
	rv := author.client.do("GET", teamDocPath(id, "/review"), nil)
	if rv.Code != 200 {
		t.Fatalf("рецензия: %d %s", rv.Code, rv.Body)
	}
	review := rv.json()["review"].(map[string]any)
	events := review["events"].([]any)
	last := events[len(events)-1].(map[string]any)
	if last["kind"] != "return" || last["comment"] != "См. комментарий" || last["actor"] != "user_editor" || last["kind_name"] == "" {
		t.Errorf("последнее событие: %v", last)
	}
	comments := review["comments"].([]any)
	if len(comments) != 1 || review["open"].(float64) != 1 {
		t.Fatalf("комментарии: %v", review)
	}
	cid := int64(comments[0].(map[string]any)["id"].(float64))
	if r := author.client.do("PUT", teamDocPath(id, fmt.Sprintf("/comments/%d", cid)), map[string]any{"resolved": true}); r.Code != 200 || r.json()["comment"].(map[string]any)["resolved"] != true {
		t.Errorf("отметка «исправлено»: %d %s", r.Code, r.Body)
	}
	cur := author.client.do("GET", teamDocPath(id, ""), nil).json()["document"].(map[string]any)
	rev = int(cur["revision"].(float64))
	if r := author.client.do("POST", teamDocPath(id, "/submit"), map[string]any{"base_revision": rev}); r.Code != 200 {
		t.Fatalf("повторная отправка: %d %s", r.Code, r.Body)
	}

	// принятие: документ опубликован, получил номер О-№ и открыт читателю
	r = editor.client.do("POST", teamDocPath(id, "/verdict"), map[string]any{"verdict": "approve", "base_revision": rev})
	if r.Code != 200 || statusOf(r) != "published" {
		t.Fatalf("принятие: %d %s", r.Code, r.Body)
	}
	code, _ := r.json()["document"].(map[string]any)["code"].(string)
	if !strings.HasPrefix(code, "О-") {
		t.Fatalf("номер не присвоен: %q", code)
	}
	if g := actors["guest"].client.do("GET", "/api/documents/"+code, nil); g.Code != 200 {
		t.Errorf("читатель не открыл опубликованный документ: %d %s", g.Code, g.Body)
	}

	// архив: Редактор — да, Автор — нет; читателю архивное не видно
	if r := author.client.do("POST", teamDocPath(id, "/archive"), map[string]any{}); r.Code != 403 {
		t.Errorf("Автор убрал в архив: %d", r.Code)
	}
	if r := editor.client.do("POST", teamDocPath(id, "/archive"), map[string]any{"comment": "Устарело"}); r.Code != 200 || statusOf(r) != "archived" {
		t.Fatalf("архив: %d %s", r.Code, r.Body)
	}
	if g := actors["guest"].client.do("GET", "/api/documents/"+code, nil); g.Code != 404 {
		t.Errorf("архивное видно читателю: %d", g.Code)
	}
	if r := editor.client.do("POST", teamDocPath(id, "/unarchive"), map[string]any{}); r.Code != 200 || statusOf(r) != "published" {
		t.Errorf("возврат из архива: %d %s", r.Code, r.Body)
	}
}

func TestSelfReviewAndLintErrorsOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	editor := actors["all_roles"] // Автор и Редактор в одном лице

	id, rev := editor.createObject(t, "Свой документ")
	if r := editor.client.do("POST", teamDocPath(id, "/submit"), map[string]any{"base_revision": rev}); r.Code != 200 {
		t.Fatalf("отправка: %d %s", r.Code, r.Body)
	}
	if r := editor.client.do("POST", teamDocPath(id, "/verdict"), map[string]any{"verdict": "approve", "base_revision": rev}); r.Code != 403 || r.errCode() != "self_review" {
		t.Errorf("принял свой документ: %d %s", r.Code, r.Body)
	}
	if r := editor.client.do("POST", teamDocPath(id, "/comments"), map[string]any{"body": "сам"}); r.Code != 403 || r.errCode() != "self_review" {
		t.Errorf("прокомментировал свой документ: %d %s", r.Code, r.Body)
	}
	if r := actors["director"].client.do("POST", teamDocPath(id, "/verdict"), map[string]any{"verdict": "approve", "base_revision": rev}); r.Code != 200 {
		t.Errorf("Директорат не принял: %d %s", r.Code, r.Body)
	}

	// битая ссылка: 422 lint_failed с отчётом
	link := map[string]any{"id": "l", "type": "doc_link", "data": map[string]any{"code": "О-9998"}}
	bad, badRev := actors["author"].createObject(t, "С битой ссылкой", map[string]any{"id": "p", "type": "paragraph", "data": map[string]any{"text": "Текст"}}, link)
	lr := actors["author"].client.do("GET", teamDocPath(bad, "/lint"), nil)
	issues := lr.json()["lint"].(map[string]any)["issues"].([]any)
	if lr.Code != 200 || len(issues) == 0 || issues[0].(map[string]any)["code"] != "broken_link" || issues[0].(map[string]any)["block_id"] != "l" || issues[0].(map[string]any)["severity"] != "error" {
		t.Fatalf("линтер: %d %s", lr.Code, lr.Body)
	}
	r := actors["author"].client.do("POST", teamDocPath(bad, "/submit"), map[string]any{"base_revision": badRev})
	if r.Code != 422 || r.errCode() != "lint_failed" {
		t.Fatalf("отправка с битой ссылкой: %d %s", r.Code, r.Body)
	}
	body := r.json()["error"].(map[string]any)
	lint := body["lint"].(map[string]any)
	if lint["errors"].(float64) != 1 || !strings.Contains(body["message"].(string), "О-9998") {
		t.Errorf("отчёт в ошибке: %s", r.Body)
	}
	if st := statusOf(actors["author"].client.do("GET", teamDocPath(bad, ""), nil)); st != "draft" {
		t.Errorf("статус после отказа: %s", st)
	}
}

func TestReviewRoutesPermissionMatrixOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	author := actors["author"]
	id, rev := author.createObject(t, "Для матрицы")
	author.client.do("POST", teamDocPath(id, "/submit"), map[string]any{"base_revision": rev})

	type call struct {
		method, tail string
		body         any
	}
	calls := map[string]call{
		"review":  {"GET", "/review", nil},
		"lint":    {"GET", "/lint", nil},
		"comment": {"POST", "/comments", map[string]any{"body": "x"}},
		"verdict": {"POST", "/verdict", map[string]any{"verdict": "return", "comment": "x", "base_revision": rev}},
	}
	// кто что получает по документу на проверке: гость 401, не команда 403, чужие 404, автор и рецензенты — по правам
	want := map[string]map[string]int{
		"guest":     {"review": 401, "lint": 401, "comment": 401, "verdict": 401},
		"plain":     {"review": 403, "lint": 403, "comment": 403, "verdict": 403},
		"moderator": {"review": 404, "lint": 404, "comment": 404, "verdict": 404},
		"archivist": {"review": 404, "lint": 404, "comment": 404, "verdict": 404},
		"author":    {"review": 200, "lint": 200, "comment": 403, "verdict": 403},
	}
	for who, exp := range want {
		for name, cl := range calls {
			if got := actors[who].client.do(cl.method, teamDocPath(id, cl.tail), cl.body).Code; got != exp[name] {
				t.Errorf("%s: %s = %d, ожидалось %d", who, name, got, exp[name])
			}
		}
	}
	// Редактор и Директорат: читают, комментируют, выносят вердикт
	for _, who := range []string{"editor", "director"} {
		for _, name := range []string{"review", "lint", "comment"} {
			cl := calls[name]
			if got := actors[who].client.do(cl.method, teamDocPath(id, cl.tail), cl.body).Code; got != 200 && got != 201 {
				t.Errorf("%s: %s = %d", who, name, got)
			}
		}
	}
	// последним — вердикт (после него документ уходит с проверки)
	if got := actors["editor"].client.do("POST", teamDocPath(id, "/verdict"), calls["verdict"].body).Code; got != 200 {
		t.Errorf("editor: вердикт = %d", got)
	}
	// несуществующий документ и неверный номер
	for _, tail := range []string{"/review", "/lint"} {
		if got := author.client.do("GET", fmt.Sprintf("%s/999999%s", docsPath, tail), nil).Code; got != 404 {
			t.Errorf("несуществующий документ %s: %d", tail, got)
		}
	}
	if got := author.client.do("GET", docsPath+"/abc/review", nil).Code; got != 404 {
		t.Errorf("мусор вместо номера: %d", got)
	}
}

func TestReviewMutationsRejectForeignOriginAndBadBodies(t *testing.T) {
	_, actors := teamStackWithActors(t)
	author := actors["author"]
	id, rev := author.createObject(t, "CSRF")

	evil := *author.client
	evil.origin = "https://evil.example"
	for _, call := range []struct{ method, tail string }{
		{"POST", "/submit"}, {"POST", "/withdraw"}, {"POST", "/verdict"}, {"POST", "/archive"}, {"POST", "/unarchive"},
		{"POST", "/comments"}, {"PUT", "/comments/1"}, {"DELETE", "/comments/1"},
	} {
		if r := evil.do(call.method, teamDocPath(id, call.tail), map[string]any{"base_revision": rev}); r.Code != 403 || r.errCode() != "forbidden_origin" {
			t.Errorf("%s %s с чужого сайта: %d %s", call.method, call.tail, r.Code, r.errCode())
		}
	}
	if st := statusOf(author.client.do("GET", teamDocPath(id, ""), nil)); st != "draft" {
		t.Errorf("запросы с чужого сайта изменили документ: %s", st)
	}

	// комментарий, которого нет; неизвестный вердикт; мусорное тело
	director := actors["director"]
	author.client.do("POST", teamDocPath(id, "/submit"), map[string]any{"base_revision": rev})
	if r := director.client.do("PUT", teamDocPath(id, "/comments/999999"), map[string]any{"resolved": true}); r.Code != 404 {
		t.Errorf("несуществующий комментарий: %d", r.Code)
	}
	if r := director.client.do("DELETE", teamDocPath(id, "/comments/999999"), nil); r.Code != 404 {
		t.Errorf("удаление несуществующего комментария: %d", r.Code)
	}
	if r := director.client.do("POST", teamDocPath(id, "/verdict"), map[string]any{"verdict": "maybe", "base_revision": rev}); r.Code != 422 {
		t.Errorf("неизвестный вердикт: %d %s", r.Code, r.Body)
	}
	if r := director.client.do("POST", teamDocPath(id, "/comments"), map[string]any{"block_id": "нет", "body": "x"}); r.Code != 422 {
		t.Errorf("комментарий к несуществующему блоку: %d %s", r.Code, r.Body)
	}
	if r := director.client.do("POST", teamDocPath(id, "/comments"), map[string]any{"body": ""}); r.Code != 422 {
		t.Errorf("пустой комментарий: %d", r.Code)
	}
	if r := director.client.do("DELETE", teamDocPath(id, "/comments/abc"), nil); r.Code != 404 {
		t.Errorf("мусор вместо номера комментария: %d", r.Code)
	}
}

func TestTeamDashboardOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	author, editor := actors["author"], actors["editor"]
	id, rev := author.createObject(t, "Для стола")
	author.client.do("POST", teamDocPath(id, "/submit"), map[string]any{"base_revision": rev})

	for name, want := range map[string]int{"guest": 401, "plain": 403, "moderator": 200, "archivist": 200, "author": 200, "editor": 200, "director": 200} {
		if got := actors[name].client.do("GET", "/api/team/dashboard", nil).Code; got != want {
			t.Errorf("%s: рабочий стол = %d, ожидалось %d", name, got, want)
		}
	}

	a := author.client.do("GET", "/api/team/dashboard", nil).json()["dashboard"].(map[string]any)
	if a["can_review"] != false || len(a["queue"].([]any)) != 0 || len(a["in_review"].([]any)) != 1 || a["counts"].(map[string]any)["review"].(float64) != 1 {
		t.Errorf("рабочий стол автора: %v", a)
	}
	e := editor.client.do("GET", "/api/team/dashboard", nil).json()["dashboard"].(map[string]any)
	queue := e["queue"].([]any)
	if e["can_review"] != true || len(queue) != 1 || int64(queue[0].(map[string]any)["id"].(float64)) != id || e["queue_total"].(float64) != 1 {
		t.Errorf("очередь Редактора: %v", e)
	}
	// у документа без замечаний и возвратов лишних полей нет, списки — массивы, а не null
	for _, key := range []string{"returned", "drafts", "in_review", "queue"} {
		if _, ok := a[key].([]any); !ok {
			t.Errorf("%s: %T вместо массива", key, a[key])
		}
	}
	if r := author.client.do("POST", "/api/team/dashboard", map[string]any{}); r.Code != 405 && r.Code != 404 {
		t.Errorf("POST на рабочий стол: %d", r.Code)
	}
}

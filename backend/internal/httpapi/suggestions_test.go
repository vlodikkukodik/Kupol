package httpapi_test

import (
	"strconv"
	"testing"

	"kupol/internal/xp"
)

// «Предложения» (шаг 5.4): форма и собственные предложения — только вошедшим, очередь и решение — только
// тем, у кого право review (Редактор, Директорат); принятие начисляет автору XP.

func suggestionPath(id int64) string {
	return "/api/team/suggestions/" + strconv.FormatInt(id, 10) + "/status"
}

func TestSuggestionsPermissionMatrix(t *testing.T) {
	_, actors := teamStackWithActors(t)

	// Гость вообще не участвует
	guest := actors["guest"].client
	want(t, guest.do("POST", "/api/suggestions", map[string]any{"text": "идея"}), 401, "unauthenticated")
	want(t, guest.do("GET", "/api/suggestions", nil), 401, "unauthenticated")
	want(t, guest.do("GET", "/api/team/suggestions", nil), 401, "unauthenticated")

	// Очередь видят только те, у кого есть право review: Редактор и Директорат
	queue := map[string]int{
		"plain": 403, "author": 403, "moderator": 403, "archivist": 403,
		"editor": 200, "all_roles": 200, "director": 200,
	}
	for name, code := range queue {
		if got := actors[name].client.do("GET", "/api/team/suggestions", nil).Code; got != code {
			t.Errorf("%s: GET /team/suggestions = %d, ожидалось %d", name, got, code)
		}
	}

	// обычный читатель пишет и видит только своё
	plain := actors["plain"].client
	r := plain.do("POST", "/api/suggestions", map[string]any{"text": "почта нужна в каталоге"})
	if r.Code != 201 {
		t.Fatalf("создание: %d %s", r.Code, r.Body)
	}
	s := r.json()["suggestion"].(map[string]any)
	if s["status"] != "received" || s["author"] != actors["plain"].login {
		t.Fatalf("новое предложение: %v", s)
	}
	mine := plain.do("GET", "/api/suggestions", nil)
	want(t, mine, 200, "")
	if items := mine.json()["items"].([]any); len(items) != 1 {
		t.Fatalf("мои предложения: %d, ожидалось 1 (только свои)", len(items))
	}
	// решение без права — 403, даже у себя на предложении
	id := int64(s["id"].(float64))
	want(t, plain.do("POST", suggestionPath(id), map[string]any{"status": "accepted"}), 403, "forbidden")
}

func TestSuggestionsStatusFlowAndXP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	plain, editor := actors["plain"], actors["editor"]

	// XP читателя до предложения
	sess := plain.client.do("GET", "/api/auth/session", nil)
	want(t, sess, 200, "")
	before := int(sess.json()["user"].(map[string]any)["xp"].(float64))

	created := plain.client.do("POST", "/api/suggestions", map[string]any{"text": "добавить папки в реестр"})
	if created.Code != 201 {
		t.Fatalf("создание: %d %s", created.Code, created.Body)
	}
	id := int64(created.json()["suggestion"].(map[string]any)["id"].(float64))

	// очередь: свежее предложение видно Редактору
	queue := editor.client.do("GET", "/api/team/suggestions?status=received", nil)
	want(t, queue, 200, "")
	if queue.json()["total"].(float64) < 1 {
		t.Fatalf("очередь пуста: %s", queue.Body)
	}

	// получено → рассмотрено → принято (+XP автору)
	want(t, editor.client.do("POST", suggestionPath(id), map[string]any{"status": "reviewed", "comment": "Читаю"}), 200, "")
	accepted := editor.client.do("POST", suggestionPath(id), map[string]any{"status": "accepted", "comment": "Принято: сделаем"})
	if accepted.Code != 200 {
		t.Fatalf("принятие: %d %s", accepted.Code, accepted.Body)
	}
	got := accepted.json()["suggestion"].(map[string]any)
	if got["status"] != "accepted" || got["handled_by"] != editor.login || got["comment"] != "Принято: сделаем" {
		t.Fatalf("итог: %v", got)
	}

	sess = plain.client.do("GET", "/api/auth/session", nil)
	after := int(sess.json()["user"].(map[string]any)["xp"].(float64))
	if after-before != xp.SuggestionXP {
		t.Fatalf("XP автора: +%d, ожидалось +%d", after-before, xp.SuggestionXP)
	}

	// окончательный статус не меняется
	bad := editor.client.do("POST", suggestionPath(id), map[string]any{"status": "rejected"})
	if bad.Code != 409 || bad.errCode() != "invalid_state" {
		t.Fatalf("принято → отклонено: %d %s", bad.Code, bad.Body)
	}
	// неизвестный статус — ошибка поля
	bad = editor.client.do("POST", suggestionPath(id), map[string]any{"status": "approved"})
	if bad.Code != 422 || bad.errCode() != "validation" || bad.fields()["status"] == "" {
		t.Fatalf("неизвестный статус: %d %s", bad.Code, bad.Body)
	}
	// мусор в адресе — 404, как у несуществующего предложения
	want(t, editor.client.do("POST", "/api/team/suggestions/abc/status", map[string]any{"status": "accepted"}), 404, "not_found")
	want(t, editor.client.do("POST", "/api/team/suggestions/999999/status", map[string]any{"status": "accepted"}), 404, "not_found")

	// пустой текст — ошибка поля
	empty := plain.client.do("POST", "/api/suggestions", map[string]any{"text": "   "})
	if empty.Code != 422 || empty.fields()["text"] == "" {
		t.Fatalf("пустой текст: %d %s", empty.Code, empty.Body)
	}
}

func TestSuggestionsSelfReviewAndItalian(t *testing.T) {
	_, actors := teamStackWithActors(t)
	all := actors["all_roles"] // все роли, включая review, — но предложение своё

	created := all.client.do("POST", "/api/suggestions", map[string]any{"text": "своё предложение"})
	if created.Code != 201 {
		t.Fatalf("создание: %d %s", created.Code, created.Body)
	}
	id := int64(created.json()["suggestion"].(map[string]any)["id"].(float64))

	self := all.client.do("POST", suggestionPath(id), map[string]any{"status": "accepted"})
	if self.Code != 403 || self.errCode() != "self_review" {
		t.Fatalf("своё предложение: %d %s", self.Code, self.Body)
	}

	// ошибки на языке запроса (итальянский), код остаётся прежним
	all.client.lang = "it"
	editor := actors["editor"].client
	created2 := editor.do("POST", "/api/suggestions", map[string]any{"text": "чужое предложение"})
	id2 := int64(created2.json()["suggestion"].(map[string]any)["id"].(float64))
	if rejected := all.client.do("POST", suggestionPath(id2), map[string]any{"status": "rejected"}); rejected.Code != 200 {
		t.Fatalf("отклонение чужого: %d %s", rejected.Code, rejected.Body)
	}
	it := all.client.do("POST", suggestionPath(id2), map[string]any{"status": "accepted"})
	if it.Code != 409 || it.errCode() != "invalid_state" {
		t.Fatalf("по-итальянски: %d %s", it.Code, it.Body)
	}
	if msg := it.json()["error"].(map[string]any)["message"].(string); msg == "" || !isItalian(msg) {
		t.Errorf("сообщение 409: %q", msg)
	}
	// 404 тоже по-итальянски
	notFound := all.client.do("POST", suggestionPath(424242), map[string]any{"status": "accepted"})
	if msg := notFound.json()["error"].(map[string]any)["message"].(string); msg != "Proposta non trovata" {
		t.Errorf("сообщение 404: %q", msg)
	}
}

// isItalian — в переводе нет кириллицы (текст на итальянском).
func isItalian(s string) bool {
	for _, r := range s {
		if (r >= 'А' && r <= 'я') || r == 'Ё' || r == 'ё' {
			return false
		}
	}
	return true
}

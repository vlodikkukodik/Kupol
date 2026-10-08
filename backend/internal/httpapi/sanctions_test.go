package httpapi_test

import (
	"fmt"
	"testing"
)

func TestSanctionsOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	moder, director, author, guest := actors["moderator"], actors["director"], actors["author"], actors["guest"]
	publishMemo(t, author, director, "МЕМО-40", "Для пометок", nil)
	remark := func(who *teamActor) response {
		return who.client.do("POST", "/api/documents/MEMO-40/remarks", map[string]any{"text": "Пометка на полях."})
	}
	body := func(kind string, extra map[string]any) map[string]any {
		m := map[string]any{"login": author.login, "kind": kind, "reason": "Нарушение правил"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	if r := guest.client.do("POST", "/api/team/sanctions", body("warning", nil)); r.Code != 401 {
		t.Errorf("гость: %d", r.Code)
	}
	if r := actors["editor"].client.do("POST", "/api/team/sanctions", body("warning", nil)); r.Code != 403 {
		t.Errorf("Редактор без права модерации: %d", r.Code)
	}
	if r := moder.client.do("POST", "/api/team/sanctions", map[string]any{"login": author.login, "kind": "ban", "reason": ""}); r.Code != 422 {
		t.Errorf("пустая причина: %d", r.Code)
	}
	if r := moder.client.do("POST", "/api/team/sanctions", map[string]any{"login": director.login, "kind": "ban", "reason": "x"}); r.Code != 403 {
		t.Errorf("Директорат наказать нельзя: %d", r.Code)
	}

	// предупреждение: только записка
	if r := moder.client.do("POST", "/api/team/sanctions", body("warning", nil)); r.Code != 201 {
		t.Fatalf("предупреждение: %d %s", r.Code, r.Body)
	}
	msg := author.client.do("GET", "/api/me/inbox", nil).json()["items"].([]any)[0].(map[string]any)
	if msg["kind"] != "sanction" {
		t.Errorf("записка: %v", msg)
	}
	if r := remark(author); r.Code != 201 {
		t.Fatalf("после предупреждения писать можно: %d %s", r.Code, r.Body)
	}

	// блокировка комментариев
	cb := moder.client.do("POST", "/api/team/sanctions", body("comment_ban", map[string]any{"days": 3}))
	if cb.Code != 201 || cb.json()["sanction"].(map[string]any)["active"] != true {
		t.Fatalf("блокировка: %d %s", cb.Code, cb.Body)
	}
	if r := remark(author); r.Code != 403 || r.errCode() != "comments_blocked" {
		t.Errorf("пометка при блокировке: %d %s", r.Code, r.Body)
	}
	cbID := int64(cb.json()["sanction"].(map[string]any)["id"].(float64))
	if r := moder.client.do("POST", fmt.Sprintf("/api/team/sanctions/%d/revoke", cbID), nil); r.Code != 200 {
		t.Fatalf("снятие: %d", r.Code)
	}
	if r := moder.client.do("POST", fmt.Sprintf("/api/team/sanctions/%d/revoke", cbID), nil); r.Code != 409 {
		t.Errorf("повторное снятие: %d", r.Code)
	}
	if r := remark(author); r.Code != 201 {
		t.Errorf("после снятия писать можно: %d", r.Code)
	}

	// бан: сессия снята, вход закрыт; снятие бана возвращает вход
	ban := moder.client.do("POST", "/api/team/sanctions", body("ban", nil))
	if ban.Code != 201 {
		t.Fatalf("бан: %d %s", ban.Code, ban.Body)
	}
	if r := author.client.do("GET", "/api/me/inbox", nil); r.Code != 401 {
		t.Errorf("сессия после бана: %d", r.Code)
	}
	if r := author.client.do("POST", "/api/auth/login", map[string]any{"login": author.login, "password": teamPassword}); r.Code != 403 || r.errCode() != "banned" {
		t.Errorf("вход при бане: %d %s", r.Code, r.Body)
	}
	if r := author.client.do("POST", "/api/auth/login", map[string]any{"login": author.login, "password": "неверный пароль"}); r.Code != 401 {
		t.Errorf("неверный пароль не раскрывает бан: %d", r.Code)
	}
	banID := int64(ban.json()["sanction"].(map[string]any)["id"].(float64))
	if r := director.client.do("POST", fmt.Sprintf("/api/team/sanctions/%d/revoke", banID), nil); r.Code != 200 {
		t.Fatalf("снятие бана: %d", r.Code)
	}
	if r := author.client.do("POST", "/api/auth/login", map[string]any{"login": author.login, "password": teamPassword}); r.Code != 200 {
		t.Errorf("вход после снятия бана: %d %s", r.Code, r.Body)
	}
	list := moder.client.do("GET", "/api/team/sanctions?login="+author.login, nil).json()["items"].([]any)
	if len(list) != 3 {
		t.Errorf("журнал наказаний: %d записей", len(list))
	}
}

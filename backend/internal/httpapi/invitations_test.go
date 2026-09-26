package httpapi_test

import (
	"context"
	"fmt"
	"testing"
)

func TestInvitationsAndUserCardOverHTTP(t *testing.T) {
	st := newStack(t, nil)
	ctx := context.Background()
	mk := func(login string, level int) *client {
		c := st.newClient(t)
		if r := c.register(login, teamPassword); r.Code != 201 {
			t.Fatalf("регистрация %s: %d %s", login, r.Code, r.Body)
		}
		if level > 1 {
			if err := st.svc.SetLevel(ctx, login, level); err != nil {
				t.Fatal(err)
			}
		}
		return c
	}
	worker, junior, council := mk("invworker", 3), mk("invjunior", 2), mk("invsovet", 6)
	guest := st.newClient(t)
	inv := map[string]any{"login": "invworker", "message": "Ждём вас в Кураторах."}

	if r := worker.do("POST", "/api/team/invitations", inv); r.Code != 403 {
		t.Errorf("не член Совета: %d", r.Code)
	}
	if r := council.do("POST", "/api/team/invitations", map[string]any{"login": "invjunior"}); r.Code != 409 {
		t.Errorf("уровень 2 не приглашается: %d", r.Code)
	}
	if r := council.do("POST", "/api/team/invitations", map[string]any{"login": "нет_такого"}); r.Code != 404 {
		t.Errorf("неизвестный логин: %d", r.Code)
	}
	if r := council.do("POST", "/api/team/invitations", map[string]any{"login": "invsovet"}); r.Code != 403 {
		t.Errorf("себя нельзя: %d", r.Code)
	}
	// ходатайство висит — приглашение его закроет
	if r := worker.do("POST", "/api/petitions", map[string]any{"text": "Прошу."}); r.Code != 201 {
		t.Fatalf("ходатайство: %d", r.Code)
	}
	created := council.do("POST", "/api/team/invitations", inv)
	if created.Code != 201 {
		t.Fatalf("приглашение: %d %s", created.Code, created.Body)
	}
	id := int64(created.json()["invitation"].(map[string]any)["id"].(float64))
	if r := council.do("POST", "/api/team/invitations", inv); r.Code != 409 {
		t.Errorf("второе нерассмотренное: %d", r.Code)
	}
	if list := worker.do("GET", "/api/invitations", nil).json()["items"].([]any); len(list) != 1 {
		t.Fatalf("приглашения читателя: %v", list)
	}
	path := fmt.Sprintf("/api/invitations/%d/respond", id)
	if r := junior.do("POST", path, map[string]any{"answer": "accept"}); r.Code != 404 {
		t.Errorf("чужое приглашение: %d", r.Code)
	}
	if r := worker.do("POST", path, map[string]any{"answer": "maybe"}); r.Code != 422 {
		t.Errorf("неверный ответ: %d", r.Code)
	}
	if r := worker.do("POST", path, map[string]any{"answer": "accept"}); r.Code != 200 {
		t.Fatalf("принятие: %d %s", r.Code, r.Body)
	}
	if r := worker.do("POST", path, map[string]any{"answer": "accept"}); r.Code != 409 {
		t.Errorf("повторно: %d", r.Code)
	}
	if lvl := worker.do("GET", "/api/auth/session", nil).json()["user"].(map[string]any)["level"].(float64); lvl != 4 {
		t.Errorf("уровень: %v", lvl)
	}
	if p := worker.do("GET", "/api/petitions", nil).json()["items"].([]any); p[0].(map[string]any)["status"] != "approved" {
		t.Errorf("ходатайство после принятия: %v", p)
	}
	if msg := council.do("GET", "/api/me/inbox", nil).json()["items"].([]any)[0].(map[string]any); msg["kind"] != "invitation_answer" {
		t.Errorf("записка приглашавшему: %v", msg)
	}

	// отзыв и отказ
	junior2 := mk("invworker2", 3)
	c2 := council.do("POST", "/api/team/invitations", map[string]any{"login": "invworker2"})
	id2 := int64(c2.json()["invitation"].(map[string]any)["id"].(float64))
	if r := council.do("POST", fmt.Sprintf("/api/team/invitations/%d/withdraw", id2), nil); r.Code != 200 {
		t.Errorf("отзыв: %d", r.Code)
	}
	if r := junior2.do("POST", fmt.Sprintf("/api/invitations/%d/respond", id2), map[string]any{"answer": "decline"}); r.Code != 409 {
		t.Errorf("ответ на отозванное: %d", r.Code)
	}

	// карточка пользователя: только вошедшим, только ограниченные сведения
	if r := guest.do("GET", "/api/users/invworker", nil); r.Code != 401 {
		t.Errorf("гость: %d", r.Code)
	}
	if r := junior.do("GET", "/api/users/нет_такого", nil); r.Code != 404 {
		t.Errorf("неизвестный: %d", r.Code)
	}
	card := junior.do("GET", "/api/users/INVWORKER", nil)
	if card.Code != 200 {
		t.Fatalf("карточка: %d %s", card.Code, card.Body)
	}
	c := card.json()["card"].(map[string]any)
	if c["login"] != "invworker" || c["level"].(float64) != 4 || c["achievements"] == nil {
		t.Errorf("карточка: %v", c)
	}
	for _, secret := range []string{"xp", "email", "roles", "login_streak", "capabilities"} {
		if _, has := c[secret]; has {
			t.Errorf("карточка раскрывает %q", secret)
		}
	}
}

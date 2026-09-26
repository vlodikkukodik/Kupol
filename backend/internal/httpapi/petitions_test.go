package httpapi_test

import (
	"context"
	"fmt"
	"testing"
)

func TestPetitionsOverHTTP(t *testing.T) {
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
	junior, worker, council, council2 := mk("junior", 2), mk("worker", 3), mk("sovet1", 6), mk("sovet2", 6)
	guest := st.newClient(t)
	body := map[string]any{"text": "Прошу повысить допуск: много работал с делами."}

	if r := guest.do("POST", "/api/petitions", body); r.Code != 401 {
		t.Errorf("гость: %d", r.Code)
	}
	if r := junior.do("POST", "/api/petitions", body); r.Code != 409 {
		t.Errorf("уровень 2 не может ходатайствовать: %d", r.Code)
	}
	if r := worker.do("POST", "/api/petitions", map[string]any{"text": ""}); r.Code != 422 {
		t.Errorf("пустой текст: %d", r.Code)
	}
	created := worker.do("POST", "/api/petitions", body)
	if created.Code != 201 || created.json()["petition"].(map[string]any)["target_level"].(float64) != 4 {
		t.Fatalf("подача: %d %s", created.Code, created.Body)
	}
	id := int64(created.json()["petition"].(map[string]any)["id"].(float64))
	if r := worker.do("POST", "/api/petitions", body); r.Code != 409 {
		t.Errorf("второе нерассмотренное: %d", r.Code)
	}

	if r := worker.do("GET", "/api/team/petitions", nil); r.Code != 403 {
		t.Errorf("очередь не члену Совета: %d", r.Code)
	}
	q := council.do("GET", "/api/team/petitions", nil)
	if q.Code != 200 || len(q.json()["items"].([]any)) != 1 {
		t.Fatalf("очередь: %d %s", q.Code, q.Body)
	}
	path := fmt.Sprintf("/api/team/petitions/%d/decision", id)
	if r := worker.do("POST", path, map[string]any{"verdict": "approved"}); r.Code != 403 {
		t.Errorf("сам себе: %d", r.Code)
	}
	if r := council.do("POST", path, map[string]any{"verdict": "maybe"}); r.Code != 422 {
		t.Errorf("неверное решение: %d", r.Code)
	}
	ok := council2.do("POST", path, map[string]any{"verdict": "approved", "comment": "Заслужил."})
	if ok.Code != 200 || ok.json()["petition"].(map[string]any)["status"] != "approved" {
		t.Fatalf("решение: %d %s", ok.Code, ok.Body)
	}
	if r := council.do("POST", path, map[string]any{"verdict": "rejected"}); r.Code != 409 {
		t.Errorf("повторное решение: %d", r.Code)
	}

	// допуск поднят, автору пришла записка
	sess := worker.do("GET", "/api/auth/session", nil).json()["user"].(map[string]any)
	if sess["level"].(float64) != 4 {
		t.Errorf("уровень после одобрения: %v", sess["level"])
	}
	msg := worker.do("GET", "/api/me/inbox", nil).json()["items"].([]any)[0].(map[string]any)
	if msg["kind"] != "petition" {
		t.Errorf("записка: %v", msg)
	}
	mine := worker.do("GET", "/api/petitions", nil).json()["items"].([]any)
	if len(mine) != 1 || mine[0].(map[string]any)["status"] != "approved" {
		t.Errorf("мои ходатайства: %v", mine)
	}
}

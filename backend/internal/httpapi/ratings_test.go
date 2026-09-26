package httpapi_test

import (
	"context"
	"testing"

	"kupol/internal/documents"
	"kupol/internal/xp"
)

func TestRatingsEndToEnd(t *testing.T) {
	st := newStack(t, nil)
	ctx := context.Background()
	if _, err := st.docs.Import(ctx, []byte(`{"code":"О-301","type":"object","title":"Тест оценок","status":"published","level":0,"composed":{"year":1979}}`), documents.ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	guest := st.newClient(t)
	reader1 := st.newClient(t)
	reader1.register("ratreader1", teamPassword)
	reader2 := st.newClient(t)
	reader2.register("ratreader2", teamPassword)

	// гость видит счётчики, но не может ставить оценку
	r := guest.do("GET", "/api/documents/O-301/ratings", nil)
	want(t, r, 200, "")
	counts := r.json()["ratings"].(map[string]any)["counts"].(map[string]any)
	if counts["acknowledged"] != float64(0) || counts["approved"] != float64(0) || counts["doubtful"] != float64(0) {
		t.Fatalf("пустые счётчики: %v", counts)
	}
	want(t, guest.do("POST", "/api/documents/O-301/ratings", map[string]any{"rating": "approved"}), 401, "unauthenticated")

	// reader1 ставит «одобряю»
	r = reader1.do("POST", "/api/documents/O-301/ratings", map[string]any{"rating": "approved"})
	want(t, r, 200, "")
	ratings := r.json()["ratings"].(map[string]any)
	counts = ratings["counts"].(map[string]any)
	if counts["approved"] != float64(1) {
		t.Fatalf("счётчик approved: %v", counts)
	}
	my := ratings["my"].(map[string]any)
	if my["rating"] != "approved" {
		t.Fatalf("моя оценка: %v", my)
	}

	// reader2 ставит «сомнительно»
	r = reader2.do("POST", "/api/documents/O-301/ratings", map[string]any{"rating": "doubtful"})
	want(t, r, 200, "")
	counts = r.json()["ratings"].(map[string]any)["counts"].(map[string]any)
	if counts["approved"] != float64(1) || counts["doubtful"] != float64(1) {
		t.Fatalf("счётчики после второй оценки: %v", counts)
	}

	// reader1 меняет оценку на «ознакомлен» (без повторного XP)
	r = reader1.do("POST", "/api/documents/O-301/ratings", map[string]any{"rating": "acknowledged"})
	want(t, r, 200, "")
	counts = r.json()["ratings"].(map[string]any)["counts"].(map[string]any)
	if counts["acknowledged"] != float64(1) || counts["approved"] != float64(0) {
		t.Fatalf("счётчики после замены: %v", counts)
	}

	// reader1 повторно ставит «ознакомлен» — toggle убирает оценку
	r = reader1.do("POST", "/api/documents/O-301/ratings", map[string]any{"rating": "acknowledged"})
	want(t, r, 200, "")
	counts = r.json()["ratings"].(map[string]any)["counts"].(map[string]any)
	if counts["acknowledged"] != float64(0) {
		t.Fatalf("toggle не убрал оценку: %v", counts)
	}
	my = r.json()["ratings"].(map[string]any)["my"].(map[string]any)
	if my["rating"] != nil {
		t.Fatalf("моя оценка после toggle: %v", my)
	}

	// DELETE убирает оценку reader2
	r = reader1.do("POST", "/api/documents/O-301/ratings", map[string]any{"rating": "approved"})
	want(t, r, 200, "")
	r = reader1.do("DELETE", "/api/documents/O-301/ratings", nil)
	want(t, r, 200, "")
	counts = r.json()["ratings"].(map[string]any)["counts"].(map[string]any)
	if counts["approved"] != float64(0) || counts["doubtful"] != float64(1) {
		t.Fatalf("счётчики после удаления: %v", counts)
	}

	// гость не может удалить
	r = guest.do("DELETE", "/api/documents/O-301/ratings", nil)
	want(t, r, 401, "unauthenticated")

	// некорректная оценка
	r = reader1.do("POST", "/api/documents/O-301/ratings", map[string]any{"rating": "wrong"})
	want(t, r, 422, "validation")
}

func TestRatingsXP(t *testing.T) {
	st := newStack(t, nil)
	ctx := context.Background()
	if _, err := st.docs.Import(ctx, []byte(`{"code":"О-302","type":"object","title":"Тест XP оценок","status":"published","level":0,"composed":{"year":1979}}`), documents.ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	reader := st.newClient(t)
	reader.register("ratxp1", teamPassword)

	// запоминаем XP до оценки (вход уже дал 10 XP)
	sess := reader.do("GET", "/api/auth/session", nil)
	want(t, sess, 200, "")
	user := sess.json()["user"].(map[string]any)
	xpBefore := int(user["xp"].(float64))

	// оценка начисляет XP
	r := reader.do("POST", "/api/documents/O-302/ratings", map[string]any{"rating": "approved"})
	want(t, r, 200, "")

	sess = reader.do("GET", "/api/auth/session", nil)
	user = sess.json()["user"].(map[string]any)
	xpAfter := int(user["xp"].(float64))
	if xpAfter-xpBefore != xp.RatingXP {
		t.Fatalf("XP после оценки: +%d, хотели +%d", xpAfter-xpBefore, xp.RatingXP)
	}

	// замена оценки не начисляет повторный XP
	r = reader.do("POST", "/api/documents/O-302/ratings", map[string]any{"rating": "doubtful"})
	want(t, r, 200, "")

	sess = reader.do("GET", "/api/auth/session", nil)
	user = sess.json()["user"].(map[string]any)
	xpAfterReplace := int(user["xp"].(float64))
	if xpAfterReplace != xpAfter {
		t.Fatalf("XP после замены: %d (было %d), не должно меняться", xpAfterReplace, xpAfter)
	}
}

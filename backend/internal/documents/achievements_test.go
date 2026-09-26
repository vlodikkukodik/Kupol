package documents

import (
	"testing"

	"kupol/internal/achievements"
	"kupol/internal/i18n"
)

// TestReadingTenDocumentsGrantsAchievement — десятое прочитанное дело выдаёт грамоту read_10 (шаг 5.6).
// recordRead — best-effort, поэтому проверяем через achievements.List, а не через ответ Get().
func TestReadingTenDocumentsGrantsAchievement(t *testing.T) {
	e := newEnv(t)
	uid := e.user("reader10")
	v := Viewer{UserID: uid, UserLevel: 1, Lang: i18n.RU}

	for i := 1; i <= 10; i++ {
		code := "МЕМО-" + itoa(i)
		e.imp(`{"code":"` + code + `","type":"memo","title":"Дело","status":"published","composed":{"year":1979},"blocks":[]}`)
		if _, err := e.svc.Get(ctx, v, code); err != nil {
			t.Fatalf("чтение %s: %v", code, err)
		}
	}

	items, err := achievements.List(ctx, e.db, uid, i18n.RU)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != achievements.Read10 {
		t.Fatalf("грамоты: %+v", items)
	}
}

// TestRedeemCodeGrantsSecretFinderAchievement — погашение скрытого кода выдаёт грамоту secret_finder
// и сразу возвращает её в RedeemResult.NewAchievements.
func TestRedeemCodeGrantsSecretFinderAchievement(t *testing.T) {
	e := newEnv(t)
	acts := e.actors()
	e.imp(`{"code":"О-1","type":"object","title":"Тайна","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},"blocks":[]}`)
	if _, err := e.svc.SecretCodeCreate(ctx, acts.director, SecretCodeInput{Code: "ГРАМОТА", DocumentRef: "О-1", RewardXP: 5}); err != nil {
		t.Fatal(err)
	}

	uid := e.user("hunter")
	v := Viewer{UserID: uid, Lang: i18n.RU}
	res, err := e.svc.RedeemCode(ctx, v, "грамота")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NewAchievements) != 1 || res.NewAchievements[0] != achievements.SecretFinder {
		t.Fatalf("новые грамоты: %+v", res.NewAchievements)
	}

	// другой скрытый код тем же аккаунтом — secret_finder уже не «новая»
	if _, err := e.svc.SecretCodeCreate(ctx, acts.director, SecretCodeInput{Code: "ЕЩЁ", DocumentRef: "О-1", RewardXP: 1}); err != nil {
		t.Fatal(err)
	}
	res2, err := e.svc.RedeemCode(ctx, v, "ЕЩЁ")
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.NewAchievements) != 0 {
		t.Fatalf("повторная грамота: %+v", res2.NewAchievements)
	}
}

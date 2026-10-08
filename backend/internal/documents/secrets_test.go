package documents

import (
	"testing"

	"kupol/internal/i18n"
)

func TestSecretCodeCreateOnlyDirectorate(t *testing.T) {
	e := newEnv(t)
	e.imp(`{"code":"О-1","type":"object","title":"Тайна","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},"blocks":[]}`)
	acts := e.actors()

	if _, err := e.svc.SecretCodeCreate(ctx, acts.editor, SecretCodeInput{Code: "СЕЗАМ", DocumentRef: "О-1", RewardXP: 50}); err != ErrForbidden {
		t.Fatalf("Редактор: ожидался ErrForbidden, получено %v", err)
	}
	c, err := e.svc.SecretCodeCreate(ctx, acts.director, SecretCodeInput{Code: "сезам", DocumentRef: "О-1", RewardXP: 50})
	if err != nil {
		t.Fatal(err)
	}
	if c.DocumentRef != "О-001" || c.RewardXP != 50 {
		t.Errorf("код: %+v", c)
	}

	list, err := e.svc.SecretCodeList(ctx, acts.director)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("список: %+v", list)
	}
}

func TestSecretCodeCreateValidation(t *testing.T) {
	e := newEnv(t)
	acts := e.actors()
	e.imp(`{"code":"О-1","type":"object","title":"Тайна","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},
		"blocks":[{"id":"p1","type":"paragraph","data":{"text":"Текст"}}]}`)

	if _, err := e.svc.SecretCodeCreate(ctx, acts.director, SecretCodeInput{Code: "К1", DocumentRef: "О-999"}); problemPaths(err)[0] != "document_ref" {
		t.Errorf("несуществующий документ: %v", err)
	}
	if _, err := e.svc.SecretCodeCreate(ctx, acts.director, SecretCodeInput{Code: "К2", DocumentRef: "О-1", BlockID: "нет-такого"}); problemPaths(err)[0] != "block_id" {
		t.Errorf("несуществующий блок: %v", err)
	}
	if _, err := e.svc.SecretCodeCreate(ctx, acts.director, SecretCodeInput{Code: "", DocumentRef: "О-1"}); problemPaths(err)[0] != "code" {
		t.Errorf("пустой код: %v", err)
	}

	if _, err := e.svc.SecretCodeCreate(ctx, acts.director, SecretCodeInput{Code: "К3", DocumentRef: "О-1", BlockID: "p1"}); err != nil {
		t.Fatalf("код с блоком: %v", err)
	}

	// повтор того же кода — конфликт (уникальность)
	if _, err := e.svc.SecretCodeCreate(ctx, acts.director, SecretCodeInput{Code: "К3", DocumentRef: "О-1"}); problemPaths(err)[0] != "code" {
		t.Errorf("повтор кода: %v", err)
	}
}

func TestRedeemCodeGrantsAccessAndXP(t *testing.T) {
	e := newEnv(t)
	acts := e.actors()
	// уровень выше обычного и статус «архив» — недостижим обычным путём
	e.imp(`{"code":"О-1","type":"object","title":"Секретный объект","status":"archived","level":6,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},
		"blocks":[{"id":"p1","type":"paragraph","data":{"text":"Тайный текст"}}]}`)
	if _, err := e.svc.SecretCodeCreate(ctx, acts.director, SecretCodeInput{Code: "ОТКРОЙСЯ", DocumentRef: "О-1", RewardXP: 42}); err != nil {
		t.Fatal(err)
	}

	uid := e.user("finder")
	v := Viewer{UserID: uid, UserLevel: 1, Lang: i18n.RU}

	// до погашения — не видно
	if _, err := e.svc.Get(ctx, v, "О-1"); err != ErrNotFound {
		t.Fatalf("до погашения: ожидался ErrNotFound, получено %v", err)
	}

	res, err := e.svc.RedeemCode(ctx, v, " откройся ")
	if err != nil {
		t.Fatalf("погашение: %v", err)
	}
	if res.AwardedXP != 42 || res.DocumentRef != "О-001" {
		t.Errorf("результат: %+v", res)
	}

	// после погашения — виден целиком, блоки не зачернены несмотря на уровень 6 > уровень читателя 1
	d, err := e.svc.Get(ctx, v, "О-1")
	if err != nil {
		t.Fatalf("после погашения: %v", err)
	}
	if len(d.Blocks) != 1 || d.Blocks[0].Type != "paragraph" {
		t.Errorf("блоки после разблокировки: %+v", d.Blocks)
	}

	// повторное погашение тем же аккаунтом — ошибка, XP не начисляется повторно
	if _, err := e.svc.RedeemCode(ctx, v, "ОТКРОЙСЯ"); err != ErrCodeAlreadyRedeemed {
		t.Fatalf("повтор: ожидался ErrCodeAlreadyRedeemed, получено %v", err)
	}

	// чужому аккаунту документ всё ещё недоступен
	other := Viewer{UserID: e.user("outsider"), UserLevel: 1, Lang: i18n.RU}
	if _, err := e.svc.Get(ctx, other, "О-1"); err != ErrNotFound {
		t.Errorf("чужой аккаунт: ожидался ErrNotFound, получено %v", err)
	}

	// неизвестный код — тот же ErrNotFound, без входа — ErrForbidden
	if _, err := e.svc.RedeemCode(ctx, v, "НЕТТАКОГО"); err != ErrNotFound {
		t.Errorf("неизвестный код: %v", err)
	}
	if _, err := e.svc.RedeemCode(ctx, Guest, "ОТКРОЙСЯ"); err != ErrForbidden {
		t.Errorf("гость: %v", err)
	}
}

func TestSecretCodeDeleteOnlyDirectorate(t *testing.T) {
	e := newEnv(t)
	acts := e.actors()
	e.imp(`{"code":"О-1","type":"object","title":"Тайна","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},"blocks":[]}`)
	c, err := e.svc.SecretCodeCreate(ctx, acts.director, SecretCodeInput{Code: "УДАЛИ", DocumentRef: "О-1"})
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := e.db.Raw(`SELECT id FROM secret_codes WHERE code = ?`, "УДАЛИ").Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SecretCodeDelete(ctx, acts.editor, id); err != ErrForbidden {
		t.Fatalf("Редактор: %v", err)
	}
	if err := e.svc.SecretCodeDelete(ctx, acts.director, id); err != nil {
		t.Fatalf("Директорат: %v", err)
	}
	if err := e.svc.SecretCodeDelete(ctx, acts.director, id); err != ErrNotFound {
		t.Errorf("повтор удаления: %v", err)
	}
	_ = c
}

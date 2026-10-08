package httpapi_test

import (
	"fmt"
	"testing"
)

const secretCodesPath = "/api/team/secret-codes"

func TestSecretCodesAdminOnlyDirectorate(t *testing.T) {
	_, actors := teamStackWithActors(t)
	author, editor, director, guest := actors["author"], actors["editor"], actors["director"], actors["guest"]
	publishMemo(t, author, director, "МЕМО-30", "Секретное дело", nil)

	body := map[string]any{"code": "ТАЙНА-1", "document_ref": "МЕМО-30", "reward_xp": 30}
	if r := editor.client.do("POST", secretCodesPath, body); r.Code != 403 {
		t.Errorf("Редактор создаёт код: %d %s", r.Code, r.Body)
	}
	if r := guest.client.do("POST", secretCodesPath, body); r.Code != 401 {
		t.Errorf("гость: %d", r.Code)
	}

	created := director.client.do("POST", secretCodesPath, body)
	if created.Code != 201 {
		t.Fatalf("Директорат создаёт код: %d %s", created.Code, created.Body)
	}
	id := int64(created.json()["code"].(map[string]any)["id"].(float64))

	list := director.client.do("GET", secretCodesPath, nil)
	if list.Code != 200 || len(list.json()["codes"].([]any)) != 1 {
		t.Fatalf("список: %d %s", list.Code, list.Body)
	}
	if r := editor.client.do("GET", secretCodesPath, nil); r.Code != 403 {
		t.Errorf("Редактор смотрит список: %d", r.Code)
	}

	delPath := fmt.Sprintf("%s/%d", secretCodesPath, id)
	if r := editor.client.do("DELETE", delPath, nil); r.Code != 403 {
		t.Errorf("Редактор удаляет: %d", r.Code)
	}
	if r := director.client.do("DELETE", delPath, nil); r.Code != 204 {
		t.Fatalf("Директорат удаляет: %d %s", r.Code, r.Body)
	}
}

func TestRedeemSecretCodeOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	author, director, guest := actors["author"], actors["director"], actors["guest"]
	publishMemo(t, author, director, "МЕМО-31", "Ещё одно секретное дело", nil)

	created := director.client.do("POST", secretCodesPath, map[string]any{"code": "ОТКРОЙСЯ-31", "document_ref": "МЕМО-31", "reward_xp": 15})
	if created.Code != 201 {
		t.Fatalf("создание кода: %d %s", created.Code, created.Body)
	}

	if r := guest.client.do("POST", "/api/secret-codes/redeem", map[string]any{"code": "ОТКРОЙСЯ-31"}); r.Code != 401 {
		t.Errorf("гость гасит код: %d", r.Code)
	}

	finder := actors["editor"] // любой вошедший читатель, не обязательно с ролями
	redeemed := finder.client.do("POST", "/api/secret-codes/redeem", map[string]any{"code": "откройся-31"})
	if redeemed.Code != 200 || redeemed.json()["awarded_xp"].(float64) != 15 {
		t.Fatalf("погашение: %d %s", redeemed.Code, redeemed.Body)
	}

	again := finder.client.do("POST", "/api/secret-codes/redeem", map[string]any{"code": "ОТКРОЙСЯ-31"})
	if again.Code != 409 || again.errCode() != "already_redeemed" {
		t.Errorf("повтор: %d %s", again.Code, again.Body)
	}

	unknown := finder.client.do("POST", "/api/secret-codes/redeem", map[string]any{"code": "НЕТТАКОГО"})
	if unknown.Code != 404 {
		t.Errorf("неизвестный код: %d %s", unknown.Code, unknown.Body)
	}
}

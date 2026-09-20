package httpapi_test

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // RFC 6238
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"
)

// otp — независимая от сервера запись RFC 6238 (SHA-1, 6 цифр, 30 секунд): тест не берёт ничего у проверяемого кода.
func otp(t *testing.T, secretB32 string, offsetSteps int64) string {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretB32)
	if err != nil {
		t.Fatalf("секрет %q: %v", secretB32, err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(time.Now().Unix()/30+offsetSteps))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1_000_000)
}

func TestTOTPOverHTTP(t *testing.T) {
	st := newStack(t, nil)
	const pw = "правильный пароль"
	c := st.newClient(t)
	reg := c.register("Vera", pw)
	if reg.Code != 201 {
		t.Fatalf("регистрация: %d %s", reg.Code, reg.Body)
	}
	backup := reg.json()["backup_code"].(string)
	user := func(r response) map[string]any { return r.json()["user"].(map[string]any) }
	if user(reg)["totp_enabled"] != false {
		t.Errorf("новая учётная запись с включённой защитой: %v", user(reg))
	}

	// всё это — только для вошедшего
	guest := st.newClient(t)
	for _, call := range [][2]string{{"GET", "/api/me/totp"}, {"POST", "/api/me/totp/setup"}, {"POST", "/api/me/totp/enable"}, {"POST", "/api/me/totp/disable"}, {"POST", "/api/me/totp/recovery-codes"}} {
		if r := guest.do(call[0], call[1], map[string]any{}); r.Code != 401 {
			t.Errorf("гость %s %s: %d", call[0], call[1], r.Code)
		}
	}
	if r := c.do("GET", "/api/me/totp", nil); r.Code != 200 || r.json()["enabled"] != false || r.json()["recovery_left"].(float64) != 0 {
		t.Errorf("состояние: %d %s", r.Code, r.Body)
	}

	// подключение: пароль обязателен; секрет и ссылка выдаются, но защита пока не включена
	if r := c.do("POST", "/api/me/totp/setup", map[string]any{"password": "чужой пароль"}); r.Code != 403 || r.errCode() != "wrong_password" {
		t.Errorf("setup с чужим паролем: %d %s", r.Code, r.Body)
	}
	setup := c.do("POST", "/api/me/totp/setup", map[string]any{"password": pw})
	if setup.Code != 200 || setup.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("setup: %d %v", setup.Code, setup.Header())
	}
	secret := setup.json()["secret"].(string)
	if uri := setup.json()["uri"].(string); !strings.HasPrefix(uri, "otpauth://totp/") || !strings.Contains(uri, "secret="+secret) {
		t.Errorf("ссылка: %s", uri)
	}
	if r := st.newClient(t).do("POST", "/api/auth/login", map[string]any{"login": "Vera", "password": pw}); r.Code != 200 {
		t.Errorf("вход до подтверждения кода: %d", r.Code)
	}

	// включение: неверный код — ошибка поля code; верный — десять одноразовых кодов
	if r := c.do("POST", "/api/me/totp/enable", map[string]any{"code": "000000"}); r.Code != 422 || r.errCode() != "totp_invalid" || r.fields()["code"] == nil {
		t.Errorf("неверный код при включении: %d %s", r.Code, r.Body)
	}
	en := c.do("POST", "/api/me/totp/enable", map[string]any{"code": otp(t, secret, 0)})
	if en.Code != 200 {
		t.Fatalf("включение: %d %s", en.Code, en.Body)
	}
	codes := en.json()["recovery_codes"].([]any)
	if len(codes) != 10 || en.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("одноразовые коды: %v %v", codes, en.Header())
	}
	if r := c.do("GET", "/api/auth/session", nil); r.Code != 200 || user(r)["totp_enabled"] != true {
		t.Errorf("сессия после включения: %d %s", r.Code, r.Body)
	}
	if r := c.do("GET", "/api/me/totp", nil); r.json()["enabled"] != true || r.json()["recovery_left"].(float64) != 10 {
		t.Errorf("состояние после включения: %s", r.Body)
	}
	if r := c.do("POST", "/api/me/totp/setup", map[string]any{"password": pw}); r.Code != 409 || r.errCode() != "totp_already_enabled" {
		t.Errorf("повторное подключение: %d %s", r.Code, r.Body)
	}

	// вход: пароль без кода → 401 totp_required, сессии нет; неверный код → 422 с полем totp; чужой пароль — обычная ошибка
	login := func(body map[string]any) (*client, response) {
		lc := st.newClient(t)
		return lc, lc.do("POST", "/api/auth/login", body)
	}
	if lc, r := login(map[string]any{"login": "Vera", "password": pw}); r.Code != 401 || r.errCode() != "totp_required" || lc.cookie != nil {
		t.Errorf("без кода: %d %s", r.Code, r.Body)
	}
	if lc, r := login(map[string]any{"login": "Vera", "password": "чужой пароль", "totp": otp(t, secret, 1)}); r.Code != 401 || r.errCode() != "invalid_credentials" || lc.cookie != nil {
		t.Errorf("чужой пароль: %d %s", r.Code, r.Body)
	}
	if lc, r := login(map[string]any{"login": "Vera", "password": pw, "totp": "123456"}); r.Code != 422 || r.errCode() != "totp_invalid" || r.fields()["totp"] == nil || lc.cookie != nil {
		t.Errorf("неверный код: %d %s", r.Code, r.Body)
	}
	// код следующего шага подходит (часы телефона вперёд), тот же — второй раз нет
	next := otp(t, secret, 1)
	lc, r := login(map[string]any{"login": "Vera", "password": pw, "totp": next})
	if r.Code != 200 || lc.cookie == nil || user(r)["totp_enabled"] != true {
		t.Fatalf("вход с кодом: %d %s", r.Code, r.Body)
	}
	if _, r := login(map[string]any{"login": "Vera", "password": pw, "totp": next}); r.Code != 422 || r.errCode() != "totp_invalid" {
		t.Errorf("повтор кода: %d %s", r.Code, r.Body)
	}
	// одноразовый код: работает один раз, число оставшихся уменьшается
	rc := codes[0].(string)
	rcClient, r := login(map[string]any{"login": "Vera", "password": pw, "totp": strings.ToUpper(rc)})
	if r.Code != 200 {
		t.Fatalf("вход по одноразовому коду: %d %s", r.Code, r.Body)
	}
	if _, r := login(map[string]any{"login": "Vera", "password": pw, "totp": rc}); r.Code != 422 {
		t.Errorf("одноразовый код сработал дважды: %d", r.Code)
	}
	if r := rcClient.do("GET", "/api/me/totp", nil); r.json()["recovery_left"].(float64) != 9 {
		t.Errorf("осталось кодов: %s", r.Body)
	}

	// восстановление по резервному коду меняет пароль, но не заменяет код: сессии нет
	rest := st.newClient(t)
	rr := rest.do("POST", "/api/auth/restore", map[string]any{"login": "Vera", "backup_code": backup, "new_password": "новый пароль 2026"})
	if rr.Code != 200 || rr.json()["user"] != nil || rr.json()["backup_code"] == "" || rest.cookie != nil {
		t.Fatalf("восстановление: %d %s (кука: %v)", rr.Code, rr.Body, rest.cookie)
	}
	if _, r := login(map[string]any{"login": "Vera", "password": "новый пароль 2026"}); r.Code != 401 || r.errCode() != "totp_required" {
		t.Errorf("вход новым паролем без кода: %d %s", r.Code, r.Body)
	}
	if _, r := login(map[string]any{"login": "Vera", "password": "новый пароль 2026", "totp": codes[1].(string)}); r.Code != 200 {
		t.Errorf("вход новым паролем с одноразовым кодом: %d %s", r.Code, r.Body)
	}

	// новые коды и выключение: пароль и код обязательны
	c2, r := login(map[string]any{"login": "Vera", "password": "новый пароль 2026", "totp": codes[2].(string)})
	if r.Code != 200 {
		t.Fatal(r.Body)
	}
	if r := c2.do("POST", "/api/me/totp/disable", map[string]any{"password": "чужой пароль", "code": codes[3]}); r.Code != 403 || r.errCode() != "wrong_password" {
		t.Errorf("выключение с чужим паролем: %d %s", r.Code, r.Body)
	}
	if r := c2.do("POST", "/api/me/totp/disable", map[string]any{"password": "новый пароль 2026", "code": "123456"}); r.Code != 422 || r.fields()["code"] == nil {
		t.Errorf("выключение с неверным кодом: %d %s", r.Code, r.Body)
	}
	fresh := c2.do("POST", "/api/me/totp/recovery-codes", map[string]any{"password": "новый пароль 2026", "code": codes[3]})
	if fresh.Code != 200 || len(fresh.json()["recovery_codes"].([]any)) != 10 {
		t.Fatalf("новые коды: %d %s", fresh.Code, fresh.Body)
	}
	if d := c2.do("POST", "/api/me/totp/disable", map[string]any{"password": "новый пароль 2026", "code": codes[4]}); d.Code != 422 {
		t.Errorf("прежний одноразовый код после обновления: %d", d.Code)
	}
	if d := c2.do("POST", "/api/me/totp/disable", map[string]any{"password": "новый пароль 2026", "code": fresh.json()["recovery_codes"].([]any)[0]}); d.Code != 204 {
		t.Fatalf("выключение: %d %s", d.Code, d.Body)
	}
	if r := c2.do("GET", "/api/me/totp", nil); r.json()["enabled"] != false {
		t.Errorf("после выключения: %s", r.Body)
	}
	if _, r := login(map[string]any{"login": "Vera", "password": "новый пароль 2026"}); r.Code != 200 {
		t.Errorf("вход одним паролем после выключения: %d %s", r.Code, r.Body)
	}
	if r := c2.do("POST", "/api/me/totp/disable", map[string]any{"password": "новый пароль 2026", "code": "123456"}); r.Code != 409 || r.errCode() != "totp_not_enabled" {
		t.Errorf("повторное выключение: %d %s", r.Code, r.Body)
	}
	// у человека без защиты лишнее поле totp при входе не мешает
	if _, r := login(map[string]any{"login": "Vera", "password": "новый пароль 2026", "totp": "000000"}); r.Code != 200 {
		t.Errorf("вход без защиты с лишним кодом: %d %s", r.Code, r.Body)
	}
}

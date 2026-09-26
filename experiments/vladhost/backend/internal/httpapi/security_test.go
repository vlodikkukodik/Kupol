package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vladhost/internal/auth"
)

type sessionsBody struct {
	Sessions []struct {
		ID        int64  `json:"id"`
		UserAgent string `json:"user_agent"`
		Current   bool   `json:"current"`
	} `json:"sessions"`
}

func (e *env) loginAs(login, pw, agent string) (string, *http.Cookie) {
	e.t.Helper()
	req := map[string]string{"login": login, "password": pw}
	w := e.doUA("POST", "/api/auth/login", req, "", agent)
	if w.Code != 200 {
		e.t.Fatalf("вход %s: %d %s", login, w.Code, w.Body)
	}
	return decode[sessionBody](e.t, w).AccessToken, refreshCookie(e.t, w)
}

// doUA — запрос с заданной программой клиента (она попадает в список сессий).
func (e *env) doUA(method, path string, body any, token, agent string) *httptest.ResponseRecorder {
	e.t.Helper()
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", agent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func TestSessionsListAndRevoke(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	e.user(adm, "john")

	tokA, _ := e.loginAs("john", "password123", "Laptop")
	tokB, ckB := e.loginAs("john", "password123", "Phone")

	list := decode[sessionsBody](t, e.do("GET", "/api/me/sessions", nil, tokA))
	// Регистрация — тоже сессия: всего три, текущая одна.
	if len(list.Sessions) != 3 {
		t.Fatalf("сессий: %+v", list)
	}
	var phone, cur int64
	for _, s := range list.Sessions {
		if s.UserAgent == "Phone" {
			phone = s.ID
		}
		if s.Current {
			cur = s.ID
		}
	}
	if phone == 0 || cur == 0 || cur == phone {
		t.Fatalf("сессии: %+v", list)
	}

	// Чужой не видит и не закрывает.
	e.user(adm, "mary")
	tokM, _ := e.loginAs("mary", "password123", "X")
	if w := e.do("DELETE", fmt.Sprintf("/api/me/sessions/%d", phone), nil, tokM); w.Code != 404 {
		t.Fatalf("чужая сессия: %d", w.Code)
	}
	// Текущую так не закрыть.
	if w := e.do("DELETE", fmt.Sprintf("/api/me/sessions/%d", cur), nil, tokA); w.Code != 422 {
		t.Fatalf("текущая: %d", w.Code)
	}

	// Закрываем телефон: его access-токен и refresh перестают работать сразу.
	if w := e.do("DELETE", fmt.Sprintf("/api/me/sessions/%d", phone), nil, tokA); w.Code != 204 {
		t.Fatalf("закрытие: %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/api/me", nil, tokB); w.Code != 401 {
		t.Fatalf("access закрытой сессии: %d", w.Code)
	}
	if w := e.do("POST", "/api/auth/refresh", nil, "", ckB); w.Code != 401 {
		t.Fatalf("refresh закрытой сессии: %d", w.Code)
	}
	if w := e.do("GET", "/api/me", nil, tokA); w.Code != 200 {
		t.Fatalf("своя сессия пострадала: %d", w.Code)
	}

	// Обновление токена не создаёт новую сессию.
	_, ckC := e.loginAs("john", "password123", "Tablet")
	before := len(decode[sessionsBody](t, e.do("GET", "/api/me/sessions", nil, tokA)).Sessions)
	if w := e.do("POST", "/api/auth/refresh", nil, "", ckC); w.Code != 200 {
		t.Fatalf("refresh: %d", w.Code)
	}
	if after := len(decode[sessionsBody](t, e.do("GET", "/api/me/sessions", nil, tokA)).Sessions); after != before {
		t.Fatalf("refresh создал сессию: %d → %d", before, after)
	}

	// «Закрыть все остальные»: остаётся одна текущая.
	w := e.do("POST", "/api/me/sessions/revoke-others", nil, tokA)
	if w.Code != 200 {
		t.Fatalf("закрыть остальные: %d", w.Code)
	}
	if n := decode[struct{ Revoked int }](t, w).Revoked; n != 2 {
		t.Fatalf("закрыто %d, нужно 2 (регистрация и планшет)", n)
	}
	list = decode[sessionsBody](t, e.do("GET", "/api/me/sessions", nil, tokA))
	if len(list.Sessions) != 1 || !list.Sessions[0].Current {
		t.Fatalf("после закрытия: %+v", list)
	}
	if w := e.do("POST", "/api/auth/refresh", nil, "", ckC); w.Code != 401 {
		t.Fatalf("refresh закрытой: %d", w.Code)
	}

	// Выход закрывает сессию целиком.
	tokD, ckD := e.loginAs("john", "password123", "D")
	if w := e.do("POST", "/api/auth/logout", nil, "", ckD); w.Code != 204 {
		t.Fatalf("выход: %d", w.Code)
	}
	if w := e.do("GET", "/api/me", nil, tokD); w.Code != 401 {
		t.Fatalf("access после выхода: %d", w.Code)
	}
}

type setupBody struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

func code(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	c, err := auth.TOTPCode(secret, at)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTwoFactorLifecycle(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	_, ckOther := e.loginAs("john", "password123", "Other")

	// Настройка требует пароль.
	if w := e.do("POST", "/api/me/2fa/setup", map[string]string{"password": "wrong"}, tok); w.Code != 422 {
		t.Fatalf("неверный пароль: %d", w.Code)
	}
	w := e.do("POST", "/api/me/2fa/setup", map[string]string{"password": "password123"}, tok)
	if w.Code != 200 {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	setup := decode[setupBody](t, w)
	if setup.Secret == "" || len(setup.URI) < 20 || setup.URI[:15] != "otpauth://totp/" {
		t.Fatalf("setup: %+v", setup)
	}
	// Пока код не подтверждён, вход обычный.
	if c, _, _ := e.login("john", "password123"); c != 200 {
		t.Fatal("до включения вход по паролю")
	}

	// Неверный код не включает.
	if w := e.do("POST", "/api/me/2fa/enable", map[string]string{"code": "000000"}, tok); w.Code != 422 {
		t.Fatalf("неверный код: %d", w.Code)
	}
	now := time.Now()
	w = e.do("POST", "/api/me/2fa/enable", map[string]string{"code": code(t, setup.Secret, now)}, tok)
	if w.Code != 200 {
		t.Fatalf("enable: %d %s", w.Code, w.Body)
	}
	codes := decode[struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}](t, w).RecoveryCodes
	if len(codes) != 10 {
		t.Fatalf("кодов восстановления: %d", len(codes))
	}
	st := decode[auth.TwoFactorStatus](t, e.do("GET", "/api/me/2fa", nil, tok))
	if !st.Enabled || st.RecoveryLeft != 10 {
		t.Fatalf("статус: %+v", st)
	}
	// Уже открытые сессии второй фактор не закрывает.
	if w := e.do("POST", "/api/auth/refresh", nil, "", ckOther); w.Code != 200 {
		t.Fatalf("прежняя сессия: %d", w.Code)
	}

	// Вход теперь в два шага: пароль даёт билет, не сессию.
	w = e.do("POST", "/api/auth/login", map[string]string{"login": "john", "password": "password123"}, "")
	if w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("первый шаг: %d %v", w.Code, w.Result().Cookies())
	}
	first := decode[struct {
		TwoFactor   bool   `json:"two_factor"`
		Ticket      string `json:"ticket"`
		AccessToken string `json:"access_token"`
	}](t, w)
	if !first.TwoFactor || first.Ticket == "" || first.AccessToken != "" {
		t.Fatalf("первый шаг: %+v", first)
	}
	// Неверный пароль по-прежнему просто ошибка.
	if c, _, _ := e.login("john", "nope-nope"); c != 401 {
		t.Fatalf("неверный пароль: %d", c)
	}
	// Код, уже использованный при включении, повторно не принимается.
	if w := e.do("POST", "/api/auth/login/2fa", map[string]string{"ticket": first.Ticket, "code": code(t, setup.Secret, now)}, ""); w.Code != 422 {
		t.Fatalf("повтор кода: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/auth/login/2fa", map[string]string{"ticket": "bogus", "code": "123456"}, ""); w.Code != 401 {
		t.Fatalf("чужой билет: %d", w.Code)
	}
	// Следующий шаг времени (окно ±30 с) — вход.
	w = e.do("POST", "/api/auth/login/2fa", map[string]string{"ticket": first.Ticket, "code": code(t, setup.Secret, now.Add(30*time.Second))}, "")
	if w.Code != 200 {
		t.Fatalf("второй шаг: %d %s", w.Code, w.Body)
	}
	refreshCookie(t, w)
	// Билет одноразовый.
	if w := e.do("POST", "/api/auth/login/2fa", map[string]string{"ticket": first.Ticket, "code": codes[0]}, ""); w.Code != 401 {
		t.Fatalf("билет второй раз: %d", w.Code)
	}

	// Вход кодом восстановления; тот же код второй раз не подходит.
	ticket := func() string {
		w := e.do("POST", "/api/auth/login", map[string]string{"login": "john", "password": "password123"}, "")
		return decode[struct{ Ticket string }](t, w).Ticket
	}
	if w := e.do("POST", "/api/auth/login/2fa", map[string]string{"ticket": ticket(), "code": " " + codes[0] + " "}, ""); w.Code != 200 {
		t.Fatalf("код восстановления: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/auth/login/2fa", map[string]string{"ticket": ticket(), "code": codes[0]}, ""); w.Code != 422 {
		t.Fatalf("код восстановления повторно: %d", w.Code)
	}
	if st := decode[auth.TwoFactorStatus](t, e.do("GET", "/api/me/2fa", nil, tok)); st.RecoveryLeft != 9 {
		t.Fatalf("осталось кодов: %d", st.RecoveryLeft)
	}

	// Билет выдерживает не больше 5 попыток.
	tk := ticket()
	for range 5 {
		e.do("POST", "/api/auth/login/2fa", map[string]string{"ticket": tk, "code": "000000"}, "")
	}
	if w := e.do("POST", "/api/auth/login/2fa", map[string]string{"ticket": tk, "code": codes[1]}, ""); w.Code != 401 {
		t.Fatalf("после 5 попыток: %d", w.Code)
	}

	// Новые коды восстановления заменяют старые.
	w = e.do("POST", "/api/me/2fa/recovery", map[string]string{"password": "password123"}, tok)
	if w.Code != 200 {
		t.Fatalf("новые коды: %d", w.Code)
	}
	fresh := decode[struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}](t, w).RecoveryCodes
	if w := e.do("POST", "/api/auth/login/2fa", map[string]string{"ticket": ticket(), "code": codes[2]}, ""); w.Code != 422 {
		t.Fatalf("старый код после замены: %d", w.Code)
	}

	// Выключение: нужен и пароль, и код.
	if w := e.do("POST", "/api/me/2fa/disable", map[string]string{"password": "wrong", "code": fresh[0]}, tok); w.Code != 422 {
		t.Fatalf("выключение с неверным паролем: %d", w.Code)
	}
	if w := e.do("POST", "/api/me/2fa/disable", map[string]string{"password": "password123", "code": "000000"}, tok); w.Code != 422 {
		t.Fatalf("выключение с неверным кодом: %d", w.Code)
	}
	if w := e.do("POST", "/api/me/2fa/disable", map[string]string{"password": "password123", "code": fresh[0]}, tok); w.Code != 204 {
		t.Fatalf("выключение: %d %s", w.Code, w.Body)
	}
	if c, _, _ := e.login("john", "password123"); c != 200 {
		t.Fatal("после выключения вход по паролю")
	}
	if st := decode[auth.TwoFactorStatus](t, e.do("GET", "/api/me/2fa", nil, tok)); st.Enabled {
		t.Fatal("всё ещё включён")
	}
}

func TestAdminResetTwoFactor(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	setup := decode[setupBody](t, e.do("POST", "/api/me/2fa/setup", map[string]string{"password": "password123"}, tok))
	if w := e.do("POST", "/api/me/2fa/enable", map[string]string{"code": code(t, setup.Secret, time.Now())}, tok); w.Code != 200 {
		t.Fatalf("enable: %d", w.Code)
	}
	if _, err := e.svc.ResetTwoFactor(context.Background(), "john@example.com"); err != nil {
		t.Fatal(err)
	}
	if w := e.do("GET", "/api/me", nil, tok); w.Code != 401 {
		t.Fatalf("сессии после сброса должны закрыться: %d", w.Code)
	}
	if c, _, _ := e.login("john", "password123"); c != 200 {
		t.Fatal("после сброса вход по паролю")
	}
}

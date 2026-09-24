package httpapi_test

import (
	"net/http"
	"testing"
)

func (e *env) changePassword(tok string, cookies []*http.Cookie, cur, next string) resp2 {
	e.t.Helper()
	w := e.do("POST", "/api/me/password", map[string]string{"current_password": cur, "new_password": next}, tok, cookies...)
	return resp2{w.Code, w.Body.String(), w.Result().Cookies(), w.Body.Bytes()}
}

type resp2 struct {
	Code    int
	Body    string
	Cookies []*http.Cookie
	raw     []byte
}

func (e *env) login(login, pw string) (int, string, *http.Cookie) {
	e.t.Helper()
	w := e.do("POST", "/api/auth/login", map[string]string{"login": login, "password": pw}, "")
	var ck *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "vh_refresh" {
			ck = c
		}
	}
	tok := ""
	if w.Code == 200 {
		tok = decode[sessionBody](e.t, w).AccessToken
	}
	return w.Code, tok, ck
}

func TestChangePassword(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin() // пароль admin: password123
	e.user(adm, "john")

	// Две сессии одного пользователя (телефон и ноутбук).
	_, tokA, ckA := e.login("john", "password123")
	_, _, ckB := e.login("john", "password123")

	cases := []struct {
		name, cur, next, field, code string
	}{
		{"неверный текущий", "wrong-current", "brand-new-pass1", "current_password", "wrong_password"},
		{"новый совпадает с текущим", "password123", "password123", "new_password", "password_same"},
		{"слишком короткий", "password123", "short", "new_password", "validation.password_short"},
		{"слишком длинный", "password123", repeat("a", 80), "new_password", "validation.password_long"},
	}
	for _, tc := range cases {
		w := e.do("POST", "/api/me/password", map[string]string{"current_password": tc.cur, "new_password": tc.next}, tokA)
		if w.Code != 422 {
			t.Fatalf("%s: %d %s", tc.name, w.Code, w.Body)
		}
		got := decode[errBody](t, w).Error
		if got.Field != tc.field || got.Code != tc.code {
			t.Fatalf("%s: %+v", tc.name, got)
		}
	}
	// Неудачные попытки ничего не меняют: старый пароль работает.
	if c, _, _ := e.login("john", "password123"); c != 200 {
		t.Fatal("старый пароль должен работать после неудачных попыток смены")
	}

	// Пустые поля и аноним.
	if w := e.do("POST", "/api/me/password", map[string]string{"current_password": "", "new_password": ""}, tokA); w.Code != 400 {
		t.Fatalf("пустые поля: %d", w.Code)
	}
	if w := e.do("POST", "/api/me/password", map[string]string{"current_password": "a", "new_password": "b"}, ""); w.Code != 401 {
		t.Fatalf("аноним: %d", w.Code)
	}

	// Успешная смена.
	r := e.changePassword(tokA, []*http.Cookie{ckA}, "password123", "brand-new-pass1")
	if r.Code != 200 {
		t.Fatalf("смена: %d %s", r.Code, r.Body)
	}
	var newCk *http.Cookie
	for _, c := range r.Cookies {
		if c.Name == "vh_refresh" {
			newCk = c
		}
	}
	if newCk == nil || newCk.Value == ckA.Value {
		t.Fatal("текущему устройству должна выдаваться новая сессия")
	}
	if c, _, _ := e.login("john", "password123"); c != 401 {
		t.Fatalf("старый пароль после смены: %d", c)
	}
	if c, _, _ := e.login("john", "brand-new-pass1"); c != 200 {
		t.Fatalf("новый пароль: %d", c)
	}
	// Новая сессия работает. Проверяем её первой: предъявление закрытого токена по замыслу считается кражей
	// и закрывает все сессии пользователя, включая новую.
	if w := e.do("POST", "/api/auth/refresh", nil, "", newCk); w.Code != 200 {
		t.Fatalf("новая сессия должна работать: %d", w.Code)
	}
	// Остальные сессии закрыты (в том числе та, с которой менялся пароль, — её заменила новая).
	if w := e.do("POST", "/api/auth/refresh", nil, "", ckB); w.Code != 401 {
		t.Fatalf("сессия другого устройства должна быть закрыта: %d", w.Code)
	}
	if w := e.do("POST", "/api/auth/refresh", nil, "", ckA); w.Code != 401 {
		t.Fatalf("старая сессия этого устройства заменена новой: %d", w.Code)
	}
	// Пароль в ответе не возвращается.
	if containsAny(r.Body, "brand-new-pass1", "password_hash", "$2a$") {
		t.Fatalf("утечка в ответе: %s", r.Body)
	}
}

func TestChangePasswordLocalized(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	w := e.doLang("it", "POST", "/api/me/password", map[string]string{"current_password": "nope-nope", "new_password": "another-pass1"}, adm)
	if got := decode[msgBody](t, w).Error; w.Code != 422 || got.Message != "La password attuale non è corretta" {
		t.Fatalf("%d %+v", w.Code, got)
	}
	w = e.doLang("ru", "POST", "/api/me/password", map[string]string{"current_password": "password123", "new_password": "password123"}, adm)
	if got := decode[msgBody](t, w).Error; got.Message != "Новый пароль совпадает с текущим" {
		t.Fatalf("%+v", got)
	}
}

func TestChangePasswordRateLimited(t *testing.T) {
	e := newEnvLimit(t, 20, 10)
	adm, _ := e.admin()
	got429 := false
	for range 30 {
		if w := e.do("POST", "/api/me/password", map[string]string{"current_password": "guess-guess", "new_password": "another-pass1"}, adm); w.Code == 429 {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("подбор текущего пароля через смену пароля должен упираться в лимит")
	}
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		for i := 0; i+len(x) <= len(s); i++ {
			if s[i:i+len(x)] == x {
				return true
			}
		}
	}
	return false
}

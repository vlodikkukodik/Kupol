package httpapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"kupol/internal/accounts"
	"kupol/internal/config"
)

var backupCodeRe = regexp.MustCompile(`^KUPOL(-[A-HJ-NP-Z2-9]{4}){4}$`)

func want(t *testing.T, r response, code int, errCode string) {
	t.Helper()
	if r.Code != code {
		t.Fatalf("статус %d, ожидался %d: %s", r.Code, code, r.Body)
	}
	if errCode != "" && r.errCode() != errCode {
		t.Fatalf("код ошибки %q, ожидался %q: %s", r.errCode(), errCode, r.Body)
	}
}

func TestGuestSessionIsNullAndNeverSetsCookies(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t)
	r := c.do("GET", "/api/auth/session", nil)
	want(t, r, 200, "")
	if m := r.json(); m["user"] != nil {
		t.Fatalf("user = %v, ожидался null", m["user"])
	}
	if len(r.Result().Cookies()) != 0 {
		t.Error("гостю нельзя ставить куки")
	}
}

func TestRegisterFlow(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t)
	r := c.register("Куратор7", "секретный пароль")
	want(t, r, 201, "")

	m := r.json()
	if code, _ := m["backup_code"].(string); !backupCodeRe.MatchString(code) {
		t.Errorf("backup_code: %v", m["backup_code"])
	}
	u := m["user"].(map[string]any)
	if u["login"] != "Куратор7" || u["level"] != float64(1) || u["level_name"] != "Посетитель" || u["directorate"] != false {
		t.Errorf("user: %v", u)
	}
	if _, err := time.Parse(time.RFC3339, u["created_at"].(string)); err != nil {
		t.Errorf("created_at: %v", u["created_at"])
	}
	// в ответах нет хешей и секретов
	for _, leak := range []string{"hash", "password", "token"} {
		if strings.Contains(strings.ToLower(r.Body.String()), leak) {
			t.Errorf("в ответе регистрации есть %q: %s", leak, r.Body)
		}
	}

	// кука сессии: HttpOnly, SameSite=Lax, Path=/, ~30 дней; Secure — только в prod (в dev сайт на http)
	ck := r.sessionSetCookie()
	if ck == nil {
		t.Fatal("кука сессии не выставлена")
	}
	if !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/" || ck.Secure || ck.Domain != "" {
		t.Errorf("атрибуты куки: %+v", ck)
	}
	if ck.MaxAge < int((30*24*time.Hour).Seconds())-5 || ck.MaxAge > int((30*24*time.Hour).Seconds()) {
		t.Errorf("Max-Age = %d, ожидалось ~30 дней", ck.MaxAge)
	}
	if len(ck.Value) != 43 {
		t.Errorf("токен %q: ожидалось 43 символа base64url", ck.Value)
	}

	// вход по этой же куке
	s := c.do("GET", "/api/auth/session", nil)
	want(t, s, 200, "")
	su, _ := s.json()["user"].(map[string]any)
	if su["login"] != "Куратор7" {
		t.Errorf("session user: %v", su)
	}
}

func TestSessionCookieIsSecureInProd(t *testing.T) {
	st := newStack(t, func(c *config.Config) { c.Env = "prod" })
	c := st.newClient(t)
	r := c.register("prodguy", "password-1")
	want(t, r, 201, "")
	if ck := r.sessionSetCookie(); ck == nil || !ck.Secure || !ck.HttpOnly {
		t.Errorf("в prod кука должна быть Secure и HttpOnly: %+v", ck)
	}
}

func TestRegisterErrors(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t)

	// поля формы
	cp := c.do("GET", "/api/auth/captcha", nil).json()
	r := c.do("POST", "/api/auth/register", map[string]any{"login": "a", "password": "x", "captcha_id": cp["id"], "captcha_answer": ""})
	want(t, r, 422, "validation")
	for _, f := range []string{"login", "password", "captcha_answer"} {
		if r.fields()[f] == nil {
			t.Errorf("нет ошибки поля %s: %s", f, r.Body)
		}
	}

	// неверный ответ анкеты
	r = c.do("POST", "/api/auth/register", map[string]any{"login": "validlogin", "password": "password-1", "captcha_id": cp["id"], "captcha_answer": "не знаю"})
	want(t, r, 422, "captcha_failed")
	if r.fields()["captcha_answer"] == nil {
		t.Error("нет подсказки в поле captcha_answer")
	}
	if c.cookie != nil {
		t.Error("после ошибки регистрации куки быть не должно")
	}

	// логин занят (без учёта регистра)
	if r := c.register("Vladislav", "password-1"); r.Code != 201 {
		t.Fatal(r.Body)
	}
	c2 := st.newClient(t)
	r = c2.register("vladislav", "password-2")
	want(t, r, 409, "login_taken")
	if r.fields()["login"] == nil {
		t.Error("нет ошибки поля login")
	}
}

func TestLoginAndLogout(t *testing.T) {
	st := newStack(t, nil)
	reg := st.newClient(t)
	reg.register("returning", "правильный пароль")

	c := st.newClient(t)
	r := c.do("POST", "/api/auth/login", map[string]any{"login": "RETURNING", "password": "правильный пароль"})
	want(t, r, 200, "")
	if r.sessionSetCookie() == nil || c.cookie == nil {
		t.Fatal("кука не выставлена")
	}
	if u := c.do("GET", "/api/auth/session", nil).json()["user"]; u == nil {
		t.Fatal("после входа сессия должна быть")
	}
	oldCookie := c.cookie

	r = c.do("POST", "/api/auth/logout", nil)
	want(t, r, 204, "")
	if ck := r.sessionSetCookie(); ck == nil || ck.MaxAge >= 0 || ck.Value != "" {
		t.Errorf("кука после выхода должна стираться: %+v", ck)
	}
	if u := c.do("GET", "/api/auth/session", nil).json()["user"]; u != nil {
		t.Fatalf("после выхода user = %v", u)
	}

	// украденная кука после выхода бесполезна: сессия удалена на сервере
	c.cookie = oldCookie
	if u := c.do("GET", "/api/auth/session", nil).json()["user"]; u != nil {
		t.Fatal("сессия после выхода жива на сервере")
	}
	if c.cookie != nil {
		t.Error("недействительная кука должна стираться")
	}

	// выход без сессии — тоже успешный
	want(t, c.do("POST", "/api/auth/logout", nil), 204, "")
}

func TestLoginErrors(t *testing.T) {
	st := newStack(t, nil)
	st.newClient(t).register("existing", "правильный пароль")
	c := st.newClient(t)

	wrongPass := c.do("POST", "/api/auth/login", map[string]any{"login": "existing", "password": "неверно"})
	noUser := c.do("POST", "/api/auth/login", map[string]any{"login": "no-such", "password": "неверно"})
	want(t, wrongPass, 401, "invalid_credentials")
	want(t, noUser, 401, "invalid_credentials")
	if strings.Contains(wrongPass.Body.String(), "existing") {
		t.Error("ответ не должен эхом возвращать логин")
	}
	// ответы неразличимы (кроме идентификатора запроса)
	strip := regexp.MustCompile(`"request_id":"[^"]*"`)
	if strip.ReplaceAllString(wrongPass.Body.String(), "") != strip.ReplaceAllString(noUser.Body.String(), "") {
		t.Errorf("ответы различаются:\n%s\n%s", wrongPass.Body, noUser.Body)
	}

	r := c.do("POST", "/api/auth/login", map[string]any{"login": "", "password": ""})
	want(t, r, 422, "validation")
	if r.fields()["login"] == nil || r.fields()["password"] == nil {
		t.Errorf("поля: %s", r.Body)
	}
}

func TestLoginRateLimitReturns429WithRetryAfter(t *testing.T) {
	st := newStack(t, nil)
	st.newClient(t).register("target", "правильный пароль")
	c := st.newClient(t)
	for i := 0; i < 5; i++ {
		want(t, c.do("POST", "/api/auth/login", map[string]any{"login": "target", "password": "неверно"}), 401, "invalid_credentials")
	}
	r := c.do("POST", "/api/auth/login", map[string]any{"login": "target", "password": "правильный пароль"})
	want(t, r, 429, "rate_limited")
	secs, err := strconv.Atoi(r.Header().Get("Retry-After"))
	if err != nil || secs < 14*60 || secs > 15*60 {
		t.Errorf("Retry-After = %q", r.Header().Get("Retry-After"))
	}
	body := r.json()["error"].(map[string]any)
	if body["retry_after"] != float64(secs) {
		t.Errorf("retry_after в теле = %v, в заголовке %d", body["retry_after"], secs)
	}
	if c.cookie != nil {
		t.Error("при блокировке куки быть не должно")
	}
}

func TestRegisterRateLimitReturns429(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t) // один IP
	for i := 0; i < 3; i++ {
		want(t, c.register("reg"+strconv.Itoa(i), "password-1"), 201, "")
		c.cookie = nil
	}
	r := c.register("reg3", "password-1")
	want(t, r, 429, "rate_limited")
	if r.Header().Get("Retry-After") == "" {
		t.Error("нет Retry-After")
	}
}

func TestBrokenCookieIsIgnoredAndCleared(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t)
	for _, v := range []string{"мусор", strings.Repeat("A", 43), strings.Repeat("A", 500)} {
		c.cookie = &http.Cookie{Name: "kupol_session", Value: v}
		r := c.do("GET", "/api/auth/session", nil)
		want(t, r, 200, "")
		if r.json()["user"] != nil {
			t.Errorf("кука %q приняла за пользователя", v)
		}
		if ck := r.sessionSetCookie(); ck == nil || ck.MaxAge >= 0 {
			t.Errorf("кука %q должна стираться", v)
		}
	}
}

func TestSessionCookieRenewedOnActivity(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t)
	c.register("renewer", "password-1")
	first := c.cookie

	// без задержки кука не перевыставляется
	if r := c.do("GET", "/api/auth/session", nil); r.sessionSetCookie() != nil {
		t.Error("кука перевыставлена слишком рано")
	}
	// «прошло 10 минут»: сдвигаем last_seen_at в БД
	if err := st.db.Exec("UPDATE sessions SET last_seen_at = last_seen_at - interval '10 minutes'").Error; err != nil {
		t.Fatal(err)
	}
	r := c.do("GET", "/api/auth/session", nil)
	ck := r.sessionSetCookie()
	if ck == nil || ck.Value != first.Value {
		t.Fatalf("после 10 минут ждали перевыставленную куку с тем же токеном: %+v", ck)
	}
	if ck.MaxAge < int((30*24*time.Hour).Seconds())-5 {
		t.Errorf("Max-Age продлённой куки: %d", ck.MaxAge)
	}
}

// ---------------------------------------------------------------- CSRF

func TestCSRFOrigin(t *testing.T) {
	st := newStack(t, nil)
	st.newClient(t).register("victim", "password-1")

	post := func(c *client, origin string) response {
		c.origin = origin
		return c.do("POST", "/api/auth/login", map[string]any{"login": "victim", "password": "password-1"})
	}

	// чужой сайт (в том числе «почти наш») отклоняется, даже с верным паролем
	for _, bad := range []string{"https://evil.example", "http://kupol.test", "https://kupol.test.evil.example", "https://kupol.test:8443", "null", "https://KUPOL.test"} {
		c := st.newClient(t)
		r := post(c, bad)
		want(t, r, 403, "forbidden_origin")
		if c.cookie != nil {
			t.Errorf("Origin %q: кука выставлена несмотря на отказ", bad)
		}
	}
	// наш сайт — можно
	want(t, post(st.newClient(t), testOrigin), 200, "")
	// без Origin и без куки (curl, скрипты) — можно: у запроса нет «чужих» полномочий
	want(t, post(st.newClient(t), ""), 200, "")
}

func TestCSRFWithSessionCookieRequiresOrigin(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t)
	c.register("owner", "password-1")

	// подделка: запрос от лица вошедшего без Origin — отказ
	c.origin = ""
	r := c.do("POST", "/api/auth/logout", nil)
	want(t, r, 403, "forbidden_origin")
	c.origin = "https://evil.example"
	r = c.do("POST", "/api/me/password", map[string]any{"current_password": "password-1", "new_password": "new-password-2"})
	want(t, r, 403, "forbidden_origin")
	c.origin = "https://evil.example"
	r = c.do("DELETE", "/api/me", map[string]any{"password": "password-1"})
	want(t, r, 403, "forbidden_origin")

	// ничего не произошло
	c.origin = testOrigin
	if u := c.do("GET", "/api/auth/session", nil).json()["user"]; u == nil {
		t.Fatal("сессия не должна была пострадать")
	}
	// GET с чужим Origin не блокируется (безопасный метод), но и не меняет состояние
	c.origin = "https://evil.example"
	want(t, c.do("GET", "/api/auth/session", nil), 200, "")
}

// ---------------------------------------------------------------- разбор тела

func TestBindJSONIsStrict(t *testing.T) {
	st := newStack(t, nil)
	send := func(contentType, body string) response {
		req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
		req.RemoteAddr = "203.0.113.200:4000"
		req.Header.Set("Origin", testOrigin)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		st.r.ServeHTTP(w, req)
		return response{w}
	}
	ok := `{"login":"x","password":"y"}`

	want(t, send("text/plain", ok), 415, "unsupported_media_type")
	want(t, send("", ok), 415, "unsupported_media_type")
	want(t, send("application/x-www-form-urlencoded", "login=x&password=y"), 415, "unsupported_media_type")
	want(t, send("application/json", `{"login":"x","password":"y","admin":true}`), 400, "bad_request") // неизвестное поле
	want(t, send("application/json", ok+`{"login":"z"}`), 400, "bad_request")                          // мусор после значения
	want(t, send("application/json", ok+` garbage`), 400, "bad_request")
	want(t, send("application/json", `{"login":`), 400, "bad_request")
	want(t, send("application/json", ``), 400, "bad_request")
	want(t, send("application/json", `[1,2]`), 400, "bad_request")
	want(t, send("application/json", `{"login":123,"password":"y"}`), 400, "bad_request") // неверный тип
	want(t, send("application/json", `{"login":"`+strings.Repeat("я", 20000)+`","password":"y"}`), 413, "payload_too_large")

	// допустимые варианты Content-Type
	want(t, send("application/json; charset=utf-8", ok), 401, "invalid_credentials")
	want(t, send("Application/JSON", ok), 401, "invalid_credentials")
}

func TestUnsupportedMethodsOnAuthRoutes(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t)
	want(t, c.do("PUT", "/api/auth/login", map[string]any{}), 405, "method_not_allowed")
	want(t, c.do("GET", "/api/auth/login", nil), 405, "method_not_allowed")
	want(t, c.do("POST", "/api/auth/session", map[string]any{}), 405, "method_not_allowed")
}

// ---------------------------------------------------------------- вошедший пользователь

func TestMeRoutesRequireAuth(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t)
	want(t, c.do("POST", "/api/me/password", map[string]any{"current_password": "a", "new_password": "b"}), 401, "unauthenticated")
	want(t, c.do("DELETE", "/api/me", map[string]any{"password": "a"}), 401, "unauthenticated")
}

func TestChangePasswordOverHTTP(t *testing.T) {
	st := newStack(t, nil)
	laptop := st.newClient(t)
	laptop.register("changer", "старый пароль")
	phone := st.newClient(t)
	want(t, phone.do("POST", "/api/auth/login", map[string]any{"login": "changer", "password": "старый пароль"}), 200, "")

	r := laptop.do("POST", "/api/me/password", map[string]any{"current_password": "не тот", "new_password": "новый пароль 2"})
	want(t, r, 403, "wrong_password")
	r = laptop.do("POST", "/api/me/password", map[string]any{"current_password": "старый пароль", "new_password": "коротко"})
	want(t, r, 422, "validation")
	if r.fields()["new_password"] == nil {
		t.Errorf("поля: %s", r.Body)
	}

	want(t, laptop.do("POST", "/api/me/password", map[string]any{"current_password": "старый пароль", "new_password": "новый пароль 2"}), 204, "")
	if laptop.do("GET", "/api/auth/session", nil).json()["user"] == nil {
		t.Error("текущая сессия должна остаться")
	}
	if phone.do("GET", "/api/auth/session", nil).json()["user"] != nil {
		t.Error("другие сессии должны быть завершены")
	}
	c := st.newClient(t)
	want(t, c.do("POST", "/api/auth/login", map[string]any{"login": "changer", "password": "старый пароль"}), 401, "invalid_credentials")
	want(t, c.do("POST", "/api/auth/login", map[string]any{"login": "changer", "password": "новый пароль 2"}), 200, "")
}

func TestDeleteAccountOverHTTP(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t)
	c.register("leaver", "правильный пароль")

	want(t, c.do("DELETE", "/api/me", map[string]any{"password": "неверно"}), 403, "wrong_password")
	if c.do("GET", "/api/auth/session", nil).json()["user"] == nil {
		t.Fatal("неверный пароль не должен был удалить аккаунт")
	}
	r := c.do("DELETE", "/api/me", map[string]any{"password": "правильный пароль"})
	want(t, r, 204, "")
	if ck := r.sessionSetCookie(); ck == nil || ck.MaxAge >= 0 {
		t.Error("кука должна стираться")
	}
	if c.do("GET", "/api/auth/session", nil).json()["user"] != nil {
		t.Error("сессия удалённого аккаунта жива")
	}
	want(t, st.newClient(t).do("POST", "/api/auth/login", map[string]any{"login": "leaver", "password": "правильный пароль"}), 401, "invalid_credentials")

	var users, sessions int64
	st.db.Raw("SELECT count(*) FROM users").Scan(&users)
	st.db.Raw("SELECT count(*) FROM sessions").Scan(&sessions)
	if users != 0 || sessions != 0 {
		t.Errorf("осталось пользователей %d, сессий %d", users, sessions)
	}
}

func TestRestoreOverHTTP(t *testing.T) {
	st := newStack(t, nil)
	owner := st.newClient(t)
	code := owner.register("forgetful", "забытый пароль").json()["backup_code"].(string)

	c := st.newClient(t)
	r := c.do("POST", "/api/auth/restore", map[string]any{"login": "forgetful", "backup_code": "KUPOL-AAAA-BBBB-CCCC-DDDD", "new_password": "новый пароль 1"})
	want(t, r, 401, "invalid_credentials")
	if !strings.Contains(r.Body.String(), "Неверный логин или резервный код") {
		t.Errorf("сообщение: %s", r.Body)
	}

	r = c.do("POST", "/api/auth/restore", map[string]any{"login": "forgetful", "backup_code": strings.ToLower(code), "new_password": "слабый"})
	want(t, r, 422, "validation")

	r = c.do("POST", "/api/auth/restore", map[string]any{"login": "forgetful", "backup_code": code, "new_password": "новый пароль 1"})
	want(t, r, 200, "")
	newCode, _ := r.json()["backup_code"].(string)
	if !backupCodeRe.MatchString(newCode) || newCode == code {
		t.Errorf("новый резервный код: %q", newCode)
	}
	if c.do("GET", "/api/auth/session", nil).json()["user"] == nil {
		t.Error("после восстановления вход должен быть выполнен")
	}
	if owner.do("GET", "/api/auth/session", nil).json()["user"] != nil {
		t.Error("прежние сессии должны быть завершены")
	}
	// использованный код больше не работает
	r = st.newClient(t).do("POST", "/api/auth/restore", map[string]any{"login": "forgetful", "backup_code": code, "new_password": "ещё пароль 3"})
	want(t, r, 401, "invalid_credentials")
}

func TestDirectorateShownInSession(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t)
	c.register("boss", "password-1")
	if err := st.svc.SetLevel(t.Context(), "boss", accounts.LevelCouncil); err != nil {
		t.Fatal(err)
	}
	u := c.do("GET", "/api/auth/session", nil).json()["user"].(map[string]any)
	if u["level"] != float64(6) || u["level_name"] != "Особый Совет" {
		t.Errorf("Особый Совет: %v", u)
	}
	if err := st.svc.SetDirectorate(t.Context(), "boss", true); err != nil {
		t.Fatal(err)
	}
	u = c.do("GET", "/api/auth/session", nil).json()["user"].(map[string]any)
	if u["directorate"] != true || u["level_name"] != "Директорат" {
		t.Errorf("Директорат: %v", u)
	}
}

// ---------------------------------------------------------------- общий лимит API

func TestAPIRateLimitPerUserAndIP(t *testing.T) {
	st := newStack(t, func(c *config.Config) { c.Limits.APIPerMinute = 5 })

	guest := st.newClient(t)
	for i := 0; i < 5; i++ {
		want(t, guest.do("GET", "/api/auth/session", nil), 200, "")
	}
	r := guest.do("GET", "/api/auth/session", nil)
	want(t, r, 429, "rate_limited")
	if r.Header().Get("Retry-After") == "" {
		t.Error("нет Retry-After")
	}

	// проверка здоровья не ограничивается: её опрашивают мониторинг и сам сайт
	for i := 0; i < 20; i++ {
		want(t, guest.do("GET", "/api/health", nil), 200, "")
	}
	// другой IP не затронут
	want(t, st.newClient(t).do("GET", "/api/auth/session", nil), 200, "")
}

func TestAPIRateLimitIsPerUserNotPerIP(t *testing.T) {
	st := newStack(t, func(c *config.Config) { c.Limits.APIPerMinute = 6; c.Limits.RegisterPerHour = 100 })
	// два пользователя за одним NAT (один IP)
	a, b := st.newClient(t), st.newClient(t)
	b.ip = a.ip
	a.register("natuser1", "password-1") // 2 запроса: captcha + register
	b.register("natuser2", "password-1") // ещё 2 с того же IP, но у каждого теперь свой ключ пользователя

	for i := 0; i < 4; i++ { // у каждого осталось по 4 запроса своего лимита (регистрация вошла как гость: 2 расхода IP)
		want(t, a.do("GET", "/api/auth/session", nil), 200, "")
		want(t, b.do("GET", "/api/auth/session", nil), 200, "")
	}
	want(t, a.do("GET", "/api/auth/session", nil), 200, "") // лимит a: 5-й
	want(t, a.do("GET", "/api/auth/session", nil), 200, "") // 6-й
	want(t, a.do("GET", "/api/auth/session", nil), 429, "rate_limited")
	// b не пострадал от расхода a — лимит на пользователя, а не на общий IP
	want(t, b.do("GET", "/api/auth/session", nil), 200, "")
}

// ---------------------------------------------------------------- прокси и IP

func TestSessionRecordsIPFromSignedProxy(t *testing.T) {
	st := newStack(t, nil)
	cp := newClientForSigned(st)
	cap := cp.captcha(t)

	body := `{"login":"proxied","password":"password-1","captcha_id":"` + cap.id + `","captcha_answer":"` + captchaAnswer(t, cap.question) + `"}`
	req := signed("POST", "/api/auth/register", "198.51.100.77", time.Now())
	req.Body = io.NopCloser(strings.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testOrigin)
	w := httptest.NewRecorder()
	st.r.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}

	var ip string
	if err := st.db.Raw("SELECT host(ip) FROM sessions LIMIT 1").Scan(&ip).Error; err != nil {
		t.Fatal(err)
	}
	if ip != "198.51.100.77" {
		t.Errorf("в сессии записан IP %q, ожидался подписанный 198.51.100.77 (а не адрес хостинга 198.51.100.99)", ip)
	}
}

type captchaResp struct{ id, question string }

type signedClient struct{ st *stack }

func newClientForSigned(st *stack) *signedClient { return &signedClient{st: st} }

func (s *signedClient) captcha(t *testing.T) captchaResp {
	t.Helper()
	req := signed("GET", "/api/auth/captcha", "198.51.100.77", time.Now())
	w := httptest.NewRecorder()
	s.st.r.ServeHTTP(w, req)
	m := response{w}.json()
	return captchaResp{id: m["id"].(string), question: m["question"].(string)}
}

func TestCaptchaEndpoint(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t)
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		r := c.do("GET", "/api/auth/captcha", nil)
		want(t, r, 200, "")
		m := r.json()
		id, _ := m["id"].(string)
		q, _ := m["question"].(string)
		if !regexp.MustCompile(`^[0-9a-f-]{36}$`).MatchString(id) || q == "" {
			t.Fatalf("ответ: %s", r.Body)
		}
		if strings.Contains(r.Body.String(), "answer") {
			t.Fatal("ответ анкеты не должен утекать клиенту")
		}
		seen[q] = true
	}
	if len(seen) < 2 {
		t.Errorf("за 40 запросов только %d разных вопросов", len(seen))
	}
}

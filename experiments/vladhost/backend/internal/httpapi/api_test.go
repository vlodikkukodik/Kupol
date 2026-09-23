package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"vladhost/internal/auth"
	"vladhost/internal/config"
	"vladhost/internal/httpapi"
	"vladhost/internal/sites"
	"vladhost/internal/testdb"
)

type env struct {
	t    *testing.T
	r    *gin.Engine
	svc  *auth.Service
	root  string // корень файлов сайтов
	certs string // каталог обмена с выпускателем сертификатов
	sites *sites.Service
}

func newEnv(t *testing.T) *env {
	// В обычных тестах лимит не мешает; сам лимит проверяет TestAuthRateLimit.
	return newEnvLimit(t, 10000, 10000)
}

func newEnvLimit(t *testing.T, perMinute, burst int) *env {
	return newEnvQuota(t, 1<<20, perMinute, burst)
}

func newEnvQuota(t *testing.T, quota int64, perMinute, burst int) *env {
	t.Helper()
	db := testdb.Open(t)
	secret := []byte(strings.Repeat("s", 32))
	svc := auth.NewService(db, secret, 15*time.Minute, time.Hour)
	root := t.TempDir()
	certs := t.TempDir()
	siteSvc := sites.NewService(db, root, "vladinc.ru", certs, sites.Limits{MaxSites: 1, DiskQuotaBytes: quota})
	cfg := config.Config{JWTSecret: secret, CookieSecure: false, AuthPerMinute: perMinute, AuthBurst: burst}
	return &env{t: t, r: httpapi.New(svc, siteSvc, cfg), svc: svc, root: root, certs: certs, sites: siteSvc}
}

func (e *env) do(method, path string, body any, token string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("тело %q: %v", w.Body.String(), err)
	}
	return v
}

type sessionBody struct {
	AccessToken string    `json:"access_token"`
	User        auth.User `json:"user"`
}

type errBody struct {
	Error struct{ Code, Field string } `json:"error"`
}

func refreshCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == "vh_refresh" {
			return c
		}
	}
	t.Fatal("нет cookie vh_refresh")
	return nil
}

// admin заводит администратора и возвращает его access-токен.
func (e *env) admin() (string, int64) {
	e.t.Helper()
	u, err := e.svc.CreateAdmin(context.Background(), "root@example.com", "boss", "password123")
	if err != nil {
		e.t.Fatal(err)
	}
	w := e.do("POST", "/api/auth/login", map[string]string{"login": "boss", "password": "password123"}, "")
	if w.Code != 200 {
		e.t.Fatalf("вход админа: %d %s", w.Code, w.Body)
	}
	return decode[sessionBody](e.t, w).AccessToken, u.ID
}

func (e *env) invite(adminToken string) string {
	e.t.Helper()
	w := e.do("POST", "/api/invites", nil, adminToken)
	if w.Code != 201 {
		e.t.Fatalf("создание инвайта: %d %s", w.Code, w.Body)
	}
	return decode[struct{ Invite auth.Invite }](e.t, w).Invite.Code
}

func TestInviteRegisterLoginFlow(t *testing.T) {
	e := newEnv(t)
	adminTok, _ := e.admin()
	code := e.invite(adminTok)

	reg := map[string]string{"invite": code, "email": "John@Example.com", "username": "John", "password": "s3cretpass"}
	w := e.do("POST", "/api/auth/register", reg, "")
	if w.Code != 200 {
		t.Fatalf("регистрация: %d %s", w.Code, w.Body)
	}
	s := decode[sessionBody](t, w)
	if s.User.Email != "john@example.com" || s.User.Username != "john" || s.User.Role != auth.RoleUser {
		t.Fatalf("пользователь нормализован неверно: %+v", s.User)
	}
	if strings.Contains(w.Body.String(), "password") {
		t.Fatal("в ответе не должно быть данных о пароле")
	}
	ck := refreshCookie(t, w)
	if !ck.HttpOnly || ck.Path != "/api/auth" {
		t.Fatalf("refresh cookie должна быть httpOnly и ограничена /api/auth: %+v", ck)
	}

	// Инвайт одноразовый.
	reg["email"], reg["username"] = "other@example.com", "other"
	w = e.do("POST", "/api/auth/register", reg, "")
	if w.Code != 422 || decode[errBody](t, w).Error.Code != "invalid_invite" {
		t.Fatalf("повторный инвайт: %d %s", w.Code, w.Body)
	}

	// Вход по имени и по email.
	for _, login := range []string{"john", "JOHN@example.com"} {
		if w := e.do("POST", "/api/auth/login", map[string]string{"login": login, "password": "s3cretpass"}, ""); w.Code != 200 {
			t.Fatalf("вход %q: %d %s", login, w.Code, w.Body)
		}
	}
	if w := e.do("POST", "/api/auth/login", map[string]string{"login": "john", "password": "wrongpass1"}, ""); w.Code != 401 {
		t.Fatalf("неверный пароль: %d", w.Code)
	}
	if w := e.do("POST", "/api/auth/login", map[string]string{"login": "ghost", "password": "wrongpass1"}, ""); w.Code != 401 {
		t.Fatalf("неизвестный логин: %d", w.Code)
	}

	// /me с токеном и без.
	if w := e.do("GET", "/api/me", nil, s.AccessToken); w.Code != 200 {
		t.Fatalf("/me: %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/api/me", nil, ""); w.Code != 401 {
		t.Fatalf("/me без токена: %d", w.Code)
	}
}

func TestRegisterValidationAndConflicts(t *testing.T) {
	e := newEnv(t)
	adminTok, _ := e.admin()

	cases := []struct {
		name, field string
		mutate      func(m map[string]string)
		status      int
	}{
		{"плохой email", "email", func(m map[string]string) { m["email"] = "not-an-email" }, 422},
		{"короткое имя", "username", func(m map[string]string) { m["username"] = "ab" }, 422},
		{"дефис в начале", "username", func(m map[string]string) { m["username"] = "-abc" }, 422},
		{"точка в имени", "username", func(m map[string]string) { m["username"] = "a.b" }, 422},
		{"двойной дефис", "username", func(m map[string]string) { m["username"] = "a--b" }, 422},
		{"зарезервированное", "username", func(m map[string]string) { m["username"] = "app" }, 422},
		{"короткий пароль", "password", func(m map[string]string) { m["password"] = "short" }, 422},
		{"имя занято админом", "username", func(m map[string]string) { m["username"] = "boss" }, 409},
		{"email занят админом", "email", func(m map[string]string) { m["email"] = "ROOT@example.com" }, 409},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]string{"invite": e.invite(adminTok), "email": "u@example.com", "username": "newuser", "password": "password123"}
			tc.mutate(body)
			w := e.do("POST", "/api/auth/register", body, "")
			if w.Code != tc.status || decode[errBody](t, w).Error.Field != tc.field {
				t.Fatalf("ожидали %d/%s, получили %d %s", tc.status, tc.field, w.Code, w.Body)
			}
		})
	}

	// Неудачная регистрация не сжигает инвайт.
	code := e.invite(adminTok)
	bad := map[string]string{"invite": code, "email": "bad", "username": "okname", "password": "password123"}
	if w := e.do("POST", "/api/auth/register", bad, ""); w.Code != 422 {
		t.Fatalf("%d", w.Code)
	}
	bad["email"] = "ok@example.com"
	if w := e.do("POST", "/api/auth/register", bad, ""); w.Code != 200 {
		t.Fatalf("инвайт должен остаться годным: %d %s", w.Code, w.Body)
	}

	if w := e.do("POST", "/api/auth/register", map[string]string{"invite": "nope", "email": "x@example.com", "username": "xxx", "password": "password123"}, ""); w.Code != 422 {
		t.Fatalf("несуществующий инвайт: %d", w.Code)
	}
}

func TestRefreshRotationAndReuseDetection(t *testing.T) {
	e := newEnv(t)
	e.admin()
	w := e.do("POST", "/api/auth/login", map[string]string{"login": "boss", "password": "password123"}, "")
	first := refreshCookie(t, w)

	w = e.do("POST", "/api/auth/refresh", nil, "", first)
	if w.Code != 200 {
		t.Fatalf("refresh: %d %s", w.Code, w.Body)
	}
	second := refreshCookie(t, w)
	if second.Value == first.Value {
		t.Fatal("refresh-токен должен меняться при каждом обновлении")
	}

	// Старый токен: отказ и закрытие всех сессий пользователя.
	if w := e.do("POST", "/api/auth/refresh", nil, "", first); w.Code != 401 {
		t.Fatalf("повторное использование: %d", w.Code)
	}
	if w := e.do("POST", "/api/auth/refresh", nil, "", second); w.Code != 401 {
		t.Fatalf("после кражи новая сессия тоже должна быть закрыта: %d", w.Code)
	}
	if w := e.do("POST", "/api/auth/refresh", nil, ""); w.Code != 401 {
		t.Fatalf("без cookie: %d", w.Code)
	}
}

func TestLogoutRevokesRefresh(t *testing.T) {
	e := newEnv(t)
	e.admin()
	w := e.do("POST", "/api/auth/login", map[string]string{"login": "boss", "password": "password123"}, "")
	ck := refreshCookie(t, w)
	if w := e.do("POST", "/api/auth/logout", nil, "", ck); w.Code != 204 {
		t.Fatalf("logout: %d", w.Code)
	}
	if w := e.do("POST", "/api/auth/refresh", nil, "", ck); w.Code != 401 {
		t.Fatalf("после выхода refresh должен быть закрыт: %d", w.Code)
	}
}

func TestInvitesAdminOnly(t *testing.T) {
	e := newEnv(t)
	adminTok, _ := e.admin()
	code := e.invite(adminTok)
	w := e.do("POST", "/api/auth/register", map[string]string{"invite": code, "email": "u@example.com", "username": "plain", "password": "password123"}, "")
	userTok := decode[sessionBody](t, w).AccessToken

	if w := e.do("GET", "/api/invites", nil, userTok); w.Code != 403 {
		t.Fatalf("обычный пользователь: %d", w.Code)
	}
	if w := e.do("POST", "/api/invites", nil, userTok); w.Code != 403 {
		t.Fatalf("обычный пользователь создаёт инвайт: %d", w.Code)
	}
	if w := e.do("GET", "/api/invites", nil, ""); w.Code != 401 {
		t.Fatalf("аноним: %d", w.Code)
	}
	w = e.do("GET", "/api/invites", nil, adminTok)
	list := decode[struct{ Invites []auth.InviteView }](t, w).Invites
	if len(list) != 1 || list[0].UsedByUsername == nil || *list[0].UsedByUsername != "plain" {
		t.Fatalf("список инвайтов: %s", w.Body)
	}
	if w := e.do("POST", "/api/invites", map[string]int{"ttl_hours": 100000}, adminTok); w.Code != 422 {
		t.Fatalf("слишком большой срок: %d", w.Code)
	}
}

func TestExpiredAndTamperedTokensRejected(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.admin()
	if w := e.do("GET", "/api/me", nil, tok+"x"); w.Code != 401 {
		t.Fatalf("подделанный токен: %d", w.Code)
	}
	if w := e.do("GET", "/api/me", nil, "garbage"); w.Code != 401 {
		t.Fatalf("мусор вместо токена: %d", w.Code)
	}
}

func TestAuthRateLimit(t *testing.T) {
	e := newEnvLimit(t, 20, 10)
	got429 := false
	for range 30 {
		w := e.do("POST", "/api/auth/login", map[string]string{"login": "x", "password": "y"}, "")
		if w.Code == 429 {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("ожидали 429 после серии попыток входа")
	}
}

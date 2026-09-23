package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vladhost/internal/auth"
	"vladhost/internal/config"
	"vladhost/internal/httpapi"
	"vladhost/internal/sites"
	"vladhost/internal/testdb"
)

type certSite struct {
	Site struct {
		ID         int64  `json:"id"`
		CertStatus string `json:"cert_status"`
		CertError  string `json:"cert_error"`
	} `json:"site"`
}

func (e *env) certStatus(tok string, id int64) (status, errMsg string) {
	e.t.Helper()
	w := e.do("GET", "/api/sites", nil, tok)
	body := decode[struct {
		Sites []struct {
			ID         int64  `json:"id"`
			CertStatus string `json:"cert_status"`
			CertError  string `json:"cert_error"`
		} `json:"sites"`
	}](e.t, w)
	for _, s := range body.Sites {
		if s.ID == id {
			return s.CertStatus, s.CertError
		}
	}
	e.t.Fatalf("сайт %d не найден", id)
	return "", ""
}

func (e *env) waitCert(tok string, id int64, want string) string {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, msg := e.certStatus(tok, id)
		if st == want {
			return msg
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("cert_status = %q (%s), ожидали %q", st, msg, want)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func (e *env) writeStatus(host, text string) {
	e.t.Helper()
	dir := filepath.Join(e.certs, "status")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, host), []byte(text+"\n"), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

func TestCertLifecycle(t *testing.T) {
	e := newEnv(t)
	go e.sites.WatchCerts(t.Context(), 30*time.Millisecond)

	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")

	// Создание сайта ставит заявку и статус pending.
	id, host := e.createSite(john, "blog")
	if !exists(t, filepath.Join(e.certs, "queue", "issue-"+host)) {
		t.Fatal("заявка на выпуск не создана")
	}
	if st, _ := e.certStatus(john, id); st != "pending" {
		t.Fatalf("после создания: %q", st)
	}
	// Пока выпускатель молчит, статус не меняется.
	time.Sleep(150 * time.Millisecond)
	if st, _ := e.certStatus(john, id); st != "pending" {
		t.Fatalf("без ответа выпускателя: %q", st)
	}
	e.writeStatus(host, "ok")
	e.waitCert(john, id, "active")

	// Ошибка выпуска доходит до пользователя, повтор ставит новую заявку.
	id2, host2 := e.createSite(mary, "shop")
	e.writeStatus(host2, "error: rate limit exceeded")
	if msg := e.waitCert(mary, id2, "failed"); msg != "rate limit exceeded" {
		t.Fatalf("текст ошибки: %q", msg)
	}
	if err := os.Remove(filepath.Join(e.certs, "queue", "issue-"+host2)); err != nil {
		t.Fatal(err)
	}
	w := e.do("POST", "/api/sites/"+itoa(id2)+"/cert/retry", nil, mary)
	if w.Code != 200 || decode[certSite](t, w).Site.CertStatus != "pending" {
		t.Fatalf("повтор: %d %s", w.Code, w.Body)
	}
	if !exists(t, filepath.Join(e.certs, "queue", "issue-"+host2)) {
		t.Fatal("повтор не создал заявку")
	}
	// Старый файл статуса (ошибка прошлой попытки) не должен закрыть новую заявку.
	time.Sleep(200 * time.Millisecond)
	if st, _ := e.certStatus(mary, id2); st != "pending" {
		t.Fatalf("устаревший статус применился: %q", st)
	}
	e.writeStatus(host2, "ok")
	e.waitCert(mary, id2, "active")

	// Повторять можно только неудачный выпуск; чужой сайт недоступен.
	if w := e.do("POST", "/api/sites/"+itoa(id2)+"/cert/retry", nil, mary); w.Code != 409 {
		t.Fatalf("повтор для активного: %d", w.Code)
	}
	if w := e.do("POST", "/api/sites/"+itoa(id)+"/cert/retry", nil, mary); w.Code != 404 {
		t.Fatalf("повтор для чужого: %d", w.Code)
	}

	// Удаление сайта ставит заявку на удаление сертификата.
	if w := e.do("DELETE", "/api/sites/"+itoa(id), nil, john); w.Code != 204 {
		t.Fatalf("удаление: %d", w.Code)
	}
	if !exists(t, filepath.Join(e.certs, "queue", "delete-"+host)) {
		t.Fatal("заявка на удаление сертификата не создана")
	}
}

// secureEnv — окружение, как на бою: Secure-cookie и проверка Origin.
func secureEnv(t *testing.T) http.Handler {
	t.Helper()
	db := testdb.Open(t)
	secret := []byte(strings.Repeat("s", 32))
	svc := auth.NewService(db, secret, 15*time.Minute, time.Hour)
	if _, err := svc.CreateAdmin(context.Background(), "root@example.com", "boss", "password123"); err != nil {
		t.Fatal(err)
	}
	siteSvc := sites.NewService(db, t.TempDir(), "vladinc.ru", "", sites.Limits{MaxSites: 1, DiskQuotaBytes: 1 << 20})
	cfg := config.Config{
		JWTSecret: secret, CookieSecure: true, PanelOrigin: "https://app.vladinc.ru",
		AuthPerMinute: 10000, AuthBurst: 10000,
	}
	return httpapi.New(svc, siteSvc, cfg)
}

func post(h http.Handler, path, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestHostCookieAndOriginCheck(t *testing.T) {
	h := secureEnv(t)
	const panel = "https://app.vladinc.ru"

	w := post(h, "/api/auth/login", `{"login":"boss","password":"password123"}`, panel, nil)
	if w.Code != 200 {
		t.Fatalf("вход: %d %s", w.Code, w.Body)
	}
	var ck *http.Cookie
	for _, c := range w.Result().Cookies() {
		if strings.HasSuffix(c.Name, "vh_refresh") {
			ck = c
		}
	}
	if ck == nil {
		t.Fatal("нет refresh-cookie")
	}
	// Требования префикса __Host-: Secure, Path=/, без Domain — иначе браузер cookie отвергнет.
	if ck.Name != "__Host-vh_refresh" || !ck.Secure || !ck.HttpOnly || ck.Path != "/" || ck.Domain != "" {
		t.Fatalf("cookie не подходит под __Host-: %+v", ck)
	}

	// Запрос со страницы пользовательского сайта на соседнем поддомене отклоняется,
	// даже с настоящей cookie, и не тратит токен.
	evil := "https://blog.john.vladinc.ru"
	if w := post(h, "/api/auth/refresh", "", evil, ck); w.Code != 403 {
		t.Fatalf("refresh с чужим Origin: %d", w.Code)
	}
	if w := post(h, "/api/auth/logout", "", evil, ck); w.Code != 403 {
		t.Fatalf("logout с чужим Origin: %d", w.Code)
	}
	if w := post(h, "/api/auth/login", `{"login":"boss","password":"password123"}`, evil, nil); w.Code != 403 {
		t.Fatalf("login с чужим Origin: %d", w.Code)
	}
	// Токен остался годным: с Origin панели, и без Origin (не браузер) обновление работает.
	w = post(h, "/api/auth/refresh", "", panel, ck)
	if w.Code != 200 {
		t.Fatalf("refresh с Origin панели: %d %s", w.Code, w.Body)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "__Host-vh_refresh" {
			ck = c
		}
	}
	if w := post(h, "/api/auth/refresh", "", "", ck); w.Code != 200 {
		t.Fatalf("refresh без Origin: %d", w.Code)
	}
}

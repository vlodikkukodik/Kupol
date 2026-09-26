package httpapi_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// doLang — запрос с заголовком Accept-Language (так фронтенд передаёт выбранный в шапке язык).
func (e *env) doLang(lang, method, path string, body any, token string) *httptest.ResponseRecorder {
	e.t.Helper()
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

type msgBody struct {
	Error struct{ Code, Message, Field string } `json:"error"`
}

func TestErrorMessagesFollowAcceptLanguage(t *testing.T) {
	e := newEnv(t)
	login := map[string]string{"login": "ghost", "password": "wrongpass1"}

	cases := []struct{ header, want, contentLang string }{
		{"it", "Nome utente o password errati", "it"},
		{"it-IT,it;q=0.9,en;q=0.8", "Nome utente o password errati", "it"},
		{"ru", "Неверный логин или пароль", "ru"},
		{"", "Неверный логин или пароль", "ru"},               // без заголовка — русский
		{"en-US,en;q=0.9", "Неверный логин или пароль", "ru"}, // неподдерживаемый язык — русский
		{"de;q=1, it;q=0.5", "Nome utente o password errati", "it"},
	}
	for _, tc := range cases {
		w := e.doLang(tc.header, "POST", "/api/auth/login", login, "")
		got := decode[msgBody](t, w).Error
		if w.Code != 401 || got.Message != tc.want || got.Code != "invalid_credentials" {
			t.Errorf("Accept-Language %q: %d %+v, ожидали %q", tc.header, w.Code, got, tc.want)
		}
		if w.Header().Get("Content-Language") != tc.contentLang {
			t.Errorf("Accept-Language %q: Content-Language = %q, ожидали %q", tc.header, w.Header().Get("Content-Language"), tc.contentLang)
		}
		if !strings.Contains(w.Header().Get("Vary"), "Accept-Language") {
			t.Errorf("ответ должен нести Vary: Accept-Language (кэши не должны смешивать языки): %q", w.Header().Get("Vary"))
		}
	}
	// Код ошибки не зависит от языка: по нему работает фронтенд и другие клиенты.
	a := decode[msgBody](t, e.doLang("it", "POST", "/api/auth/login", login, "")).Error.Code
	b := decode[msgBody](t, e.doLang("ru", "POST", "/api/auth/login", login, "")).Error.Code
	if a != b {
		t.Fatalf("код ошибки зависит от языка: %q и %q", a, b)
	}
}

func TestLocalizedValidationAndArgs(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()

	// Ошибка поля: код и поле те же, текст на нужном языке.
	body := map[string]string{"invite": e.invite(adm), "email": "x@example.com", "username": "a--b", "password": "password123"}
	w := e.doLang("it", "POST", "/api/auth/register", body, "")
	got := decode[msgBody](t, w).Error
	if w.Code != 422 || got.Field != "username" || got.Code != "validation.username_dashes" || got.Message != "Due trattini consecutivi non sono ammessi" {
		t.Fatalf("it: %d %+v", w.Code, got)
	}
	w = e.doLang("ru", "POST", "/api/auth/register", body, "")
	if got := decode[msgBody](t, w).Error; got.Message != "Два дефиса подряд недопустимы" {
		t.Fatalf("ru: %+v", got)
	}

	// Сообщение с числом: значение подставляется в текст на обоих языках.
	w = e.doLang("it", "POST", "/api/invites", map[string]int{"ttl_hours": 100000}, adm)
	if got := decode[msgBody](t, w).Error; w.Code != 422 || got.Message != "Durata dell'invito: da 1 a 720 ore" {
		t.Fatalf("it ttl: %d %+v", w.Code, got)
	}
	w = e.doLang("ru", "POST", "/api/invites", map[string]int{"ttl_hours": 100000}, adm)
	if got := decode[msgBody](t, w).Error; got.Message != "Срок инвайта: от 1 до 720 часов" {
		t.Fatalf("ru ttl: %+v", got)
	}
}

func TestLocalizedArchiveErrorsCarryFileName(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, _ := e.createSite(tok, "blog")
	url := "/api/sites/" + itoa(id) + "/deploy"

	for lang, want := range map[string]string{
		"it": "Percorso non valido nell'archivio: ../evil.txt",
		"ru": "Недопустимый путь в архиве: ../evil.txt",
	} {
		z := makeZip(t, zipFile{name: "index.html", body: "x"}, zipFile{name: "../evil.txt", body: "x"})
		w := e.uploadLang(lang, url, tok, z)
		got := decode[msgBody](t, w).Error
		if w.Code != 422 || got.Code != "archive.bad_path" || got.Message != want {
			t.Errorf("%s: %d %+v, ожидали %q", lang, w.Code, got, want)
		}
	}
	// Ошибки без параметров тоже переведены.
	w := e.uploadLang("it", url, tok, makeZip(t, zipFile{name: "about.html", body: "x"}))
	if got := decode[msgBody](t, w).Error; got.Message != "Nella radice dell'archivio manca index.html o index.php" {
		t.Errorf("it no_index: %+v", got)
	}
}

func TestLocalizedFileAndSiteErrors(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, _ := e.createSite(tok, "blog")

	w := e.doLang("it", "POST", "/api/sites", map[string]string{"slug": "two"}, tok)
	if got := decode[msgBody](t, w).Error; w.Code != 403 || got.Message != "Limite di siti per account raggiunto" {
		t.Errorf("site_limit: %d %+v", w.Code, got)
	}
	w = e.doLang("it", "GET", "/api/sites/"+itoa(id)+"/file?path=nope.txt", nil, tok)
	if got := decode[msgBody](t, w).Error; w.Code != 404 || got.Message != "File o cartella non trovati" {
		t.Errorf("file_not_found: %d %+v", w.Code, got)
	}
	w = e.doLang("it", "GET", "/api/sites/"+itoa(id)+"/file?path=../x", nil, tok)
	if got := decode[msgBody](t, w).Error; w.Code != 422 || got.Message != "Percorso non valido" {
		t.Errorf("bad_path: %d %+v", w.Code, got)
	}
	w = e.doLang("it", "GET", "/api/me", nil, "")
	if got := decode[msgBody](t, w).Error; w.Code != 401 || got.Message != "Sessione non valida" {
		t.Errorf("unauthorized: %d %+v", w.Code, got)
	}
	w = e.doLang("it", "GET", "/api/invites", nil, tok)
	if got := decode[msgBody](t, w).Error; w.Code != 403 || got.Message != "Permessi insufficienti" {
		t.Errorf("forbidden: %d %+v", w.Code, got)
	}
}

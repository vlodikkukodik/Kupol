package httpapi_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type zipFile struct {
	name, body string
	symlink    bool
}

func makeZip(t *testing.T, files ...zipFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		if f.symlink {
			h.SetMode(os.ModeSymlink | 0o777)
		} else {
			h.SetMode(0o644)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(f.body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (e *env) upload(path, token string, data []byte) *httptest.ResponseRecorder {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "site.zip")
	_, _ = fw.Write(data)
	_ = mw.Close()
	req := httptest.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

// user регистрирует обычного пользователя и возвращает его токен.
func (e *env) user(adminTok, name string) string {
	e.t.Helper()
	w := e.do("POST", "/api/auth/register", map[string]string{
		"invite": e.invite(adminTok), "email": name + "@example.com", "username": name, "password": "password123",
	}, "")
	if w.Code != 200 {
		e.t.Fatalf("регистрация %s: %d %s", name, w.Code, w.Body)
	}
	return decode[sessionBody](e.t, w).AccessToken
}

type siteBody struct {
	Site struct {
		ID        int64  `json:"id"`
		Host      string `json:"host"`
		URL       string `json:"url"`
		Status    string `json:"status"`
		DiskBytes int64  `json:"disk_bytes"`
	} `json:"site"`
}

func (e *env) createSite(tok, slug string) (int64, string) {
	e.t.Helper()
	w := e.do("POST", "/api/sites", map[string]string{"slug": slug}, tok)
	if w.Code != 201 {
		e.t.Fatalf("создание сайта: %d %s", w.Code, w.Body)
	}
	b := decode[siteBody](e.t, w)
	return b.Site.ID, b.Site.Host
}

func (e *env) read(host, rel string) string {
	b, err := os.ReadFile(filepath.Join(e.root, host, "public", rel))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func TestSiteCreateAndDeploy(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")

	w := e.do("POST", "/api/sites", map[string]string{"slug": "Blog"}, tok)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	b := decode[siteBody](t, w)
	if b.Site.Host != "blog.john.vladinc.ru" || b.Site.URL != "https://blog.john.vladinc.ru" || b.Site.Status != "empty" {
		t.Fatalf("сайт: %+v", b.Site)
	}

	// Архив «папки»: одна корневая папка снимается.
	z := makeZip(t, zipFile{name: "dist/index.html", body: "<h1>hi</h1>"}, zipFile{name: "dist/css/a.css", body: "b{}"}, zipFile{name: "__MACOSX/x", body: "junk"})
	w = e.upload("/api/sites/"+itoa(b.Site.ID)+"/deploy", tok, z)
	if w.Code != 200 {
		t.Fatalf("деплой: %d %s", w.Code, w.Body)
	}
	got := decode[siteBody](t, w)
	if got.Site.Status != "live" || got.Site.DiskBytes != int64(len("<h1>hi</h1>")+len("b{}")) {
		t.Fatalf("после деплоя: %+v", got.Site)
	}
	if e.read(b.Site.Host, "index.html") != "<h1>hi</h1>" || e.read(b.Site.Host, "css/a.css") != "b{}" {
		t.Fatal("файлы не на месте")
	}

	// Повторный деплой целиком заменяет содержимое, мусора от старой версии не остаётся.
	z = makeZip(t, zipFile{name: "index.html", body: "v2"})
	if w = e.upload("/api/sites/"+itoa(b.Site.ID)+"/deploy", tok, z); w.Code != 200 {
		t.Fatalf("второй деплой: %d %s", w.Code, w.Body)
	}
	if e.read(b.Site.Host, "index.html") != "v2" {
		t.Fatal("index.html не обновился")
	}
	if _, err := os.Stat(filepath.Join(e.root, b.Site.Host, "public", "css")); !os.IsNotExist(err) {
		t.Fatal("старые файлы должны исчезнуть")
	}
	entries, _ := os.ReadDir(filepath.Join(e.root, b.Site.Host))
	if len(entries) != 1 {
		t.Fatalf("во временных каталогах остался мусор: %v", entries)
	}
}

func TestSiteLimitsAndOwnership(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")

	id, _ := e.createSite(john, "one")
	if w := e.do("POST", "/api/sites", map[string]string{"slug": "two"}, john); w.Code != 403 || decode[errBody](t, w).Error.Code != "site_limit" {
		t.Fatalf("лимит сайтов: %d %s", w.Code, w.Body)
	}
	// Одинаковое имя у разных людей допустимо: host включает имя пользователя.
	e.createSite(mary, "one")

	// Чужой сайт недоступен и не раскрывает своего существования.
	if w := e.upload("/api/sites/"+itoa(id)+"/deploy", mary, makeZip(t, zipFile{name: "index.html", body: "x"})); w.Code != 404 {
		t.Fatalf("деплой в чужой сайт: %d", w.Code)
	}
	if w := e.do("DELETE", "/api/sites/"+itoa(id), nil, mary); w.Code != 404 {
		t.Fatalf("удаление чужого сайта: %d", w.Code)
	}
	if w := e.do("GET", "/api/sites", nil, ""); w.Code != 401 {
		t.Fatalf("аноним: %d", w.Code)
	}

	for _, slug := range []string{"a", "-ab", "ab-", "a--b", "a.b", "UPPER_x"} {
		if w := e.do("POST", "/api/sites", map[string]string{"slug": slug}, mary); w.Code != 422 && w.Code != 403 {
			t.Fatalf("slug %q: %d", slug, w.Code)
		}
	}

	var list struct {
		Sites []siteBody `json:"sites"`
	}
	w := e.do("GET", "/api/sites", nil, john)
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Sites) != 1 {
		t.Fatalf("список: %s", w.Body)
	}

	// Удаление освобождает лимит и чистит диск.
	dir := filepath.Join(e.root, "one.john.vladinc.ru")
	if w := e.do("DELETE", "/api/sites/"+itoa(id), nil, john); w.Code != 204 {
		t.Fatalf("удаление: %d", w.Code)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("каталог сайта должен быть удалён")
	}
	e.createSite(john, "again")
}

func TestDeployRejectsHostileArchives(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, host := e.createSite(tok, "blog")
	url := "/api/sites/" + itoa(id) + "/deploy"

	good := makeZip(t, zipFile{name: "index.html", body: "keep"})
	if w := e.upload(url, tok, good); w.Code != 200 {
		t.Fatal(w.Body)
	}

	cases := map[string][]byte{
		"zip-slip":         makeZip(t, zipFile{name: "index.html", body: "x"}, zipFile{name: "../evil.txt", body: "x"}),
		"вложенный ..":     makeZip(t, zipFile{name: "index.html", body: "x"}, zipFile{name: "a/../../evil.txt", body: "x"}),
		"абсолютный путь":  makeZip(t, zipFile{name: "index.html", body: "x"}, zipFile{name: "/etc/evil", body: "x"}),
		"обратный слеш":    makeZip(t, zipFile{name: "index.html", body: "x"}, zipFile{name: `..\evil`, body: "x"}),
		"симлинк":          makeZip(t, zipFile{name: "index.html", body: "x"}, zipFile{name: "link", body: "/etc/passwd", symlink: true}),
		"нет index.html":   makeZip(t, zipFile{name: "about.html", body: "x"}),
		"дубликат файла":   makeZip(t, zipFile{name: "index.html", body: "x"}, zipFile{name: "index.html", body: "y"}),
		"не zip":           []byte("definitely not a zip"),
		"пустой архив":     makeZip(t),
		"index.html в dir": makeZip(t, zipFile{name: "a/index.html", body: "x"}, zipFile{name: "b/other", body: "x"}),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			w := e.upload(url, tok, data)
			if w.Code != 422 || decode[errBody](t, w).Error.Code != "bad_archive" {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			// Неудачный деплой не ломает работающий сайт и ничего не пишет за пределы каталога.
			if e.read(host, "index.html") != "keep" {
				t.Fatal("рабочая версия сайта пострадала")
			}
			if _, err := os.Stat(filepath.Join(e.root, "evil.txt")); err == nil {
				t.Fatal("файл записан за пределы каталога сайта")
			}
			if _, err := os.Stat(filepath.Join(e.root, host, "evil.txt")); err == nil {
				t.Fatal("файл записан за пределы public")
			}
		})
	}
}

func TestDeployQuota(t *testing.T) {
	e := newEnv(t) // квота 1 МиБ
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, host := e.createSite(tok, "blog")
	url := "/api/sites/" + itoa(id) + "/deploy"

	ok := makeZip(t, zipFile{name: "index.html", body: "keep"})
	if w := e.upload(url, tok, ok); w.Code != 200 {
		t.Fatal(w.Body)
	}
	// Хорошо сжимаемый «бомба»-файл: в архиве мало, распакованный — больше квоты.
	bomb := makeZip(t, zipFile{name: "index.html", body: "x"}, zipFile{name: "big.bin", body: strings.Repeat("A", 2<<20)})
	w := e.upload(url, tok, bomb)
	if w.Code != 413 || decode[errBody](t, w).Error.Code != "quota_exceeded" {
		t.Fatalf("zip-бомба: %d %s", w.Code, w.Body)
	}
	if e.read(host, "index.html") != "keep" {
		t.Fatal("рабочая версия сайта пострадала")
	}
	entries, _ := os.ReadDir(filepath.Join(e.root, host))
	if len(entries) != 1 {
		t.Fatalf("недораспакованные файлы не удалены: %v", entries)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

package httpapi_test

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entryBody struct {
	Entries []struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size"`
	} `json:"entries"`
}

func fileURL(id int64, kind, p string) string {
	return "/api/sites/" + itoa(id) + "/" + kind + "?path=" + url.QueryEscape(p)
}

func (e *env) put(id int64, tok, p, content string) int {
	return e.do("PUT", fileURL(id, "file", p), map[string]string{"content": content}, tok).Code
}

func TestFileManagerFlow(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, host := e.createSite(tok, "blog")

	if c := e.put(id, tok, "index.html", "<h1>hi</h1>"); c != 204 {
		t.Fatalf("создание файла: %d", c)
	}
	if c := e.put(id, tok, "css/deep/a.css", "b{}"); c != 204 {
		t.Fatalf("файл во вложенной новой папке: %d", c)
	}
	w := e.do("GET", fileURL(id, "file", "index.html"), nil, tok)
	if w.Code != 200 || decode[struct{ Content string }](t, w).Content != "<h1>hi</h1>" {
		t.Fatalf("чтение: %d %s", w.Code, w.Body)
	}
	// Перезапись.
	if c := e.put(id, tok, "index.html", "v2"); c != 204 || e.read(host, "index.html") != "v2" {
		t.Fatalf("перезапись: %d", c)
	}

	// Список: папки первыми, служебных временных файлов не видно.
	w = e.do("GET", fileURL(id, "files", ""), nil, tok)
	list := decode[entryBody](t, w)
	if len(list.Entries) != 2 || list.Entries[0].Name != "css" || !list.Entries[0].IsDir || list.Entries[1].Name != "index.html" {
		t.Fatalf("список: %s", w.Body)
	}

	// mkdir, rename, upload.
	if w := e.do("POST", "/api/sites/"+itoa(id)+"/files/mkdir", map[string]string{"path": "img"}, tok); w.Code != 204 {
		t.Fatalf("mkdir: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/sites/"+itoa(id)+"/files/mkdir", map[string]string{"path": "img"}, tok); w.Code != 409 {
		t.Fatalf("повторный mkdir: %d", w.Code)
	}
	if w := e.do("POST", "/api/sites/"+itoa(id)+"/files/rename", map[string]string{"from": "css", "to": "styles"}, tok); w.Code != 204 {
		t.Fatalf("rename: %d %s", w.Code, w.Body)
	}
	if e.read(host, "styles/deep/a.css") != "b{}" {
		t.Fatal("папка не переименована")
	}
	if w := e.do("POST", "/api/sites/"+itoa(id)+"/files/rename", map[string]string{"from": "styles", "to": "index.html"}, tok); w.Code != 409 {
		t.Fatalf("rename поверх существующего: %d", w.Code)
	}
	if w := e.do("POST", "/api/sites/"+itoa(id)+"/files/rename", map[string]string{"from": "styles", "to": "styles/inner"}, tok); w.Code != 422 {
		t.Fatalf("rename папки в саму себя: %d", w.Code)
	}
	if w := e.upload("/api/sites/"+itoa(id)+"/files/upload?path=img", tok, []byte("PNGDATA")); w.Code != 204 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	if e.read(host, "img/site.zip") != "PNGDATA" {
		t.Fatal("файл не загружен под своим именем")
	}

	// Занятое место пересчитывается после операций.
	w = e.do("GET", "/api/sites", nil, tok)
	sb := decode[struct {
		Sites []struct {
			DiskBytes int64 `json:"disk_bytes"`
		}
	}](t, w)
	if want := int64(len("v2") + len("b{}") + len("PNGDATA")); sb.Sites[0].DiskBytes != want {
		t.Fatalf("disk_bytes = %d, ожидали %d", sb.Sites[0].DiskBytes, want)
	}

	// Удаление файла и папки.
	if w := e.do("DELETE", fileURL(id, "files", "styles"), nil, tok); w.Code != 204 {
		t.Fatalf("удаление папки: %d", w.Code)
	}
	if w := e.do("DELETE", fileURL(id, "files", "styles"), nil, tok); w.Code != 404 {
		t.Fatalf("удаление несуществующего: %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(e.root, host, "public", "styles")); !os.IsNotExist(err) {
		t.Fatal("папка не удалена")
	}
	if w := e.do("DELETE", fileURL(id, "files", ""), nil, tok); w.Code != 422 {
		t.Fatalf("удаление корня сайта: %d", w.Code)
	}
}

func TestFilePathTraversalBlocked(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, host := e.createSite(tok, "blog")
	e.put(id, tok, "index.html", "ok")

	// Секрет рядом с public и снаружи корня сайтов.
	if err := os.WriteFile(filepath.Join(e.root, "secret.txt"), []byte("TOP-SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Симлинк наружу, подложенный в обход API: os.Root не должен его пропустить.
	pub := filepath.Join(e.root, host, "public")
	if err := os.Symlink(filepath.Join(e.root, "secret.txt"), filepath.Join(pub, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(e.root, filepath.Join(pub, "linkdir")); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"../secret.txt", "a/../../secret.txt", "/etc/passwd", `..\secret.txt`, "a\x00b", "x/../.."} {
		if w := e.do("GET", fileURL(id, "file", p), nil, tok); w.Code == 200 {
			t.Fatalf("чтение %q прошло: %s", p, w.Body)
		}
		if c := e.put(id, tok, p, "pwn"); c == 204 {
			t.Fatalf("запись %q прошла", p)
		}
	}
	for _, p := range []string{"link.txt", "linkdir/secret.txt"} {
		w := e.do("GET", fileURL(id, "file", p), nil, tok)
		if w.Code == 200 || strings.Contains(w.Body.String(), "TOP-SECRET") {
			t.Fatalf("симлинк %q вывел файл за пределы сайта: %d %s", p, w.Code, w.Body)
		}
	}
	if c := e.put(id, tok, "linkdir/pwn.txt", "pwn"); c == 204 {
		t.Fatal("запись через симлинк-папку прошла")
	}
	if _, err := os.Stat(filepath.Join(e.root, "pwn.txt")); err == nil {
		t.Fatal("файл записан за пределы сайта")
	}
	if b, _ := os.ReadFile(filepath.Join(e.root, "secret.txt")); string(b) != "TOP-SECRET" {
		t.Fatal("внешний файл изменён")
	}
}

func TestFileLimitsAndTypes(t *testing.T) {
	e := newEnv(t) // квота 1 МиБ
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, host := e.createSite(tok, "blog")
	e.put(id, tok, "keep.txt", "keep")

	// Квота диска: файл больше остатка не создаётся и не оставляет мусора.
	if w := e.upload("/api/sites/"+itoa(id)+"/files/upload?path=", tok, []byte(strings.Repeat("A", 2<<20))); w.Code != 413 {
		t.Fatalf("загрузка сверх квоты: %d %s", w.Code, w.Body)
	}
	if entries, _ := os.ReadDir(filepath.Join(e.root, host, "public")); len(entries) != 1 {
		t.Fatalf("после отказа остался мусор: %v", entries)
	}
	// Редактор тоже упирается в квоту, а не только в свой лимит.
	if w := e.do("PUT", fileURL(id, "file", "big.txt"), map[string]string{"content": strings.Repeat("a", 2<<20)}, tok); w.Code != 413 || decode[errBody](t, w).Error.Code != "quota_exceeded" {
		t.Fatalf("сохранение сверх квоты: %d %s", w.Code, w.Body)
	}

	// Бинарный файл нельзя открыть в редакторе.
	if w := e.upload("/api/sites/"+itoa(id)+"/files/upload?path=", tok, []byte{0x89, 'P', 'N', 'G', 0, 1, 2}); w.Code != 204 {
		t.Fatalf("upload бинарника: %d", w.Code)
	}
	if w := e.do("GET", fileURL(id, "file", "site.zip"), nil, tok); w.Code != 415 {
		t.Fatalf("чтение бинарника: %d %s", w.Code, w.Body)
	}
	// Папку читать как файл, а файл как папку — нельзя.
	e.do("POST", "/api/sites/"+itoa(id)+"/files/mkdir", map[string]string{"path": "d"}, tok)
	if w := e.do("GET", fileURL(id, "file", "d"), nil, tok); w.Code != 422 {
		t.Fatalf("папка как файл: %d", w.Code)
	}
	if w := e.do("GET", fileURL(id, "files", "keep.txt"), nil, tok); w.Code != 422 {
		t.Fatalf("файл как папка: %d", w.Code)
	}
	if c := e.put(id, tok, "d", "x"); c != 422 {
		t.Fatalf("запись поверх папки: %d", c)
	}
	if c := e.put(id, tok, "keep.txt/inner", "x"); c != 422 {
		t.Fatalf("запись внутрь файла: %d", c)
	}
}

func TestEditorSizeCap(t *testing.T) {
	e := newEnvQuota(t, 10<<20, 10000, 10000)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, host := e.createSite(tok, "blog")

	w := e.do("PUT", fileURL(id, "file", "big.txt"), map[string]string{"content": strings.Repeat("a", 2<<20+10)}, tok)
	if w.Code != 413 || decode[errBody](t, w).Error.Code != "too_large" {
		t.Fatalf("больше лимита редактора: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(e.root, host, "public", "big.txt")); err == nil {
		t.Fatal("файл не должен появиться")
	}
	// Тот же размер через загрузку разрешён — лимит только у редактора, — но открыть его в редакторе нельзя.
	if w := e.upload("/api/sites/"+itoa(id)+"/files/upload?path=", tok, []byte(strings.Repeat("a", 2<<20+10))); w.Code != 204 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", fileURL(id, "file", "site.zip"), nil, tok); w.Code != 413 {
		t.Fatalf("открытие большого файла: %d", w.Code)
	}
}

func TestFilesOwnership(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	id, _ := e.createSite(john, "blog")
	e.put(id, john, "index.html", "mine")

	if w := e.do("GET", fileURL(id, "file", "index.html"), nil, mary); w.Code != 404 {
		t.Fatalf("чтение чужого: %d", w.Code)
	}
	if c := e.put(id, mary, "index.html", "pwn"); c != 404 {
		t.Fatalf("запись в чужой: %d", c)
	}
	if w := e.do("GET", fileURL(id, "files", ""), nil, mary); w.Code != 404 {
		t.Fatalf("список чужого: %d", w.Code)
	}
	if w := e.do("GET", fileURL(id, "files", ""), nil, ""); w.Code != 401 {
		t.Fatalf("аноним: %d", w.Code)
	}
}

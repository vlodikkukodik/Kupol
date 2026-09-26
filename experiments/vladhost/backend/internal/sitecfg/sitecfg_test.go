package sitecfg

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCleanDir(t *testing.T) {
	good := map[string]string{
		"": "", " ": "", "/": "", "dist": "dist", "/dist/": "dist", " a/b ": "a/b", "a//b": "a/b", "./a": "a", "a/./b": "a/b",
		"мой сайт/тут": "мой сайт/тут", strings.Repeat("a/", 7) + "z": strings.Repeat("a/", 7) + "z",
	}
	for in, want := range good {
		if got, ok := CleanDir(in); !ok || got != want {
			t.Errorf("CleanDir(%q) = %q, %v; ожидали %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"..", "../x", "a/../b", "a/..", ".git", "a/.ssh", ".", "a/.hidden/b", `a\b`, "a\x00b", strings.Repeat("a/", 8) + "z", strings.Repeat("d", 201), strings.Repeat("d", 256)} {
		if got, ok := CleanDir(in); ok && in != "." {
			t.Errorf("CleanDir(%q) принят как %q", in, got)
		}
	}
	// «.» — текущая папка, то есть корень.
	if got, ok := CleanDir("."); ok && got != "" {
		t.Errorf("CleanDir(.) = %q", got)
	}
}

func TestCleanFile(t *testing.T) {
	for in, want := range map[string]string{"404.html": "404.html", "/errors/404.html": "errors/404.html", "a/./b.html": "a/b.html"} {
		if got, ok := CleanFile(in); !ok || got != want {
			t.Errorf("CleanFile(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "/", "dir/", "..", "../x.html", ".htaccess", "a/.env", `a\b.html`} {
		if _, ok := CleanFile(in); ok {
			t.Errorf("CleanFile(%q) принят", in)
		}
	}
}

func TestNormalizeAcceptsAndCanonicalizes(t *testing.T) {
	got, err := Normalize(Settings{
		RootDir: " /dist/ ", Index: []string{" home.html ", "", "index.html", "home.html"}, Autoindex: true,
		ErrorPages: map[string]string{"404": "/errors/404.html", "500": "  ", "403": "403.html"}, WWW: "add", HSTS: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{
		RootDir: "dist", Index: []string{"home.html", "index.html"}, Autoindex: true,
		ErrorPages: map[string]string{"404": "errors/404.html", "403": "403.html"}, WWW: "add", HSTS: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("получили %+v, ожидали %+v", got, want)
	}
	if got, err := Normalize(Settings{}); err != nil || !reflect.DeepEqual(got, Settings{}) {
		t.Fatalf("пустые настройки: %+v %v", got, err)
	}
}

func TestNormalizeRejects(t *testing.T) {
	cases := []struct {
		name  string
		in    Settings
		field string
		code  string
	}{
		{"корень выше", Settings{RootDir: "../x"}, "root_dir", "invalid"},
		{"скрытый корень", Settings{RootDir: ".git"}, "root_dir", "invalid"},
		{"много индексов", Settings{Index: []string{"a", "b", "c", "d", "e", "f"}}, "index", "too_many"},
		{"индекс с путём", Settings{Index: []string{"a/b.html"}}, "index", "invalid"},
		{"индекс скрытый", Settings{Index: []string{".htaccess"}}, "index", "invalid"},
		{"индекс ..", Settings{Index: []string{".."}}, "index", "invalid"},
		{"индекс с обратным слэшем", Settings{Index: []string{`a\b`}}, "index", "invalid"},
		{"индекс с управляющим", Settings{Index: []string{"a\nb"}}, "index", "invalid"},
		{"неизвестный код", Settings{ErrorPages: map[string]string{"418": "x.html"}}, "error_pages", "status"},
		{"код не число", Settings{ErrorPages: map[string]string{"abc": "x.html"}}, "error_pages", "status"},
		{"код с нулём впереди", Settings{ErrorPages: map[string]string{"0404": "x.html"}}, "error_pages", "status"},
		{"путь выше", Settings{ErrorPages: map[string]string{"404": "../x.html"}}, "error_pages", "invalid"},
		{"каталог вместо файла", Settings{ErrorPages: map[string]string{"404": "errors/"}}, "error_pages", "invalid"},
		{"htaccess как страница", Settings{ErrorPages: map[string]string{"403": "sub/.htpasswd"}}, "error_pages", "invalid"},
		{"неверный www", Settings{WWW: "both"}, "www", "invalid"},
	}
	for _, c := range cases {
		_, err := Normalize(c.in)
		var p Problem
		if !errors.As(err, &p) || p.Field != c.field || p.Code != c.code {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if _, err := Normalize(Settings{ErrorPages: map[string]string{"404": "../x.html"}}); err == nil || !strings.Contains(err.Error(), "error_pages") {
		t.Error("текст ошибки")
	}
	var p Problem
	_ = errors.As(func() error { _, e := Normalize(Settings{ErrorPages: map[string]string{"410": "../x"}}); return e }(), &p)
	if p.Arg != 410 {
		t.Errorf("ошибка страницы должна называть код: %+v", p)
	}
}

func TestSanitizeDropsOnlyInvalidParts(t *testing.T) {
	got := Sanitize(Settings{
		RootDir: "../etc", Index: []string{"ok.html", "../bad", "", ".hidden", "b.html", "c", "d", "e", "f"},
		ErrorPages: map[string]string{"404": "e/404.html", "418": "x", "403": "../x", "500": ".htpasswd"}, WWW: "sideways", Autoindex: true, HSTS: true,
	})
	want := Settings{Index: []string{"ok.html", "b.html", "c", "d", "e"}, ErrorPages: map[string]string{"404": "e/404.html"}, Autoindex: true, HSTS: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("получили %+v, ожидали %+v", got, want)
	}
}

func TestIndexFilesDefault(t *testing.T) {
	if got := (Settings{}).IndexFiles(); !reflect.DeepEqual(got, DefaultIndex) {
		t.Fatal(got)
	}
	if got := (Settings{Index: []string{"home.html"}}).IndexFiles(); !reflect.DeepEqual(got, []string{"home.html"}) {
		t.Fatal(got)
	}
}

func TestParseToleratesGarbage(t *testing.T) {
	for _, data := range []string{"", "{", "null", "[]", `{"root_dir": 5}`, `{"index": "x"}`, "\x00\x01"} {
		if got := Parse([]byte(data)); !reflect.DeepEqual(got, Settings{}) {
			t.Errorf("Parse(%q) = %+v", data, got)
		}
	}
}

func TestWriteReadRoundTripAndAtomicity(t *testing.T) {
	dir := t.TempDir()
	if got, err := Read(dir); err != nil || !reflect.DeepEqual(got, Settings{}) {
		t.Fatalf("нет файла: %+v %v", got, err)
	}
	want := Settings{RootDir: "dist", Index: []string{"home.html"}, Autoindex: true, ErrorPages: map[string]string{"404": "404.html"}, WWW: "remove", HSTS: true}
	if err := Write(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v %v", got, err)
	}
	fi, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("права файла: %v %v", fi.Mode(), err)
	}
	// Перезапись целиком, временных файлов не остаётся.
	if err := Write(dir, Settings{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := Read(dir); !reflect.DeepEqual(got, Settings{}) {
		t.Fatalf("после перезаписи: %+v", got)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("остались временные файлы: %v", ents)
	}
	// Хостильный файл читается безопасно.
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`{"root_dir":"../../etc","index":["../x"],"www":"evil"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := Read(dir); !reflect.DeepEqual(got, Settings{}) {
		t.Fatalf("хостильный файл: %+v", got)
	}
}

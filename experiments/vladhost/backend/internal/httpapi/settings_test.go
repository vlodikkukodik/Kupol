package httpapi_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type settingsBody struct {
	Settings struct {
		RootDir    string            `json:"root_dir"`
		Index      []string          `json:"index"`
		Autoindex  bool              `json:"autoindex"`
		ErrorPages map[string]string `json:"error_pages"`
		WWW        string            `json:"www"`
		HSTS       bool              `json:"hsts"`
	} `json:"settings"`
	DefaultIndex  []string `json:"default_index"`
	ErrorStatuses []int    `json:"error_statuses"`
	MaxIndex      int      `json:"max_index"`
}

func TestSiteSettingsDefaults(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, host := e.createSite(john, "blog")
	w := e.do("GET", "/api/sites/"+itoa(id)+"/settings", nil, john)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"index":[]`) || !strings.Contains(w.Body.String(), `"error_pages":{}`) {
		t.Fatalf("умолчания должны быть пустыми списком и объектом, а не null: %d %s", w.Code, w.Body)
	}
	got := decode[settingsBody](t, w)
	if got.Settings.RootDir != "" || got.Settings.Autoindex || got.Settings.WWW != "" || got.Settings.HSTS ||
		!reflect.DeepEqual(got.DefaultIndex, []string{"index.html", "index.htm"}) || got.MaxIndex != 5 || len(got.ErrorStatuses) < 5 {
		t.Fatalf("%+v", got)
	}
	// Файла настроек нет, пока их не сохранили.
	if _, err := os.Stat(filepath.Join(e.root, host, "settings.json")); err == nil {
		t.Fatal("файл настроек не должен создаваться при чтении")
	}
}

func TestSiteSettingsSaveAndCanonicalize(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, host := e.createSite(john, "blog")
	url := "/api/sites/" + itoa(id) + "/settings"
	pub := filepath.Join(e.root, host, "public")
	if err := os.MkdirAll(filepath.Join(pub, "dist", "errors"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pub, "dist", "errors", "404.html"), []byte("nf"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := e.do("PUT", url, map[string]any{
		"root_dir": " /dist/ ", "index": []string{" home.html ", "", "index.html", "home.html"}, "autoindex": true,
		"error_pages": map[string]string{"404": "/errors/404.html", "500": " "}, "www": "add", "hsts": true,
	}, john)
	if w.Code != 200 {
		t.Fatalf("сохранение: %d %s", w.Code, w.Body)
	}
	got := decode[settingsBody](t, w).Settings
	if got.RootDir != "dist" || !reflect.DeepEqual(got.Index, []string{"home.html", "index.html"}) || !got.Autoindex ||
		!reflect.DeepEqual(got.ErrorPages, map[string]string{"404": "errors/404.html"}) || got.WWW != "add" || !got.HSTS {
		t.Fatalf("канонический вид: %+v", got)
	}
	// Тот же результат при чтении, а на диске файл лежит рядом с public (не внутри: пользователь не должен его менять).
	if again := decode[settingsBody](t, e.do("GET", url, nil, john)).Settings; !reflect.DeepEqual(again, got) {
		t.Fatalf("чтение: %+v", again)
	}
	raw, err := os.ReadFile(filepath.Join(e.root, host, "settings.json"))
	if err != nil || !strings.Contains(string(raw), `"root_dir": "dist"`) {
		t.Fatalf("файл настроек: %v %s", err, raw)
	}
	if _, err := os.Stat(filepath.Join(pub, "settings.json")); err == nil {
		t.Fatal("настройки не должны лежать в public")
	}
	if w := e.do("GET", "/api/sites/"+itoa(id)+"/file?path=settings.json", nil, john); w.Code == 200 {
		t.Fatalf("файл настроек виден через файловый менеджер: %s", w.Body)
	}

	// Сброс к умолчаниям.
	w = e.do("PUT", url, map[string]any{}, john)
	if w.Code != 200 || decode[settingsBody](t, w).Settings.RootDir != "" {
		t.Fatalf("сброс: %d %s", w.Code, w.Body)
	}
}

func TestSiteSettingsCreatesRootDir(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, host := e.createSite(john, "blog")
	if w := e.do("PUT", "/api/sites/"+itoa(id)+"/settings", map[string]any{"root_dir": "build/out"}, john); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if fi, err := os.Stat(filepath.Join(e.root, host, "public", "build", "out")); err != nil || !fi.IsDir() {
		t.Fatalf("корневая папка не создана: %v", err)
	}
}

func TestSiteSettingsValidation(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, host := e.createSite(john, "blog")
	url := "/api/sites/" + itoa(id) + "/settings"
	pub := filepath.Join(e.root, host, "public")
	if err := os.MkdirAll(pub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pub, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.html"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	linked := true
	if err := os.Symlink(filepath.Join(outside, "secret.html"), filepath.Join(pub, "link.html")); err != nil {
		linked = false
	}

	cases := []struct {
		name  string
		body  map[string]any
		code  string
		field string
	}{
		{"корень выше", map[string]any{"root_dir": "../x"}, "validation.settings_root_dir", "root_dir"},
		{"корень скрытый", map[string]any{"root_dir": ".git"}, "validation.settings_root_dir", "root_dir"},
		{"корень — файл", map[string]any{"root_dir": "file.txt"}, "validation.settings_root_dir", "root_dir"},
		{"много индексов", map[string]any{"index": []string{"a", "b", "c", "d", "e", "f"}}, "validation.settings_index_many", "index"},
		{"индекс с путём", map[string]any{"index": []string{"a/b.html"}}, "validation.settings_index", "index"},
		{"индекс скрытый", map[string]any{"index": []string{".htaccess"}}, "validation.settings_index", "index"},
		{"неизвестный код", map[string]any{"error_pages": map[string]string{"418": "x.html"}}, "validation.settings_error_status", "error_pages"},
		{"путь страницы выше", map[string]any{"error_pages": map[string]string{"404": "../x.html"}}, "validation.settings_error_path", "error_pages"},
		{"страница — .htaccess", map[string]any{"error_pages": map[string]string{"403": ".htaccess"}}, "validation.settings_error_path", "error_pages"},
		{"файла страницы нет", map[string]any{"error_pages": map[string]string{"404": "nope.html"}}, "validation.settings_error_file", "error_pages"},
		{"страница — каталог", map[string]any{"root_dir": "d", "error_pages": map[string]string{"404": "."}}, "validation.settings_error_path", "error_pages"},
		{"неверный www", map[string]any{"www": "both"}, "validation.settings_www", "www"},
	}
	if linked {
		cases = append(cases, struct {
			name  string
			body  map[string]any
			code  string
			field string
		}{"страница — ссылка наружу", map[string]any{"error_pages": map[string]string{"404": "link.html"}}, "validation.settings_error_file", "error_pages"})
	}
	for _, c := range cases {
		w := e.do("PUT", url, c.body, john)
		got := decode[errBody](t, w).Error
		if w.Code != 422 || got.Code != c.code || got.Field != c.field {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
	}
	// Неудачное сохранение ничего не меняет.
	if _, err := os.Stat(filepath.Join(e.root, host, "settings.json")); err == nil {
		t.Fatal("после отказов появился файл настроек")
	}
	if w := e.do("PUT", url, "not-an-object", john); w.Code != 400 {
		t.Errorf("не объект: %d", w.Code)
	}
}

func TestSiteSettingsErrorMessageNamesTheCodeInBothLanguages(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")
	url := "/api/sites/" + itoa(id) + "/settings"
	body := map[string]any{"error_pages": map[string]string{"404": "nope.html"}}
	ru := e.doLang("ru", "PUT", url, body, john).Body.String()
	it := e.doLang("it", "PUT", url, body, john).Body.String()
	if !strings.Contains(ru, "404") || !strings.Contains(ru, "не найден") || !strings.Contains(it, "404") || !strings.Contains(it, "non trovato") {
		t.Fatalf("ru=%s it=%s", ru, it)
	}
}

func TestSiteSettingsOwnership(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	id, _ := e.createSite(john, "blog")
	url := "/api/sites/" + itoa(id) + "/settings"
	for _, m := range []string{"GET", "PUT"} {
		if w := e.do(m, url, map[string]any{"hsts": true}, mary); w.Code != 404 {
			t.Errorf("%s чужим: %d", m, w.Code)
		}
		if w := e.do(m, url, map[string]any{}, ""); w.Code != 401 {
			t.Errorf("%s без токена: %d", m, w.Code)
		}
	}
	// Чужой запрос ничего не записал.
	if got := decode[settingsBody](t, e.do("GET", url, nil, john)).Settings; got.HSTS {
		t.Fatal("чужой пользователь изменил настройки")
	}
}

func TestDeletingSiteRemovesSettings(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, host := e.createSite(john, "blog")
	e.do("PUT", "/api/sites/"+itoa(id)+"/settings", map[string]any{"hsts": true}, john)
	if w := e.do("DELETE", "/api/sites/"+itoa(id), nil, john); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if _, err := os.Stat(filepath.Join(e.root, host)); err == nil {
		t.Fatal("каталог сайта с настройками остался")
	}
	// Новый сайт с тем же адресом начинает с настроек по умолчанию.
	id2, _ := e.createSite(john, "blog")
	if got := decode[settingsBody](t, e.do("GET", "/api/sites/"+itoa(id2)+"/settings", nil, john)).Settings; got.HSTS {
		t.Fatal("новый сайт унаследовал настройки удалённого")
	}
}

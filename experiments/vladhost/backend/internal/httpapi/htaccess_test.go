package httpapi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type htaccessBody struct {
	Files []struct {
		Path  string `json:"path"`
		Diags []struct {
			Line      int    `json:"line"`
			Directive string `json:"directive"`
			Code      string `json:"code"`
		} `json:"diags"`
	} `json:"files"`
}

func TestHtaccessCheckEndpoint(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	id, host := e.createSite(john, "blog")
	url := "/api/sites/" + itoa(id) + "/htaccess"

	// Без .htaccess — пустой список (не null).
	w := e.do("GET", url, nil, john)
	if w.Code != 200 || w.Body.String() != `{"files":[]}` {
		t.Fatalf("пусто: %d %s", w.Code, w.Body)
	}

	pub := filepath.Join(e.root, host, "public")
	mustWrite := func(rel, body string) {
		t.Helper()
		p := filepath.Join(pub, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(".htaccess", "RewriteEngine On\nphp_value memory_limit 256M\nHeader set X-A 1\n")
	mustWrite("docs/.htaccess", "Options -Indexes\nAddHandler cgi-script .cgi\n")
	mustWrite("docs/other.txt", "not htaccess")
	mustWrite("clean/.htaccess", "Options -Indexes\n")

	got := decode[htaccessBody](t, e.do("GET", url, nil, john))
	if len(got.Files) != 3 {
		t.Fatalf("файлы: %+v", got.Files)
	}
	byPath := map[string]int{}
	for _, f := range got.Files {
		byPath[f.Path] = len(f.Diags)
		for _, d := range f.Diags {
			if d.Code != "unsupported" || d.Line == 0 {
				t.Errorf("%s: %+v", f.Path, d)
			}
		}
	}
	if byPath[".htaccess"] != 1 || byPath["docs/.htaccess"] != 1 {
		t.Fatalf("замечания по файлам: %v", byPath)
	}

	// Файл без замечаний: в JSON пустой список, а не null (фронтенд разбирает ответ строго).
	if w := e.do("GET", url, nil, john); !strings.Contains(w.Body.String(), `"path":"clean/.htaccess","diags":[]`) {
		t.Fatalf("diags без замечаний должен быть []: %s", w.Body)
	}

	// Чужой сайт и аноним.
	if w := e.do("GET", url, nil, mary); w.Code != 404 {
		t.Fatalf("чужой сайт: %d", w.Code)
	}
	if w := e.do("GET", url, nil, ""); w.Code != 401 {
		t.Fatalf("аноним: %d", w.Code)
	}
}

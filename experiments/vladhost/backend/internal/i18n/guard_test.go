package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Пакеты, тексты которых видит пользователь: русских строк в них быть не должно, только ключи каталога.
// Исключены пакеты для оператора (конфигурация, миграции, CLI): их сообщения не уходят пользователю.
var userFacing = []string{"httpapi", "sites", "auth", "ftpd", "apperr"}

func sourceFiles(t *testing.T, pkgs ...string) []string {
	t.Helper()
	var files []string
	for _, p := range pkgs {
		matches, err := filepath.Glob(filepath.Join("..", p, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range matches {
			if !strings.HasSuffix(f, "_test.go") {
				files = append(files, f)
			}
		}
	}
	if len(files) == 0 {
		t.Fatal("не найдено ни одного исходного файла")
	}
	return files
}

// Сторож: кириллица в строковых литералах пользовательских пакетов допускается только в вызовах log.*
// (журнал для оператора). Всё остальное обязано идти через каталог.
func TestNoHardcodedRussianInUserFacingCode(t *testing.T) {
	fset := token.NewFileSet()
	for _, file := range sourceFiles(t, userFacing...) {
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		walk := func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.CallExpr:
				if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "log" {
						return false // журнал оператора
					}
				}
			case *ast.BasicLit:
				if v.Kind == token.STRING && hasCyrillic(v.Value) {
					t.Errorf("%s: русский текст в коде — вынесите в каталог i18n: %s", fset.Position(v.Pos()), v.Value)
				}
			}
			return true
		}
		ast.Inspect(f, walk)
	}
}

var (
	reNew        = regexp.MustCompile(`apperr\.New\(\s*[^,]+,\s*"([^"]+)"`)
	reValidation = regexp.MustCompile(`apperr\.Validation\(\s*"[^"]*",\s*"([^"]+)"`)
	reArchive    = regexp.MustCompile(`badArchive\("([a-z_]+)"`)
	reFail       = regexp.MustCompile(`fail\(c,\s*[^,]+,\s*"([^"]+)"`)
	reKey        = regexp.MustCompile(`i18n\.(?:T|Bi)\((?:[a-zA-Z.]+,\s*)?"([^"]+)"`)
)

// usedKeys собирает ключи каталога, к которым обращается код (по шаблонам вызовов).
func usedKeys(t *testing.T) map[string]string {
	t.Helper()
	used := map[string]string{}
	for _, file := range sourceFiles(t, userFacing...) {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		add := func(re *regexp.Regexp, prefix string) {
			for _, m := range re.FindAllStringSubmatch(src, -1) {
				if strings.HasSuffix(m[1], ".") {
					continue // начало составного ключа ("err."+код), сам ключ собирается на лету
				}
				used[prefix+m[1]] = file
			}
		}
		add(reNew, "err.")
		add(reValidation, "err.validation.")
		add(reArchive, "err.archive.")
		add(reFail, "err.")
		add(reKey, "")
	}
	return used
}

// Каждый код ошибки, который может уйти клиенту, есть в обоих каталогах, и наоборот:
// в каталогах нет мёртвых ключей, на которые никто не ссылается.
func TestCatalogMatchesCode(t *testing.T) {
	used := usedKeys(t)
	if len(used) < 20 {
		t.Fatalf("сторож нашёл слишком мало ключей (%d) — не сломались ли шаблоны поиска?", len(used))
	}
	for key, file := range used {
		for lang, cat := range map[Lang]map[string]string{RU: ru, IT: it} {
			if _, ok := cat[key]; !ok {
				t.Errorf("код использует ключ %q (%s), но его нет в каталоге %s", key, file, lang)
			}
		}
	}
	var dead []string
	for key := range ru {
		if _, ok := used[key]; !ok {
			dead = append(dead, key)
		}
	}
	sort.Strings(dead)
	for _, key := range dead {
		t.Errorf("ключ %q есть в каталоге, но код на него не ссылается", key)
	}
}

func TestGuardFindsViolations(t *testing.T) {
	// Сам сторож не должен молчать: проверяем на заведомо плохом коде, что кириллица находится.
	src := "package x\nvar a = \"привет\"\nfunc f() { log.Println(\"журнал\") }\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "log" {
					return false
				}
			}
		}
		if l, ok := n.(*ast.BasicLit); ok && l.Kind == token.STRING && hasCyrillic(l.Value) {
			s, _ := strconv.Unquote(l.Value)
			found = append(found, s)
		}
		return true
	})
	if len(found) != 1 || found[0] != "привет" {
		t.Fatalf("сторож должен находить литерал вне log и пропускать log: %v", found)
	}
}

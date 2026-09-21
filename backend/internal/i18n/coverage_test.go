package i18n

import (
	"fmt"
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

// Сторож перевода: каждый русский текст в исходниках сервера, который может дойти до читателя, обязан иметь итальянский
// перевод. Тест разбирает исходники (go/ast), собирает строковые литералы с кириллицей и сверяет их с каталогом.
// Не проверяются: сообщения журнала, errors.New и fmt.Errorf (внутренние ошибки), служебные пакеты (config, database, audit…)
// и команда kupol — они для оператора, а не для читателя, — а также «данные», не тексты интерфейса (см. dataKeys).

var cyr = regexp.MustCompile(`[А-Яа-яЁё]`)

// usedElsewhere — тексты каталога, которые в исходниках не видны как литералы (собираются иначе); пока таких нет.
var usedElsewhere = map[string]bool{
	"%s № %s": true, // «№» — не буква кириллицы: тест такой литерал не видит
}

// Не интерфейс: служебные пакеты, где сообщения — для оператора или журнала; captcha.go — вопросы анкеты на каждом
// языке написаны в самом файле (у итальянского и ответы свои).
var skipDirs = []string{"/internal/accounts/captcha.go", "/cmd/", "/internal/audit/", "/internal/config/", "/internal/database/", "/internal/testutil/", "/internal/proxyauth/", "/internal/passwords/", "/internal/ratelimit/", "/internal/logging/", "/internal/i18n/"}

// Внутренние ошибки: «пакет: текст» — текст для журнала, не для читателя.
var internalPrefix = regexp.MustCompile(`^(documents|accounts|proxyauth|passwords|database): `)

// dataKeys — русские строки, которые не показываются читателю как текст: ключи (приставки шифров, ответы анкеты,
// зарезервированные логины), название приложения-аутентификатора, примеры шифров. Переводить их нельзя.
var dataKeys = map[string]bool{
	"ё": true, "е": true, "О": true, "ПРИКАЗ": true, "ИНЦ": true, "ЛД": true, "ОТД": true, "ОБ": true, "ПРОТ": true, "ПОК": true, "МЕМО": true, "О-": true,
	"КУПОЛ": true, "Форма КУПОЛ-1": true,
	"О-041": true, "ПРИКАЗ-1978-12": true, "ИНЦ-1982-07": true, "ЛД-0157": true, "ОТД-2 или ОБ-14": true, "ПРОТ-1979-03": true, "ПОК-1980-22": true, "МЕМО-5": true,
	// зарезервированные логины и сокращения (accounts/rules.go)
	"купол": true, "директорат": true, "особый_совет": true, "особыйсовет": true, "совет": true, "комитет": true, "гражданин": true, "посетитель": true,
	"стажер": true, "сотрудник": true, "надзиратель": true, "куратор": true, "автор": true, "редактор": true, "модератор": true, "архивариус": true, "цак": true,
	"начальник": true, "начальник_купол": true,
	// разметка закрытого фрагмента в Markdown-выгрузке (часть формата, а не текст интерфейса)
	"]{допуск=": true,
	// пояснения для разработчика: почему вид блока не попадает в поисковый индекс (documents/search_index.go)
	"нет собственного текста: строится из свойств документа": true, "нет текста": true, "только номер страницы": true,
	"шифр и примечание видны читателю только при доступной цели": true,
	// текст, который запоминается в базе (пометка версии) и пишется в журнал: не переводится
	"Откат к версии %d (редакция %d, %s)": true, "документ не прошёл проверку (%d):\n%s": true,
}

func skipCall(call *ast.CallExpr) bool {
	switch f := call.Fun.(type) {
	case *ast.SelectorExpr:
		x := ""
		if id, ok := f.X.(*ast.Ident); ok {
			x = id.Name
		}
		name := f.Sel.Name
		if (x == "errors" && name == "New") || (x == "fmt" && (name == "Errorf" || strings.HasPrefix(name, "Fprint") || strings.HasPrefix(name, "Print"))) {
			return true
		}
		switch name {
		case "Info", "Warn", "Error", "Debug", "InfoContext", "WarnContext", "ErrorContext", "Fatal", "Fatalf", "Printf", "Println":
			return true
		}
	case *ast.Ident:
		return f.Name == "panic"
	}
	return false
}

// userStrings — русские строковые литералы сервера, способные попасть в ответ: msgid → «файл:строка».
func userStrings(t *testing.T) map[string]string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	out := map[string]string{}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		for _, dir := range skipDirs {
			if strings.Contains(path, dir) {
				return nil
			}
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		skipped := map[*ast.BasicLit]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && skipCall(call) {
				ast.Inspect(call, func(m ast.Node) bool {
					if bl, ok := m.(*ast.BasicLit); ok {
						skipped[bl] = true
					}
					return true
				})
			}
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			bl, ok := n.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING || skipped[bl] {
				return true
			}
			s, err := strconv.Unquote(bl.Value)
			if err != nil || !cyr.MatchString(s) || dataKeys[s] || internalPrefix.MatchString(s) {
				return true
			}
			if _, seen := out[s]; !seen {
				out[s] = fmt.Sprintf("%s:%d", strings.TrimPrefix(fset.Position(bl.Pos()).Filename, root+"/"), fset.Position(bl.Pos()).Line)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEveryUserStringIsTranslatedToItalian(t *testing.T) {
	var missing []string
	for s, where := range userStrings(t) {
		if !IT.Has(s) {
			missing = append(missing, fmt.Sprintf("%s\t%q", where, s))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("нет итальянского перевода (%d):\n%s", len(missing), strings.Join(missing, "\n"))
	}
}

// Ключ каталога, которого нет в исходниках, — мусор от переименованного или удалённого текста.
func TestCatalogHasNoStaleKeys(t *testing.T) {
	used := userStrings(t)
	var stale []string
	for _, k := range Keys(IT) {
		if _, ok := used[k]; !ok && !usedElsewhere[k] {
			stale = append(stale, strconv.Quote(k))
		}
	}
	if len(stale) > 0 {
		t.Fatalf("в каталоге есть ключи, которых нет в исходниках (%d):\n%s", len(stale), strings.Join(stale, "\n"))
	}
}

var verbRe = regexp.MustCompile(`%(?:\[\d+\])?[-+# 0]*\d*(?:\.\d+)?[a-zA-Z%]`)

// Перевод обязан принимать те же значения, что и оригинал: набор подстановок (%s, %d, %q…) совпадает.
func TestTranslationsKeepFormatVerbs(t *testing.T) {
	norm := func(s string) string {
		var verbs []string
		for _, v := range verbRe.FindAllString(s, -1) {
			if v != "%%" {
				verbs = append(verbs, verbRe.ReplaceAllString(v, "%")+string(v[len(v)-1]))
			}
		}
		sort.Strings(verbs)
		return strings.Join(verbs, ",")
	}
	for k, v := range Catalog(IT) {
		if norm(k) != norm(v) {
			t.Errorf("подстановки не совпадают:\n  %q\n  %q", k, v)
		}
		if cyr.MatchString(v) {
			t.Errorf("в итальянском переводе кириллица: %q", v)
		}
	}
}

func TestParse(t *testing.T) {
	cases := map[string]Lang{
		"":                             RU,
		"ru-RU,ru;q=0.9":               RU,
		"it-IT,it;q=0.9,en;q=0.8":      IT,
		"it":                           IT,
		"en-US,en;q=0.9,it;q=0.5":      IT,
		"en-US,de":                     RU,
		"ru;q=0.5,it;q=0.9":            IT,
		"it;q=0.2,ru;q=0.9":            RU,
		"IT-it":                        IT,
		"*":                            RU,
		"it;q=oops":                    IT,
		"fr, it;q=0.8, ru;q=0.8":       IT, // при равном весе — тот, что раньше
		"ru;q=0.8, it;q=0.8, fr;q=0.9": RU,
	}
	for in, want := range cases {
		if got := Parse(in); got != want {
			t.Errorf("Parse(%q) = %q, ожидали %q", in, got, want)
		}
	}
}

func TestTranslateFallsBackToRussianAndSubstitutes(t *testing.T) {
	if got := IT.T("нет такого текста %d", 5); got != "нет такого текста 5" {
		t.Errorf("непереведённый текст должен остаться русским: %q", got)
	}
	if got := RU.T("год — от %d до %d", 1900, 2099); got != "год — от 1900 до 2099" {
		t.Errorf("русский: %q", got)
	}
}

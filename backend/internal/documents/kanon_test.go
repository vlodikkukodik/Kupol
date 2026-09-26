package documents

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// docs/kanon.md — справочник автора и полное описание формата загрузки. Чтобы описание не устаревало, каждый JSON-пример из него
// разбирается настоящим разбором загрузки, а список видов блоков и типов документов сверяется с кодом.
const kanonPath = "../../../docs/kanon.md"

var (
	// <!-- import-example --> или <!-- import-error: подстрока --> и следом блок ```json … ```
	exampleRe = regexp.MustCompile("(?s)<!-- import-(example|error)(?:: ([^>]*?))? -->\\s*```json\\n(.*?)\\n```")
	// первый столбец таблицы вида: | `heading` | … — в разделе про блоки (тип блока в обратных кавычках)
	kindRowRe = regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\|")
)

func readKanon(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(kanonPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// problemsOf разбирает файл так же, как загрузка командой: разбор пакета, затем проверка каждого документа.
func problemsOf(raw []byte) []Problem {
	inputs, batch, err := ParseInputs(raw)
	if err != nil {
		if ve, ok := err.(*ValidationError); ok {
			return ve.Problems
		}
		return []Problem{{Path: "$", Message: err.Error()}}
	}
	var probs Problems
	for i := range inputs {
		path := "$"
		if batch {
			path = fmt.Sprintf("documents[%d]", i)
		}
		inputs[i].prepare(path, &probs)
	}
	return probs.List()
}

func TestKanonExamplesAreValid(t *testing.T) {
	doc := readKanon(t)
	matches := exampleRe.FindAllStringSubmatch(doc, -1)
	var okCount, errCount int
	for i, m := range matches {
		kind, want, body := m[1], m[2], m[3]
		probs := problemsOf([]byte(body))
		switch kind {
		case "example":
			okCount++
			if len(probs) > 0 {
				t.Errorf("пример № %d из kanon.md не проходит проверку: %v", i+1, probs)
			}
		case "error":
			errCount++
			if len(probs) == 0 {
				t.Errorf("пример ошибки № %d из kanon.md, а файл проходит проверку", i+1)
				continue
			}
			found := false
			for _, p := range probs {
				if strings.Contains(p.Message, want) {
					found = true
				}
			}
			if !found {
				t.Errorf("пример ошибки № %d: ждали замечание со словами %q, получили %v", i+1, want, probs)
			}
		}
	}
	// пример пропал или разметка сломалась — тест не должен молча ничего не проверять
	if okCount < 4 || errCount < 3 {
		t.Fatalf("в kanon.md найдено примеров: %d верных и %d с ошибкой (ждём не меньше 4 и 3)", okCount, errCount)
	}
}

// Полный пример из kanon.md обязан содержать все виды блоков — иначе он перестаёт быть полным.
func TestKanonFullExampleCoversEveryBlockKind(t *testing.T) {
	doc := readKanon(t)
	var full string
	for _, m := range exampleRe.FindAllStringSubmatch(doc, -1) {
		if strings.Contains(m[3], `"О-141"`) {
			full = m[3]
		}
	}
	if full == "" {
		t.Fatal("в kanon.md нет полного примера (О-141)")
	}
	inputs, _, err := ParseInputs([]byte(full))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, b := range inputs[0].Blocks {
		seen[b.Type] = true
	}
	for name := range kinds {
		if !seen[name] {
			t.Errorf("в полном примере kanon.md нет блока %q", name)
		}
	}
}

// Каждый вид блока и каждый тип документа описан в kanon.md.
func TestKanonDescribesEveryKindAndType(t *testing.T) {
	doc := readKanon(t)
	documented := map[string]bool{}
	for _, m := range kindRowRe.FindAllStringSubmatch(doc, -1) {
		documented[m[1]] = true
	}
	var missing []string
	for name := range kinds {
		if !documented[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("в kanon.md не описаны блоки: %v", missing)
	}
	for _, name := range []string{"heading", "paragraph", "list", "quote", "dossier_header", "experiment_log", "stamp", "memo", "clipping", "table", "doc_link", "divider", "page", "footnote", "appendix"} {
		if _, ok := kinds[name]; !ok {
			t.Errorf("kanon.md описывает блок %q, а в коде такого нет", name)
		}
		if !strings.Contains(doc, "`"+name+"`") {
			t.Errorf("в kanon.md нет блока %q", name)
		}
	}
	for _, typ := range Types {
		if !strings.Contains(doc, "`"+string(typ)+"`") {
			t.Errorf("в kanon.md не описан тип документа %q", typ)
		}
		if !strings.Contains(doc, typ.Name()) {
			t.Errorf("в kanon.md нет названия типа %q", typ.Name())
		}
	}
	for level := 0; level <= MaxLevel; level++ {
		if !strings.Contains(doc, "| "+fmt.Sprint(level)+" | "+LevelName(level)+" |") {
			t.Errorf("в таблице уровней kanon.md нет строки уровня %d (%s)", level, LevelName(level))
		}
	}
}

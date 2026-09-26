package documents

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func ib(kind, data string) InputBlock {
	return InputBlock{Type: kind, Data: json.RawMessage(data)}
}

func normalize(t *testing.T, in ...InputBlock) ([]Block, []Problem) {
	t.Helper()
	var p Problems
	out := NormalizeBlocks(in, &p)
	return out, p.List()
}

func mustNormalize(t *testing.T, in ...InputBlock) []Block {
	t.Helper()
	out, probs := normalize(t, in...)
	if len(probs) > 0 {
		t.Fatalf("неожиданные замечания: %v", probs)
	}
	return out
}

// mustFail проверяет, что есть замечание с указанным путём и фрагментом текста.
func mustFail(t *testing.T, desc string, in []InputBlock, path, contains string) {
	t.Helper()
	_, probs := normalize(t, in...)
	for _, p := range probs {
		if p.Path == path && strings.Contains(p.Message, contains) {
			return
		}
	}
	t.Errorf("%s: ожидалось замечание %s с %q, получено %v", desc, path, contains, probs)
}

func TestRichAcceptsStringAndRuns(t *testing.T) {
	blocks := mustNormalize(t,
		ib("paragraph", `{"text":"Простая строка"}`),
		ib("paragraph", `{"text":[{"text":"Жирный ","bold":true},{"text":"секрет","level":4},{"text":" курсив","italic":true}]}`),
	)
	if got := string(blocks[0].Data); got != `{"text":[{"text":"Простая строка"}]}` {
		t.Errorf("строка должна стать одним фрагментом: %s", got)
	}
	if got := string(blocks[1].Data); got != `{"text":[{"text":"Жирный ","bold":true},{"text":"секрет","level":4},{"text":" курсив","italic":true}]}` {
		t.Errorf("канонический вид: %s", got)
	}
}

func TestRichRejections(t *testing.T) {
	one := func(data string) []InputBlock { return []InputBlock{ib("paragraph", data)} }
	mustFail(t, "число вместо текста", one(`{"text":5}`), "blocks[0].data", "строка или массив фрагментов")
	mustFail(t, "объект вместо текста", one(`{"text":{"text":"x"}}`), "blocks[0].data", "строка или массив фрагментов")
	mustFail(t, "неизвестное поле во фрагменте", one(`{"text":[{"text":"x","colour":"red"}]}`), "blocks[0].data", "неизвестное поле")
	mustFail(t, "пустой текст", one(`{"text":""}`), "blocks[0].data.text", "не может быть пустым")
	mustFail(t, "только пробелы", one(`{"text":"   "}`), "blocks[0].data.text", "не может быть пустым")
	mustFail(t, "пустой массив", one(`{"text":[]}`), "blocks[0].data.text", "не может быть пустым")
	mustFail(t, "пустой фрагмент", one(`{"text":[{"text":"a"},{"text":""}]}`), "blocks[0].data.text[1].text", "не может быть пустым")
	mustFail(t, "уровень фрагмента 8", one(`{"text":[{"text":"a","level":8}]}`), "blocks[0].data.text[0].level", "от 0 до 7")
	mustFail(t, "отрицательный уровень фрагмента", one(`{"text":[{"text":"a","level":-1}]}`), "blocks[0].data.text[0].level", "от 0 до 7")
	mustFail(t, "управляющий символ", one("{\"text\":\"a\\u0000b\"}"), "blocks[0].data.text", "управляющие символы")
	mustFail(t, "слишком длинный текст", one(`{"text":"`+strings.Repeat("я", maxRichText+1)+`"}`), "blocks[0].data.text", "слишком длинный")
	mustFail(t, "нет поля text", one(`{}`), "blocks[0].data.text", "не может быть пустым")

	// перевод строки и табуляция допустимы
	mustNormalize(t, ib("paragraph", `{"text":"строка\nвторая\tчерез табуляцию"}`))
}

func TestBlockCanonicalDefaults(t *testing.T) {
	blocks := mustNormalize(t,
		ib("heading", `{"text":"Заголовок"}`),
		ib("stamp", `{"text":"СЕКРЕТНО"}`),
		ib("divider", `{}`),
		ib("dossier_header", ``),
		ib("doc_link", `{"code":"o-41"}`),
	)
	want := []string{
		`{"depth":2,"text":"Заголовок"}`,
		`{"text":"СЕКРЕТНО","tone":"red"}`,
		`{"style":"line"}`,
		`{}`,
		`{"code":"О-041"}`,
	}
	for i, w := range want {
		if got := string(blocks[i].Data); got != w {
			t.Errorf("блок %d (%s): %s, ожидалось %s", i, blocks[i].Type, got, w)
		}
	}
	// идентификаторы по умолчанию — по положению
	for i, b := range blocks {
		if b.ID != fmt.Sprintf("b%d", i+1) {
			t.Errorf("id блока %d: %q", i, b.ID)
		}
	}
}

func TestEveryKindHasValidMinimalForm(t *testing.T) {
	minimal := map[string]string{
		"heading":        `{"text":"З"}`,
		"paragraph":      `{"text":"А"}`,
		"list":           `{"items":["а","б"]}`,
		"quote":          `{"text":"Ц"}`,
		"dossier_header": `{}`,
		"experiment_log": `{"entries":[{"text":"Запись"}]}`,
		"stamp":          `{"text":"ИЗЪЯТО"}`,
		"memo":           `{"kind":"letter","body":["Текст"]}`,
		"clipping":       `{"kind":"handwritten","paragraphs":["Текст"]}`,
		"table":          `{"columns":["А"],"rows":[["1"]]}`,
		"doc_link":       `{"code":"О-1"}`,
		"divider":        `{}`,
		"page":           `{}`,
		"footnote":       `{"mark":"1","text":"Сноска"}`,
		"appendix":       `{"title":"Приложение"}`,

		"containment_procedure": `{"steps":["Шаг"]}`,
		"directive":             `{"recipient":"Отдел","order":"Предписание"}`,
		"incident_timeline":     `{"entries":[{"event":"Событие"}]}`,
		"personnel_record":      `{}`,
		"roster":                `{"entries":[{"name":"Имя"}]}`,
		"hypothesis":            `{"text":"Гипотеза","result":"Результат"}`,
		"qa":                    `{"entries":[{"question":"В","answer":"О"}]}`,
		"routing":               `{"entries":[{"who":"Кто","decision":"approved"}]}`,
		"image":                 `{"upload":"0123456789abcdef0123456789abcdef"}`,
		"audio":                 `{"upload":"0123456789abcdef0123456789abcdef"}`,
	}
	for _, kind := range KindNames() {
		data, ok := minimal[kind]
		if !ok {
			t.Errorf("в тесте нет минимальной формы для %q", kind)
			continue
		}
		blocks, probs := normalize(t, ib(kind, data))
		if len(probs) > 0 || len(blocks) != 1 {
			t.Errorf("%s: %v", kind, probs)
		}
	}
	if len(minimal) != len(KindNames()) {
		t.Errorf("типов в тесте %d, в KindNames %d", len(minimal), len(KindNames()))
	}
}

func TestBlockRejections(t *testing.T) {
	// общие ошибки блока
	mustFail(t, "нет типа", []InputBlock{{Data: json.RawMessage(`{}`)}}, "blocks[0].type", "не указан тип")
	mustFail(t, "неизвестный тип", []InputBlock{ib("hologram", `{}`)}, "blocks[0].type", "неизвестный тип блока")
	mustFail(t, "картинка без ключа", []InputBlock{ib("image", `{}`)}, "blocks[0].data.upload", "ключ загруженного файла")
	mustFail(t, "аудио с чужим ключом", []InputBlock{ib("audio", `{"upload":"../etc"}`)}, "blocks[0].data.upload", "ключ загруженного файла")
	mustFail(t, "уровень блока 9", []InputBlock{{Type: "divider", Level: intp(9)}}, "blocks[0].level", "от 0 до 7")
	mustFail(t, "неизвестное поле блока", []InputBlock{ib("heading", `{"text":"З","colour":"red"}`)}, "blocks[0].data", `неизвестное поле "colour"`)
	mustFail(t, "мусор после данных", []InputBlock{ib("divider", `{} {}`)}, "blocks[0].data", "")
	mustFail(t, "некорректный JSON данных", []InputBlock{ib("heading", `{"text":`)}, "blocks[0].data", "")

	// идентификаторы
	mustFail(t, "плохой id", []InputBlock{{ID: "Плохой id", Type: "divider"}}, "blocks[0].id", "не подходит")
	mustFail(t, "заглавные в id", []InputBlock{{ID: "Block1", Type: "divider"}}, "blocks[0].id", "не подходит")
	mustFail(t, "дубль id", []InputBlock{{ID: "x", Type: "divider"}, {ID: "x", Type: "divider"}}, "blocks[1].id", "уже занят блоком blocks[0]")
	mustFail(t, "id по умолчанию совпал с явным", []InputBlock{{ID: "b2", Type: "divider"}, {Type: "divider"}}, "blocks[1].id", "уже занят")

	// шапка одна
	mustFail(t, "две шапки", []InputBlock{ib("dossier_header", `{}`), ib("dossier_header", `{}`)}, "blocks[1].type", "только одна")

	// heading
	mustFail(t, "глубина 4", []InputBlock{ib("heading", `{"depth":4,"text":"З"}`)}, "blocks[0].data.depth", "от 1 до 3")
	mustFail(t, "пустой заголовок", []InputBlock{ib("heading", `{"text":""}`)}, "blocks[0].data.text", "не может быть пустым")
	mustFail(t, "слишком длинный заголовок", []InputBlock{ib("heading", `{"text":"`+strings.Repeat("я", 301)+`"}`)}, "blocks[0].data.text", "слишком длинное")
	mustFail(t, "глубина строкой", []InputBlock{ib("heading", `{"depth":"2","text":"З"}`)}, "blocks[0].data", "ожидается число")

	// list
	mustFail(t, "пустой список", []InputBlock{ib("list", `{"items":[]}`)}, "blocks[0].data.items", "нужен хотя бы 1")
	mustFail(t, "пустой пункт", []InputBlock{ib("list", `{"items":["а",""]}`)}, "blocks[0].data.items[1]", "не может быть пустым")

	// stamp
	mustFail(t, "тон штампа", []InputBlock{ib("stamp", `{"text":"С","tone":"green"}`)}, "blocks[0].data.tone", "допустимы: red, ink")
	mustFail(t, "наклон штампа", []InputBlock{ib("stamp", `{"text":"С","tilt":40}`)}, "blocks[0].data.tilt", "от -15 до 15")
	mustFail(t, "длинный штамп", []InputBlock{ib("stamp", `{"text":"`+strings.Repeat("Я", 41)+`"}`)}, "blocks[0].data.text", "слишком длинное")

	// memo
	mustFail(t, "вид бланка", []InputBlock{ib("memo", `{"kind":"telegram","body":["т"]}`)}, "blocks[0].data.kind", "memo, order, letter")
	mustFail(t, "бланк без текста", []InputBlock{ib("memo", `{"kind":"memo","body":[]}`)}, "blocks[0].data.body", "нужен хотя бы 1")
	mustFail(t, "пустой адресат", []InputBlock{ib("memo", `{"kind":"memo","to":[""],"body":["т"]}`)}, "blocks[0].data.to[0]", "не может быть пустым")

	// clipping
	mustFail(t, "вид вырезки", []InputBlock{ib("clipping", `{"kind":"leaflet","paragraphs":["т"]}`)}, "blocks[0].data.kind", "newspaper, handwritten, transcript")
	mustFail(t, "вырезка без абзацев", []InputBlock{ib("clipping", `{"kind":"newspaper"}`)}, "blocks[0].data.paragraphs", "нужен хотя бы 1")
	mustFail(t, "реплики у газеты", []InputBlock{ib("clipping", `{"kind":"newspaper","paragraphs":["т"],"lines":[{"text":"р"}]}`)}, "blocks[0].data.lines", "только у расшифровки")
	mustFail(t, "абзацы у расшифровки", []InputBlock{ib("clipping", `{"kind":"transcript","paragraphs":["т"],"lines":[{"text":"р"}]}`)}, "blocks[0].data.paragraphs", "вместо абзацев")
	mustFail(t, "расшифровка без реплик", []InputBlock{ib("clipping", `{"kind":"transcript"}`)}, "blocks[0].data.lines", "от 1 до 500")
	mustFail(t, "пустая реплика", []InputBlock{ib("clipping", `{"kind":"transcript","lines":[{"text":""}]}`)}, "blocks[0].data.lines[0].text", "не может быть пустым")

	// table
	mustFail(t, "таблица без столбцов", []InputBlock{ib("table", `{"columns":[],"rows":[]}`)}, "blocks[0].data.columns", "от 1 до 12")
	mustFail(t, "13 столбцов", []InputBlock{ib("table", `{"columns":["1","2","3","4","5","6","7","8","9","10","11","12","13"],"rows":[["x"]]}`)}, "blocks[0].data.columns", "от 1 до 12")
	mustFail(t, "таблица без строк", []InputBlock{ib("table", `{"columns":["А"],"rows":[]}`)}, "blocks[0].data.rows", "от 1 до 200")
	mustFail(t, "строка неверной длины", []InputBlock{ib("table", `{"columns":["А","Б"],"rows":[["1"]]}`)}, "blocks[0].data.rows[0]", "в строке 1 ячеек, а столбцов 2")
	mustFail(t, "пустое название столбца", []InputBlock{ib("table", `{"columns":[""],"rows":[["1"]]}`)}, "blocks[0].data.columns[0]", "не может быть пустым")
	mustNormalize(t, ib("table", `{"columns":["А","Б"],"rows":[["1",""]]}`)) // пустая ячейка допустима

	// doc_link
	mustFail(t, "ссылка не на шифр", []InputBlock{ib("doc_link", `{"code":"документик"}`)}, "blocks[0].data.code", "не шифр документа")
	mustFail(t, "пустая ссылка", []InputBlock{ib("doc_link", `{}`)}, "blocks[0].data.code", "не шифр документа")

	// divider, footnote, appendix
	mustFail(t, "вид разделителя", []InputBlock{ib("divider", `{"style":"wavy"}`)}, "blocks[0].data.style", "line, stars")
	mustFail(t, "сноска без метки", []InputBlock{ib("footnote", `{"text":"т"}`)}, "blocks[0].data.mark", "не может быть пустым")
	mustFail(t, "длинная метка", []InputBlock{ib("footnote", `{"mark":"123456789","text":"т"}`)}, "blocks[0].data.mark", "слишком длинное")
	mustFail(t, "приложение без названия", []InputBlock{ib("appendix", `{}`)}, "blocks[0].data.title", "не может быть пустым")

	// experiment_log
	mustFail(t, "журнал без записей", []InputBlock{ib("experiment_log", `{"entries":[]}`)}, "blocks[0].data.entries", "от 1 до 200")
	mustFail(t, "запись без текста", []InputBlock{ib("experiment_log", `{"entries":[{"date":"1979-03-14"}]}`)}, "blocks[0].data.entries[0].text", "не может быть пустым")
	mustFail(t, "пустой участник", []InputBlock{ib("experiment_log", `{"entries":[{"participants":[""],"text":"т"}]}`)}, "blocks[0].data.entries[0].participants[0]", "не может быть пустым")
}

func TestManyProblemsAreReportedTogether(t *testing.T) {
	_, probs := normalize(t,
		ib("heading", `{"depth":9,"text":""}`),
		ib("hologram", `{}`),
		ib("stamp", `{"text":"С","tone":"green"}`),
		InputBlock{ID: "Плохой", Type: "divider"},
	)
	paths := map[string]bool{}
	for _, p := range probs {
		paths[p.Path] = true
	}
	for _, want := range []string{"blocks[0].data.depth", "blocks[0].data.text", "blocks[1].type", "blocks[2].data.tone", "blocks[3].id"} {
		if !paths[want] {
			t.Errorf("нет замечания %s среди %v", want, probs)
		}
	}
}

func TestTooManyBlocks(t *testing.T) {
	in := make([]InputBlock, maxBlocks+1)
	for i := range in {
		in[i] = InputBlock{Type: "divider"}
	}
	mustFail(t, "501 блок", in, "blocks", "слишком много блоков")
	mustNormalize(t, in[:maxBlocks]...)
}

func TestLinkCodes(t *testing.T) {
	blocks := mustNormalize(t,
		ib("paragraph", `{"text":"т"}`),
		ib("doc_link", `{"code":"O-41"}`),
		ib("doc_link", `{"code":"приказ-1978-2"}`),
	)
	codes, err := LinkCodes(blocks)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(codes, ",") != "О-041,ПРИКАЗ-1978-02" {
		t.Errorf("шифры ссылок: %v", codes)
	}
}

func TestBlocksRoundTripThroughStorage(t *testing.T) {
	blocks := mustNormalize(t,
		ib("memo", `{"kind":"order","number":"№ 12","to":["Начальнику ОТД-2"],"body":["Приказываю…",[{"text":"секрет","level":3}]],"signature":"Начальник"}`),
		ib("table", `{"caption":"П.о. по датам","columns":["Дата","П.о."],"rows":[["1979-03","12"],["1979-04",[{"text":"40","level":5}]]]}`),
	)
	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatal(err)
	}
	var back []Block
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(RenderOrFail(t, blocks))
	b, _ := json.Marshal(RenderOrFail(t, back))
	if string(a) != string(b) {
		t.Errorf("после сохранения ответ изменился:\n%s\n%s", a, b)
	}
}

func RenderOrFail(t *testing.T, blocks []Block) []OutBlock {
	t.Helper()
	out, err := RenderBlocks(blocks, 0, MaxLevel, nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func intp(n int) *int { return &n }

package pdf

import (
	"context"
	"strings"
	"testing"

	"kupol/internal/documents"
	"kupol/internal/i18n"
)

// kindBlocks — по одному блоку каждого вида, как отдаёт их сервер читателю. Вид без отрисовки роняет тест.
var kindBlocks = map[string]documents.OutBlock{
	"heading":        {Type: "heading", Data: documents.OutHeading{Depth: 2, Text: "Заголовок дела"}},
	"paragraph":      {Type: "paragraph", Data: documents.OutParagraph{Text: []documents.OutRun{{Text: "Абзац с "}, {Text: "жирным", Bold: true}}}},
	"list":           {Type: "list", Data: documents.OutList{Ordered: true, Items: [][]documents.OutRun{{{Text: "первый"}}, {{Text: "второй", Italic: true}}}}},
	"quote":          {Type: "quote", Data: documents.OutQuote{Text: []documents.OutRun{{Text: "Цитата"}}, Source: "Протокол-77"}},
	"dossier_header": {Type: "dossier_header", Data: struct{}{}},
	"experiment_log": {Type: "experiment_log", Data: documents.OutExperimentLog{
		Title: "Протокол",
		Entries: []documents.OutLogEntry{
			{Date: "12.05.1978", Participants: []string{"Иванов", "Петров"}, Text: []documents.OutRun{{Text: "Наблюдение"}}},
		},
	}},
	"stamp": {Type: "stamp", Data: documents.OutStamp{Text: "ИЗЪЯТО", Tone: "red", Tilt: -8}},
	"memo": {Type: "memo", Data: documents.OutMemo{
		Kind: "memo", Number: "5", Date: "1 сентября 1978", From: "Отдел ОБ", To: []string{"Лаборатория"},
		Subject: "О работах", Body: [][]documents.OutRun{{{Text: "Текст записки."}}}, Signature: "Начальник",
	}},
	"clipping": {Type: "clipping", Data: documents.OutClipping{
		Kind: "newspaper", Title: "Тишина в квартале", Source: "Вечерний вестник", Date: "3 января 1979",
		Paragraphs: [][]documents.OutRun{{{Text: "Текст вырезки."}}},
	}},
	"table": {Type: "table", Data: documents.OutTable{
		Caption: "Сводка", Columns: []string{"Признак", "Значение"},
		Rows: [][][]documents.OutRun{{{{Text: "Уровень"}}, {{Text: "3"}}}, {{{Text: "Масса"}}, {{Text: "0,4"}}}},
	}},
	"doc_link": {Type: "doc_link", Data: documents.OutDocLink{
		Available: true, Code: "О-042", Slug: "O-042", Title: "Соседнее дело", TypeName: "Объект", Note: "см. также",
	}},
	"divider":  {Type: "divider", Data: documents.OutDivider{Style: "line"}},
	"page":     {Type: "page", Data: documents.OutPage{Number: "3"}},
	"footnote": {Type: "footnote", Data: documents.OutFootnote{Mark: "1", Text: []documents.OutRun{{Text: "Сноска"}}}},
	"appendix": {Type: "appendix", Data: documents.OutAppendix{Number: "А", Title: "Чертёж"}},
	"containment_procedure": {Type: "containment_procedure", Data: documents.OutContainmentProcedure{
		Title: "Порядок", Steps: [][]documents.OutRun{{{Text: "Шаг первый"}}, {{Text: "Шаг второй"}}},
	}},
	"directive": {Type: "directive", Data: documents.OutDirective{
		Recipient: "Отдел ОБ", Order: []documents.OutRun{{Text: "Предписание"}}, Deadline: "до 1 мая",
	}},
	"incident_timeline": {Type: "incident_timeline", Data: documents.OutIncidentTimeline{
		Entries: []documents.OutTimelineEntry{
			{Time: "03:14", Event: []documents.OutRun{{Text: "Событие"}}},
			{Time: "04:20", Event: []documents.OutRun{{Text: "Ещё событие", Bold: true}}},
		},
	}},
	"personnel_record": {Type: "personnel_record", Data: documents.OutPersonnelRecord{
		Rank: "Старшина", Clearance: "3", Status: "active",
	}},
	"roster": {Type: "roster", Data: documents.OutRoster{
		Entries: []documents.OutRosterEntry{{Name: "Иванов И. И.", Position: "Наблюдатель"}, {Name: "Петров П. П."}},
	}},
	"hypothesis": {Type: "hypothesis", Data: documents.OutHypothesis{
		Text: []documents.OutRun{{Text: "Гипотеза о явлениях"}}, Result: []documents.OutRun{{Text: "Проверено частично"}},
	}},
	"qa": {Type: "qa", Data: documents.OutQA{
		Entries: []documents.OutQAEntry{
			{Question: []documents.OutRun{{Text: "Что случилось?"}}, Answer: []documents.OutRun{{Text: "Неизвестно."}}},
		},
	}},
	"routing": {Type: "routing", Data: documents.OutRouting{
		Entries: []documents.OutRoutingEntry{{Who: "Архивариус", Decision: "approved"}, {Who: "Директорат", Decision: "pending"}},
	}},
	"image": {Type: "image", Data: documents.OutImage{
		Upload: "0123456789abcdef0123456789abcdef", Caption: "Снимок места", Sticker: "frame",
	}},
	"audio": {Type: "audio", Data: documents.OutAudio{
		Upload: "0123456789abcdef0123456789abcdef", Title: "Запись разговора",
		Transcript: []documents.OutRun{{Text: "— Слышно?"}},
	}},
}

func newSheet(t *testing.T) (*sheet, *documents.OutDocument) {
	t.Helper()
	doc := &documents.OutDocument{
		Code: "О-041", Slug: "O-041", Type: "object", TypeName: "Объект",
		Title: "Дело о тишине", Grif: "Секретно", Level: 3,
		Composed: documents.Composed{Year: 1978}, CopyNumber: "0042",
		Status: "published", Author: strPtr("архивариус"),
	}
	blocks := make([]documents.OutBlock, 0, len(kindBlocks)+2)
	for _, kind := range documents.KindNames() {
		blk, ok := kindBlocks[kind]
		if !ok {
			t.Errorf("в тесте нет блока вида %q — вид без отрисовки в PDF роняет тест", kind)
			continue
		}
		blocks = append(blocks, blk)
	}
	// закрытый блок и закрытый фрагмент — «чёрные плашки»
	blocks = append(blocks, documents.OutBlock{Type: "redacted", Data: map[string]any{"level": 5}})
	blocks = append(blocks, documents.OutBlock{Type: "paragraph", Data: documents.OutParagraph{
		Text: []documents.OutRun{{Text: "Видимая часть "}, {Redacted: true, Level: 5}, {Text: " и снова видимая"}},
	}})
	doc.Blocks = blocks
	return &sheet{doc: doc, lang: i18n.RU, origin: "https://kupol.test", level: 7, images: map[string]string{}}, doc
}

func requireTypst(t *testing.T) *Runner {
	t.Helper()
	r, err := DefaultRunner()
	if err != nil {
		t.Skipf("пропуск: %v", err)
	}
	return r
}

// TestRenderEveryKind — каждый вид блока собирается и компилируется в PDF.
func TestRenderEveryKind(t *testing.T) {
	r := requireTypst(t)
	s, doc := newSheet(t)
	body, err := s.blocks()
	if err != nil {
		t.Fatalf("разметка блоков: %v", err)
	}
	if len(doc.Blocks) != len(documents.KindNames())+2 {
		t.Fatalf("блоков %d, ожидалось %d", len(doc.Blocks), len(documents.KindNames())+2)
	}
	opts := Options{Lang: i18n.RU, SiteOrigin: "https://kupol.test", ViewerLevel: 7}
	pdf, err := r.Render(context.Background(), doc, opts)
	if err != nil {
		t.Fatalf("сборка PDF: %v\nразметка:\n%s", err, body)
	}
	if string(pdf[:4]) != "%PDF" {
		t.Fatalf("не PDF: %.20q", pdf)
	}
	if len(pdf) < 8000 {
		t.Errorf("PDF подозрительно мал: %d байт", len(pdf))
	}
}

// TestRenderEveryKindItalian — итальянские подписи тоже собираются.
func TestRenderEveryKindItalian(t *testing.T) {
	r := requireTypst(t)
	_, doc := newSheet(t)
	opts := Options{Lang: i18n.IT, SiteOrigin: "https://kupol.test", ViewerLevel: 7}
	if _, err := r.Render(context.Background(), doc, opts); err != nil {
		t.Fatalf("сборка PDF (IT): %v", err)
	}
}

// TestRenderAdversarialText — текст документа не должен ломать разметку Typst.
func TestRenderAdversarialText(t *testing.T) {
	r := requireTypst(t)
	doc := adversarialDoc()
	opts := Options{Lang: i18n.RU, SiteOrigin: "https://kupol.test/?q=1", ViewerLevel: 7}
	if _, err := r.Render(context.Background(), doc, opts); err != nil {
		t.Fatalf("пытка разметкой: %v", err)
	}
}

// adversarialDoc — документ, набранный текстом, который обязан прийти на страницу буквами.
func adversarialDoc() *documents.OutDocument {
	junk := strings.Join([]string{
		"знаки: #let x = 1 $math$ *жирный* _курсив_ `код` [скобки] <метка> @ссылка ~ тильда",
		"\\ обратный слеш \" кавычки ' апостроф",
		"- пункт списка в начале строки",
		"+ нумерованный пункт",
		"= заголовок в строке",
		"/ комментарий в строке",
		"продолжение строки после пробела - и снова -",
		"1. не список",
		"конец",
	}, "\n")
	multi := "Первая строка\nвторая строка\n\nВторой абзац."
	doc := &documents.OutDocument{
		Code: "О-999", Slug: "O-999", Type: "order", TypeName: "Приказ",
		Title: junk, Grif: "Особой важности", Level: 7,
		Composed: documents.Composed{Year: 1980},
		Blocks: []documents.OutBlock{
			{Type: "paragraph", Data: documents.OutParagraph{Text: []documents.OutRun{{Text: junk}}}},
			{Type: "paragraph", Data: documents.OutParagraph{Text: []documents.OutRun{{Text: multi, Bold: true}}}},
			{Type: "heading", Data: documents.OutHeading{Depth: 1, Text: junk}},
			{Type: "list", Data: documents.OutList{Items: [][]documents.OutRun{{{Text: junk}}, {{Text: "обычный"}}}}},
			{Type: "quote", Data: documents.OutQuote{Text: []documents.OutRun{{Text: junk}}, Source: junk}},
			{Type: "table", Data: documents.OutTable{Caption: junk, Columns: []string{stuff(junk)}, Rows: [][][]documents.OutRun{{{{Text: junk}}, {{Text: junk}}}}}},
			{Type: "memo", Data: documents.OutMemo{Kind: "order", Number: junk, From: junk, Subject: junk, Body: [][]documents.OutRun{{{Text: junk}}}, Signature: junk}},
			{Type: "stamp", Data: documents.OutStamp{Text: junk, Tone: "red", Tilt: -3}},
			{Type: "doc_link", Data: documents.OutDocLink{Available: true, Code: junk, Slug: "x", Title: junk, TypeName: junk, Note: junk}},
			{Type: "redacted", Data: map[string]any{"level": 4}},
			{Type: "footnote", Data: documents.OutFootnote{Mark: junk, Text: []documents.OutRun{{Text: junk}}}},
			{Type: "appendix", Data: documents.OutAppendix{Number: junk, Title: junk}},
			{Type: "page", Data: documents.OutPage{Number: junk}},
		},
	}
	return doc
}

func stuff(s string) string { return s }

func strPtr(s string) *string { return &s }

// TestEscape — единичные проверки экранирования (без typst).
func TestEscape(t *testing.T) {
	cases := map[string]string{
		"обычный текст":    "обычный текст",
		"#directive":       `\#directive`,
		"*жирный*":         `\*жирный\*`,
		"конец-строки":     "конец-строки",
		"- начало":         `\- начало`,
		"строка\n- внутри": "строка\\\n\\- внутри",
		"двойной\n\nабзац": "двойной\n\nабзац",
		"tab\there":        "tab\there",
	}
	for in, want := range cases {
		if got := escape(in); got != want {
			t.Errorf("escape(%q) = %q, ожидалось %q", in, got, want)
		}
	}
	if got := escapeString(`путь \" и \`); strings.Contains(got, `"и`) {
		t.Errorf("escapeString пропустил кавычку: %q", got)
	}
}

// TestRedactionWidth — ширина полосы живёт в пределах как на сайте и не зависит от закрытого текста.
func TestRedactionWidth(t *testing.T) {
	seen := map[float64]bool{}
	for i := 0; i < 50; i++ {
		w := redactionWidth("О-041|" + itoa(i) + "|0|5")
		if w < redactionMin || w > redactionMax {
			t.Errorf("ширина %v вне %.1f–%.1f", w, redactionMin, redactionMax)
		}
		if w != redactionWidth("О-041|"+itoa(i)+"|0|5") {
			t.Errorf("ширина для одного места меняется")
		}
		seen[w] = true
	}
	if len(seen) < 5 {
		t.Errorf("ширины почти не различаются: %d разных", len(seen))
	}
	// то же место — та же ширина; другое место — обычно другая
	if redactionWidth("a|1|10|3") == redactionWidth("b|2|99|3") && redactionWidth("a|1|10|3") == redactionWidth("a|2|10|3") {
		t.Errorf("ширина не реагирует на положение")
	}
}

// TestArchiveRealism — гриф, архивный шифр, листы и экземпляр считаются как на сайте (§7).
func TestArchiveRealism(t *testing.T) {
	if got := classification(i18n.RU, 3); got != "Секретно" {
		t.Errorf("классификация 3: %q", got)
	}
	if got := classification(i18n.RU, 99); got != "Особой важности · только Директорат" {
		t.Errorf("классификация за шкалой: %q", got)
	}
	doc := &documents.OutDocument{Code: "О-041", Type: "object", Composed: documents.Composed{Year: 1978}}
	got := archiveMark(i18n.RU, doc)
	want := "Фонд 1 · Опись 1978 · Дело 41 · Листов 1"
	if got != want {
		t.Errorf("архивный шифр: %q, ожидалось %q", got, want)
	}
	if c := copyText(i18n.RU, ""); c != "экз. б/н" {
		t.Errorf("без экземпляра: %q", c)
	}
	if c := copyText(i18n.RU, "0042"); c != "экз. № 0042" {
		t.Errorf("с экземпляром: %q", c)
	}
	// разрывы страниц и закрытые блоки уходят в счёт листов
	blocks := []documents.OutBlock{{Type: "page"}, {Type: "redacted", Data: map[string]any{"level": 2}}}
	if n := sheetCount(blocks); n != 2 {
		t.Errorf("листов с разрывом и плашкой: %d, ожидалось 2", n)
	}
	month := 9
	day := 14
	if d := composedText(i18n.RU, documents.Composed{Year: 1978, Month: &month, Day: &day}); d != "14 сентября 1978 г." {
		t.Errorf("дата: %q", d)
	}
	if d := composedText(i18n.RU, documents.Composed{Year: 1978}); d != "1978 г." {
		t.Errorf("год: %q", d)
	}
	if d := composedText(i18n.IT, documents.Composed{Year: 1978, Month: &month, Day: &day}); d != "14 settembre 1978" {
		t.Errorf("дата (IT): %q", d)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

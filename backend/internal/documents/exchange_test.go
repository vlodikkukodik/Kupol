package documents

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"kupol/internal/i18n"
)

// Образец из 15 видов блоков — тот же, что у сквозных тестов редактора.
func fixtureBlocks(t *testing.T) []InputBlock {
	t.Helper()
	raw, err := os.ReadFile("../../../frontend/e2e/fixtures/editor-blocks.json")
	if err != nil {
		t.Fatal(err)
	}
	var blocks []InputBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatal(err)
	}
	return blocks
}

// newKindFixtures — образцы восьми видов блоков, по одному на тип дела (containment_procedure … routing).
// В общий файл редактора (fixtureBlocks) не идут нарочно: их не редактирует визуальный редактор (Tiptap
// хранит их как узел-«коробку»), поэтому у e2e-теста редактора свой отдельный счёт «15 видов».
func newKindFixtures(t *testing.T) []InputBlock {
	t.Helper()
	raw := `[
		{"id":"cp1","type":"containment_procedure","data":{"title":"Порядок","steps":["Шаг один.","Шаг два."]}},
		{"id":"dir1","type":"directive","data":{"recipient":"ОТД-2","order":"Провести проверку.","deadline":"до 01.05"}},
		{"id":"tl1","type":"incident_timeline","data":{"entries":[{"time":"14:02","event":"Событие."}]}},
		{"id":"pr1","type":"personnel_record","data":{"rank":"Куратор","clearance":"Уровень 4","status":"active"}},
		{"id":"ro1","type":"roster","data":{"entries":[{"name":"И. Петров","position":"Начальник"}]}},
		{"id":"hy1","type":"hypothesis","data":{"text":"Гипотеза.","result":"Результат.","confirmed":true}},
		{"id":"qa1","type":"qa","data":{"entries":[{"question":"Вопрос?","answer":"Ответ."}]}},
		{"id":"rt1","type":"routing","data":{"entries":[{"who":"Начальник","decision":"approved"}]}},
		{"id":"im1","type":"image","data":{"upload":"0123456789abcdef0123456789abcdef","caption":"Схема","sticker":"clip"}},
		{"id":"au1","type":"audio","data":{"upload":"fedcba9876543210fedcba9876543210","title":"Запись","transcript":"Текст записи."}}
	]`
	var blocks []InputBlock
	if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
		t.Fatal(err)
	}
	return blocks
}

// allKindFixtures — все виды блоков (15 редакторских + 8 новых по типам дела) для тестов, которые сверяются
// с полным реестром `kinds`.
func allKindFixtures(t *testing.T) []InputBlock {
	return append(fixtureBlocks(t), newKindFixtures(t)...)
}

func (e *env) export(a Actor, id int64, f ExportFormat) *ExportFile {
	e.t.Helper()
	file, err := e.svc.TeamExport(ctx, a, id, f)
	if err != nil {
		e.t.Fatalf("TeamExport(%s): %v", f, err)
	}
	return file
}

func (e *env) docCount() int64 { return e.count("SELECT count(*) FROM documents") }

func TestExportJSONIsLoadableAndRoundTrips(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	lvl := 3
	blocks := fixtureBlocks(t)
	blocks[1].Level = &lvl
	src, err := e.svc.TeamCreate(ctx, a.owner, CreateInput{Type: "object", Content: Content{
		Title: "Объект для круга", Level: &lvl, DirectLink: "forbidden", Grif: "КУПОЛ · СЛУЖЕБНОЕ", Composed: &Composed{Year: 1979},
		Props:  &Props{DangerClass: intp(3), DeviationPoints: intp(42), Category: "entity", ContainmentStatus: "contained", DiscoveryPlace: "Насосная станция"},
		Blocks: blocks,
	}})
	if err != nil {
		t.Fatal(err)
	}

	file := e.export(a.owner, src.ID, ExportJSON)
	if file.Filename != "object-"+itoa(int(src.ID))+".json" || !strings.HasPrefix(file.ContentType, "application/json") {
		t.Errorf("файл: %q %q", file.Filename, file.ContentType)
	}
	// файл разбирается строго — тем же разбором, что и загрузка командой
	if _, _, err := ParseInputs(file.Data); err != nil {
		t.Fatalf("экспорт не проходит разбор загрузки: %v", err)
	}

	// проверка (dry run) ничего не создаёт и сообщает, что будет
	before := e.docCount()
	res, err := e.svc.TeamImport(ctx, a.other, file.Data, true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Document != nil || res.Title != "Объект для круга" || res.TypeName != "Объект" || res.Blocks != len(blocks) || res.StatusIgnored != "" {
		t.Errorf("итог проверки: %+v", res)
	}
	if e.docCount() != before {
		t.Fatal("проверка файла создала документ")
	}

	// настоящая загрузка: черновик за тем, кто загрузил; содержимое побитово то же
	res, err = e.svc.TeamImport(ctx, a.other, file.Data, false)
	if err != nil {
		t.Fatal(err)
	}
	got := res.Document
	if got == nil || got.ID == src.ID || got.Status != "draft" || got.Author == nil || *got.Author != "other" || got.Revision != 1 {
		t.Fatalf("загруженный документ: %+v", got)
	}
	want, _ := src.Content.canonical()
	have, _ := got.Content.canonical()
	if !bytes.Equal(want, have) {
		t.Errorf("содержимое изменилось после круга экспорт → импорт:\n было %s\n стало %s", want, have)
	}
	// в истории — обычный снимок создания
	if vs := e.versions(got.ID); len(vs) != 1 || VersionKind(vs[0].Kind) != VersionCreate {
		t.Errorf("история загруженного: %v", kindsOf(vs))
	}
}

func TestImportAlwaysCreatesDraftWhateverTheFileSays(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	for status, number := range map[string]string{"published": "71", "review": "72", "archived": "73"} {
		raw := `{"type":"memo","code":"МЕМО-` + number + `","title":"Из файла","status":"` + status + `","composed":{"year":1979},"blocks":[{"id":"b1","type":"paragraph","data":{"text":"Текст."}}]}`
		res, err := e.svc.TeamImport(ctx, a.owner, []byte(raw), false)
		if err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		if res.StatusIgnored != status || res.Document.Status != "draft" {
			t.Errorf("статус «%s» из файла: итог %q, документ %q — рецензию обходить нельзя", status, res.StatusIgnored, res.Document.Status)
		}
		if e.mustGetStatus(res.Document.ID) != "draft" {
			t.Errorf("в базе статус не черновик")
		}
		var published *string
		if err := e.db.Raw("SELECT published_at::text FROM documents WHERE id = ?", res.Document.ID).Scan(&published).Error; err != nil || published != nil {
			t.Errorf("у загруженного есть дата публикации: %v %v", published, err)
		}
	}
	// без статуса и с «draft» — ничего не «игнорируется»
	res, err := e.svc.TeamImport(ctx, a.owner, []byte(`{"type":"memo","code":"МЕМО-74","title":"Без статуса","composed":{"year":1979},"blocks":[]}`), false)
	if err != nil || res.StatusIgnored != "" {
		t.Errorf("без статуса: %v %+v", err, res)
	}
}

func TestImportRightsAndFailuresLeaveNoTrace(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	good := `{"type":"memo","code":"МЕМО-81","title":"Ладно","composed":{"year":1979},"blocks":[]}`

	// без права писать — нельзя ни загрузить, ни проверить
	for name, who := range map[string]Actor{"модератор": a.moderator, "посторонний": a.plain} {
		for _, dry := range []bool{true, false} {
			if _, err := e.svc.TeamImport(ctx, who, []byte(good), dry); !errors.Is(err, ErrForbidden) {
				t.Errorf("%s (dry=%v): %v", name, dry, err)
			}
		}
	}
	if _, err := e.svc.TeamImport(ctx, a.director, []byte(good), false); err != nil { // Директорат может
		t.Errorf("Директорат: %v", err)
	}

	before := e.docCount()
	cases := map[string]struct{ raw, path string }{
		"не JSON":          {`{"type":`, "$"},
		"пусто":            {"  ", "$"},
		"неизвестное поле": {`{"type":"memo","code":"МЕМО-82","titel":"опечатка","composed":{"year":1979},"blocks":[]}`, "$"},
		"пакет":            {`{"documents":[` + good + `]}`, "$"},
		"нет названия":     {`{"type":"memo","code":"МЕМО-83","title":"","composed":{"year":1979},"blocks":[]}`, "title"},
		"плохой тип":       {`{"type":"nope","code":"МЕМО-84","title":"Х","composed":{"year":1979},"blocks":[]}`, "type"},
		"плохой блок":      {`{"type":"memo","code":"МЕМО-85","title":"Х","composed":{"year":1979},"blocks":[{"id":"b1","type":"heading","data":{"depth":9,"text":"Х"}}]}`, "blocks[0]"},
		"нет шифра у меморандума": {`{"type":"memo","title":"Х","composed":{"year":1979},"blocks":[]}`, "code"},
	}
	for name, c := range cases {
		for _, dry := range []bool{true, false} {
			_, err := e.svc.TeamImport(ctx, a.owner, []byte(c.raw), dry)
			var ve *ValidationError
			if !errors.As(err, &ve) || len(ve.Problems) == 0 {
				t.Errorf("%s (dry=%v): ожидались замечания, получено %v", name, dry, err)
				continue
			}
			if c.path != "$" && !strings.Contains(ve.Problems[0].Path, c.path) {
				t.Errorf("%s: путь замечания %q, ожидалось «%s»", name, ve.Problems[0].Path, c.path)
			}
		}
	}
	if e.docCount() != before {
		t.Errorf("отклонённые файлы оставили документы: %d → %d", before, e.docCount())
	}

	// занятый шифр — и при проверке, и при загрузке; чужой документ не тронут
	if _, err := e.svc.TeamImport(ctx, a.owner, []byte(good), true); !errors.Is(err, ErrCodeTaken) {
		t.Errorf("проверка занятого шифра: %v", err)
	}
	if _, err := e.svc.TeamImport(ctx, a.owner, []byte(good), false); !errors.Is(err, ErrCodeTaken) {
		t.Errorf("загрузка занятого шифра: %v", err)
	}
}

func TestImportObjectWithoutCodeGetsNoNumberUntilPublished(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	res, err := e.svc.TeamImport(ctx, a.owner, []byte(`{"type":"object","title":"Безымянный","composed":{"year":1979},"blocks":[]}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != "" || res.Document.Code != nil {
		t.Errorf("у черновика объекта не должно быть номера: %q %v", res.Code, res.Document.Code)
	}
}

func TestExportAccessFollowsDocumentAccess(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Мой черновик")

	for name, who := range map[string]Actor{"Автор": a.owner, "Директорат": a.director} {
		for _, f := range []ExportFormat{ExportJSON, ExportMarkdown} {
			if _, err := e.svc.TeamExport(ctx, who, d.ID, f); err != nil {
				t.Errorf("%s (%s): %v", name, f, err)
			}
		}
	}
	// чужой черновик, не-член команды и несуществующий документ — одинаково «не найден»
	for name, who := range map[string]Actor{"другой Автор": a.other, "модератор": a.moderator, "посторонний": a.plain} {
		if _, err := e.svc.TeamExport(ctx, who, d.ID, ExportJSON); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.svc.TeamExport(ctx, a.owner, 999999, ExportJSON); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующий: %v", err)
	}
	var qe *QueryError
	if _, err := e.svc.TeamExport(ctx, a.owner, d.ID, "pdf"); !errors.As(err, &qe) || qe.Field != "format" {
		t.Errorf("неизвестный формат: %v", err)
	}
}

func TestMarkdownCoversEveryBlockKind(t *testing.T) {
	blocks := allKindFixtures(t)
	seen := map[string]bool{}
	for _, b := range blocks {
		seen[b.Type] = true
		md := renderBlockMarkdown(b, i18n.RU)
		if strings.Contains(md, "не переносится") || strings.TrimSpace(md) == "" {
			t.Errorf("блок «%s» не отрисован в Markdown: %q", b.Type, md)
		}
	}
	for kind := range kinds {
		if !seen[kind] {
			t.Errorf("в образце нет блока «%s»: тест не покрывает новый вид", kind)
		}
	}
	// неизвестный вид не теряется молча
	if md := renderBlockMarkdown(InputBlock{ID: "x", Type: "hologram", Data: json.RawMessage(`{}`)}, i18n.RU); !strings.Contains(md, "hologram") {
		t.Errorf("неизвестный вид: %q", md)
	}
}

func TestMarkdownContent(t *testing.T) {
	lvl := 3
	blk := func(id, typ, data string, level *int) InputBlock {
		return InputBlock{ID: id, Type: typ, Level: level, Data: json.RawMessage(data)}
	}
	md := renderMarkdown(Input{
		Code: "О-041", Type: "object", Title: "Объект *№1* [тест]", Status: "draft", Level: intp(2), Grif: "Форма КУПОЛ-1",
		Composed: &Composed{Year: 1979, Month: intp(3), Day: intp(14)},
		Props:    &Props{DangerClass: intp(3), Department: "ОТД-2", DiscoveryPlace: `Станция "Север"`},
		Blocks: []InputBlock{
			blk("h", "heading", `{"depth":1,"text":"Общие сведения"}`, nil),
			blk("p", "paragraph", `{"text":[{"text":"Обычный "},{"text":"жирный","bold":true},{"text":" "},{"text":"секрет","level":3},{"text":"\nвторая строка # не заголовок"}]}`, &lvl),
			blk("l", "list", `{"ordered":true,"items":[[{"text":"Один"}],[{"text":"Два"}]]}`, nil),
			blk("t", "table", `{"columns":["А","Б|В"],"rows":[[[{"text":"1"}],[{"text":"a|b"}]]]}`, nil),
			blk("d", "divider", `{"style":"line"}`, nil),
		},
	})
	for _, want := range []string{
		"---\ncode: \"О-041\"\ntype: \"object\"\nstatus: \"draft\"\nlevel: 2\n",
		"composed: \"1979-03-14\"", "danger_class: 3", `discovery_place: "Станция \"Север\""`, "department: \"ОТД-2\"",
		"# Объект \\*№1\\* \\[тест\\]",
		"<!-- допуск блока: 3 -->",
		"## Общие сведения",
		"Обычный **жирный** [секрет]{допуск=3}  \nвторая строка # не заголовок",
		"1. Один\n2. Два",
		"| А | Б\\|В |\n| --- | --- |\n| 1 | a\\|b |",
		"\n---\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("в Markdown нет %q:\n%s", want, md)
		}
	}
	// текст, похожий на разметку, не становится ею
	if got := mdEscape("# не заголовок"); got != `\# не заголовок` {
		t.Errorf("mdEscape: %q", got)
	}
	if got := mdEscape("- не список"); got != `\- не список` {
		t.Errorf("mdEscape: %q", got)
	}
}

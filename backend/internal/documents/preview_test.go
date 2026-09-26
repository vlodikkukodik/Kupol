package documents

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Предпросмотр «глазами уровня N»: главное — он не может разойтись с тем, что сервер отдаёт читателю на самом деле.

func rawBlock(id, typ, data string) InputBlock {
	return InputBlock{ID: id, Type: typ, Data: json.RawMessage(data)}
}

// secretContent — открытый текст и три вида закрытого: фрагмент (уровень 3), блок уровня 4 и блок уровня 6.
func secretContent(title string) Content {
	l4, l6 := 4, 6
	return Content{
		Title: title, Composed: &Composed{Year: 1979},
		Blocks: []InputBlock{
			rawBlock("open", "paragraph", `{"text":[{"text":"Открыто. "},{"text":"СЕКРЕТ-ФРАГМЕНТ-3","level":3},{"text":" Конец."}]}`),
			{ID: "b4", Type: "paragraph", Level: &l4, Data: json.RawMessage(`{"text":"СЕКРЕТ-БЛОК-4"}`)},
			{ID: "b6", Type: "paragraph", Level: &l6, Data: json.RawMessage(`{"text":"СЕКРЕТ-БЛОК-6"}`)},
			rawBlock("tail", "paragraph", `{"text":"Заключение."}`),
		},
	}
}

func (e *env) previewDoc(a Actor, c Content) *TeamDocument {
	e.t.Helper()
	codeSeq++
	d, err := e.svc.TeamCreate(ctx, a, CreateInput{Type: "memo", Code: fmt.Sprintf("МЕМО-%d", codeSeq), Content: c})
	if err != nil {
		e.t.Fatalf("TeamCreate: %v", err)
	}
	return d
}

func (e *env) preview(a Actor, id int64, level int, c *Content) *PreviewResult {
	e.t.Helper()
	res, err := e.svc.TeamPreview(ctx, a, id, level, c)
	if err != nil {
		e.t.Fatalf("TeamPreview(уровень %d): %v", level, err)
	}
	return res
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPreviewFiltersByLevelOnTheServer(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.previewDoc(a.owner, secretContent("Секреты"))

	// уровень → какие секреты уже видны
	want := map[int][3]bool{ // фрагмент-3, блок-4, блок-6
		0: {false, false, false}, 1: {false, false, false}, 2: {false, false, false},
		3: {true, false, false}, 4: {true, true, false}, 5: {true, true, false},
		6: {true, true, true}, 7: {true, true, true},
	}
	for level := 0; level <= 7; level++ {
		res := e.preview(a.owner, d.ID, level, nil)
		if res.Access != PreviewOpen || res.Document == nil {
			t.Fatalf("уровень %d: документ не открыт: %+v", level, res)
		}
		body := asJSON(t, res.Document.Blocks)
		got := [3]bool{strings.Contains(body, "СЕКРЕТ-ФРАГМЕНТ-3"), strings.Contains(body, "СЕКРЕТ-БЛОК-4"), strings.Contains(body, "СЕКРЕТ-БЛОК-6")}
		if got != want[level] {
			t.Errorf("уровень %d: видны секреты %v, ожидалось %v\n%s", level, got, want[level], body)
		}
		if !strings.Contains(body, "Открыто.") || !strings.Contains(body, "Заключение.") {
			t.Errorf("уровень %d: открытый текст пропал: %s", level, body)
		}
		if level < 6 && !strings.Contains(body, `"type":"redacted"`) {
			t.Errorf("уровень %d: закрытому блоку нет метки: %s", level, body)
		}
	}
}

// Предпросмотр и обычное чтение читателем — один и тот же ответ: расхождение означало бы, что автор проверяет не то.
func TestPreviewEqualsWhatTheReaderGets(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.previewDoc(a.owner, secretContent("Как у читателя"))
	e.setStatus(d, StatusPublished)

	for level := 0; level <= 7; level++ {
		res := e.preview(a.owner, d.ID, level, nil)
		read, err := e.svc.Get(ctx, previewViewer(level, a.owner.UserID), *d.Code)
		if err != nil {
			t.Fatalf("уровень %d: Get: %v", level, err)
		}
		// «Упоминается в» и лист ознакомления — живые сведения о самом документе, в предпросмотре их нет: сравниваем то, что читатель видит в тексте
		read.MentionedIn, read.ReadCount = nil, 0
		if got, want := asJSON(t, res.Document), asJSON(t, read); got != want {
			t.Errorf("уровень %d: предпросмотр расходится с чтением\nпредпросмотр: %s\nчтение:       %s", level, got, want)
		}
	}
}

func TestPreviewUsesUnsavedContentAndSavesNothing(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.previewDoc(a.owner, memoContent("Сохранённое"))
	e.setStatus(d, StatusPublished)
	before := e.count("SELECT count(*) FROM document_versions")
	reads := e.count("SELECT count(*) FROM document_reads")

	edited := memoContent("Правка в редакторе", paragraph("b1", "Ещё не сохранено."))
	res := e.preview(a.owner, d.ID, 0, &edited)
	if res.Document.Title != "Правка в редакторе" || !strings.Contains(asJSON(t, res.Document.Blocks), "Ещё не сохранено.") {
		t.Fatalf("предпросмотр не по правкам редактора: %s", asJSON(t, res.Document))
	}

	stored, err := e.svc.TeamGet(ctx, a.owner, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Content.Title != "Сохранённое" || stored.Revision != d.Revision {
		t.Errorf("предпросмотр изменил документ: %q, редакция %d", stored.Content.Title, stored.Revision)
	}
	if n := e.count("SELECT count(*) FROM document_versions"); n != before {
		t.Errorf("предпросмотр создал снимки: было %d, стало %d", before, n)
	}
	if n := e.count("SELECT count(*) FROM document_reads"); n != reads {
		t.Errorf("предпросмотр записал «прочтение»: было %d, стало %d", reads, n)
	}
}

func TestPreviewToleratesUnfinishedDraft(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.previewDoc(a.owner, memoContent("Черновик"))

	draft := memoContent("Черновик",
		paragraph("ok", "Готовый абзац."),
		rawBlock("empty-stamp", "stamp", `{"text":""}`), // пустой штамп: сервер не примет
		rawBlock("bad-id!", "paragraph", `{"text":"Абзац с плохим идентификатором."}`),
	)
	res := e.preview(a.owner, d.ID, 0, &draft)
	if res.Access != PreviewOpen || res.Document == nil {
		t.Fatalf("неполный черновик закрыл предпросмотр: %+v", res)
	}
	body := asJSON(t, res.Document.Blocks)
	if !strings.Contains(body, "Готовый абзац.") {
		t.Errorf("готовый блок пропал: %s", body)
	}
	if strings.Contains(body, `"type":"stamp"`) {
		t.Errorf("блок с замечанием попал в предпросмотр: %s", body)
	}
	paths := []string{}
	for _, p := range res.Problems {
		paths = append(paths, p.Path)
	}
	if !strings.Contains(strings.Join(paths, " "), "blocks[1]") || !strings.Contains(strings.Join(paths, " "), "blocks[2].id") {
		t.Errorf("замечания не названы по блокам: %v", paths)
	}
}

func TestPreviewDocumentAccessMatchesReader(t *testing.T) {
	e := newEnv(t)
	a := e.actors()

	closed := memoContent("Закрытое дело", paragraph("b1", "Текст."))
	closed.Level = intp(3)
	hidden := e.previewDoc(a.owner, closed) // по умолчанию: «Дело не найдено»

	denied := closed
	denied.Title, denied.DirectLink = "С отказом", "forbidden"
	forbidden := e.previewDoc(a.owner, denied)

	for level := 0; level <= 7; level++ {
		res := e.preview(a.owner, hidden.ID, level, nil)
		wantAccess := PreviewNotFound
		if level >= 3 {
			wantAccess = PreviewOpen
		}
		if res.Access != wantAccess {
			t.Errorf("уровень %d, режим not_found: %s, ожидалось %s", level, res.Access, wantAccess)
		}
		if res.Access != PreviewOpen && (res.Document != nil || res.RequiredLevel != 3) {
			t.Errorf("уровень %d: закрытый документ раскрыл содержимое или не назвал уровень: %+v", level, res)
		}

		res = e.preview(a.owner, forbidden.ID, level, nil)
		wantAccess = PreviewDenied
		if level >= 3 {
			wantAccess = PreviewOpen
		}
		if res.Access != wantAccess {
			t.Errorf("уровень %d, режим forbidden: %s, ожидалось %s", level, res.Access, wantAccess)
		}
	}
}

func TestPreviewResolvesLinksAsThatReaderSees(t *testing.T) {
	e := newEnv(t)
	a := e.actors()

	secret := memoContent("Цель ссылки", paragraph("b1", "Закрытое."))
	secret.Level = intp(4)
	target := e.previewDoc(a.owner, secret)
	e.setStatus(target, StatusPublished)

	src := memoContent("Источник", rawBlock("link", "doc_link", fmt.Sprintf(`{"code":%q,"note":"СЕКРЕТ-ПРИМЕЧАНИЕ"}`, *target.Code)))
	d := e.previewDoc(a.owner, src)

	low := asJSON(t, e.preview(a.owner, d.ID, 2, nil).Document.Blocks)
	if strings.Contains(low, *target.Code) || strings.Contains(low, "Цель ссылки") || strings.Contains(low, "СЕКРЕТ-ПРИМЕЧАНИЕ") || !strings.Contains(low, `"available":false`) {
		t.Errorf("уровень 2 узнал о закрытом документе по ссылке: %s", low)
	}
	high := asJSON(t, e.preview(a.owner, d.ID, 4, nil).Document.Blocks)
	if !strings.Contains(high, "Цель ссылки") || !strings.Contains(high, `"available":true`) {
		t.Errorf("уровень 4 не увидел доступную цель ссылки: %s", high)
	}
}

func TestPreviewRights(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.previewDoc(a.owner, memoContent("Личный черновик"))

	for name, who := range map[string]Actor{"чужой автор": a.other, "редактор": a.editor, "без прав": a.plain} {
		if _, err := e.svc.TeamPreview(ctx, who, d.ID, 0, nil); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: чужой черновик должен быть «не найден», получено %v", name, err)
		}
	}
	// Директорат видит чужой черновик и может смотреть его любым уровнем
	if res := e.preview(a.director, d.ID, 0, nil); res.Document == nil {
		t.Error("Директорат не увидел предпросмотр чужого черновика")
	}
	// владелец смотрит, даже если править не вправе (опубликованное)
	e.setStatus(d, StatusPublished)
	if res := e.preview(a.owner, d.ID, 0, nil); res.Document == nil {
		t.Error("владелец не увидел предпросмотр опубликованного документа")
	}

	for _, bad := range []int{-1, 8, 100} {
		var qe *QueryError
		if _, err := e.svc.TeamPreview(ctx, a.owner, d.ID, bad, nil); !errors.As(err, &qe) || qe.Field != "level" {
			t.Errorf("уровень %d: ожидалась ошибка параметра level, получено %v", bad, err)
		}
	}
	if _, err := e.svc.TeamPreview(ctx, a.owner, 999999, 0, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующий документ: %v", err)
	}
}

package documents

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------- помощники

type actors struct {
	owner     Actor // Автор: пишет, владелец документов
	other     Actor // другой Автор
	editor    Actor // Редактор: проверяет и правит опубликованное
	director  Actor // Директорат
	moderator Actor // в команде, но без прав на документы
	plain     Actor // обычный пользователь
}

func (e *env) actors() actors {
	mk := func(login string, dir, write, review, edit bool) Actor {
		return Actor{UserID: e.user(login), Login: login, Directorate: dir, CanWrite: write, CanReview: review, CanEditPublished: edit}
	}
	return actors{
		owner:     mk("owner", false, true, false, false),
		other:     mk("other", false, true, false, false),
		editor:    mk("editor", false, true, true, true),
		director:  mk("director", true, true, true, true),
		moderator: mk("moderator", false, false, false, false),
		plain:     mk("plain", false, false, false, false),
	}
}

func paragraph(id, text string) InputBlock {
	raw, _ := json.Marshal(map[string]any{"text": text})
	return InputBlock{ID: id, Type: "paragraph", Data: raw}
}

func memoContent(title string, blocks ...InputBlock) Content {
	if blocks == nil {
		blocks = []InputBlock{paragraph("b1", "Первый абзац.")}
	}
	return Content{Title: title, Composed: &Composed{Year: 1979}, Blocks: blocks}
}

var codeSeq int

// draft создаёт черновик-меморандум от имени автора.
func (e *env) draft(a Actor, title string) *TeamDocument {
	e.t.Helper()
	codeSeq++
	d, err := e.svc.TeamCreate(ctx, a, CreateInput{Type: "memo", Code: fmt.Sprintf("МЕМО-%d", codeSeq), Content: memoContent(title)})
	if err != nil {
		e.t.Fatalf("TeamCreate: %v", err)
	}
	return d
}

func (e *env) setStatus(d *TeamDocument, st Status) {
	e.t.Helper()
	if _, err := e.svc.SetStatus(ctx, *d.Code, st); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) versions(docID int64) []Version {
	e.t.Helper()
	var vs []Version
	if err := e.db.Where("document_id = ?", docID).Order("id").Find(&vs).Error; err != nil {
		e.t.Fatal(err)
	}
	return vs
}

func kindsOf(vs []Version) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Kind
	}
	return out
}

func mustSave(t *testing.T, e *env, a Actor, d *TeamDocument, c Content) *SaveResult {
	t.Helper()
	r, err := e.svc.TeamSave(ctx, a, d.ID, d.Revision, c)
	if err != nil {
		t.Fatalf("TeamSave: %v", err)
	}
	return r
}

// ---------------------------------------------------------------- права

// Ожидания записаны здесь, а не выведены из кода: изменение правил в Actor должно ломать тест.
func TestActorPermissionsMatrix(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	mine := func(u Actor) *int64 { id := u.UserID; return &id }

	type row struct {
		who        string
		actor      Actor
		status     Status
		owned      bool
		view, edit bool
	}
	rows := []row{
		// Автор: своё — всегда видит; правит черновик и «на проверке», опубликованное — нет
		{"владелец", a.owner, StatusDraft, true, true, true},
		{"владелец", a.owner, StatusReview, true, true, true},
		{"владелец", a.owner, StatusPublished, true, true, false},
		{"владелец", a.owner, StatusArchived, true, true, false},
		// чужой Автор ничего не видит
		{"чужой автор", a.other, StatusDraft, false, false, false},
		{"чужой автор", a.other, StatusReview, false, false, false},
		{"чужой автор", a.other, StatusPublished, false, false, false},
		// Редактор: чужой черновик — нет; всё остальное — видит и правит
		{"редактор", a.editor, StatusDraft, false, false, false},
		{"редактор", a.editor, StatusReview, false, true, true},
		{"редактор", a.editor, StatusPublished, false, true, true},
		{"редактор", a.editor, StatusArchived, false, true, true},
		// свой черновик Редактор правит как Автор (у него есть право писать)
		{"редактор-владелец", a.editor, StatusDraft, true, true, true},
		// Директорат — всё
		{"Директорат", a.director, StatusDraft, false, true, true},
		{"Директорат", a.director, StatusReview, false, true, true},
		{"Директорат", a.director, StatusPublished, false, true, true},
		{"Директорат", a.director, StatusArchived, false, true, true},
		// без прав: свой документ виден (он его автор), править нельзя; чужое не видно
		{"без прав, владелец", a.moderator, StatusDraft, true, true, false},
		{"без прав, чужое", a.moderator, StatusReview, false, false, false},
		{"без прав, чужое", a.plain, StatusPublished, false, false, false},
	}
	for _, r := range rows {
		d := &Document{Status: string(r.status)}
		if r.owned {
			d.AuthorID = mine(r.actor)
		} else {
			d.AuthorID = mine(a.owner)
			if r.actor.UserID == a.owner.UserID {
				d.AuthorID = mine(a.other)
			}
		}
		if got := r.actor.CanView(d); got != r.view {
			t.Errorf("%s, %s (свой=%v): CanView = %v, ожидалось %v", r.who, r.status, r.owned, got, r.view)
		}
		if got := r.actor.CanEdit(d); got != r.edit {
			t.Errorf("%s, %s (свой=%v): CanEdit = %v, ожидалось %v", r.who, r.status, r.owned, got, r.edit)
		}
	}
	// документ без автора (загружен командой): владельца нет, видят Директорат и Редактор (вне черновика)
	orphan := &Document{Status: string(StatusReview)}
	if a.owner.CanView(orphan) || !a.editor.CanView(orphan) || !a.director.CanView(orphan) {
		t.Error("документ без автора: видят только Редактор и Директорат")
	}
}

// Список и точечное чтение согласованы с предикатом CanView, а флаг can_edit — с CanEdit.
func TestTeamListAndGetAgreeWithPermissions(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	all := []Actor{a.owner, a.other, a.editor, a.director, a.moderator, a.plain}

	type made struct {
		id     int64
		author Actor
		status Status
	}
	var docs []made
	for _, author := range []Actor{a.owner, a.other, a.editor} {
		for _, st := range []Status{StatusDraft, StatusReview, StatusPublished, StatusArchived} {
			d := e.draft(author, fmt.Sprintf("%s/%s", author.Login, st))
			if st != StatusDraft {
				e.setStatus(d, st)
			}
			docs = append(docs, made{d.ID, author, st})
		}
	}

	for _, viewer := range all {
		list, err := e.svc.TeamList(ctx, viewer, TeamListQuery{PerPage: 100})
		if err != nil {
			t.Fatal(err)
		}
		got := map[int64]TeamItem{}
		for _, it := range list.Items {
			got[it.ID] = it
		}
		for _, m := range docs {
			doc := &Document{Status: string(m.status)}
			id := m.author.UserID
			doc.AuthorID = &id
			wantView, wantEdit := viewer.CanView(doc), viewer.CanEdit(doc)
			it, inList := got[m.id]
			if inList != wantView {
				t.Errorf("%s: документ %s/%s в списке = %v, ожидалось %v", viewer.Login, m.author.Login, m.status, inList, wantView)
			}
			if inList && it.CanEdit != wantEdit {
				t.Errorf("%s: can_edit для %s/%s = %v, ожидалось %v", viewer.Login, m.author.Login, m.status, it.CanEdit, wantEdit)
			}
			td, err := e.svc.TeamGet(ctx, viewer, m.id)
			switch {
			case wantView && err != nil:
				t.Errorf("%s: TeamGet %s/%s: %v", viewer.Login, m.author.Login, m.status, err)
			case !wantView && !errors.Is(err, ErrNotFound):
				t.Errorf("%s: TeamGet %s/%s должен давать ErrNotFound, получено %v", viewer.Login, m.author.Login, m.status, err)
			case wantView && td.CanEdit != wantEdit:
				t.Errorf("%s: TeamGet can_edit %s/%s = %v", viewer.Login, m.author.Login, m.status, td.CanEdit)
			}
		}
		if int(list.Total) != len(got) {
			t.Errorf("%s: total %d, в списке %d", viewer.Login, list.Total, len(got))
		}
	}
}

func TestTeamListFiltersAndBadParameters(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d1 := e.draft(a.owner, "Приказ об отчётности")
	d2 := e.draft(a.owner, "Инцидент 100%_уверен")
	e.setStatus(d2, StatusReview)
	e.draft(a.other, "Чужой")

	list := func(q TeamListQuery) []string {
		t.Helper()
		r, err := e.svc.TeamList(ctx, a.director, q)
		if err != nil {
			t.Fatalf("%+v: %v", q, err)
		}
		var titles []string
		for _, it := range r.Items {
			titles = append(titles, it.Title)
		}
		slices.Sort(titles)
		return titles
	}
	if got := list(TeamListQuery{Status: "review"}); !slices.Equal(got, []string{"Инцидент 100%_уверен"}) {
		t.Errorf("по статусу: %v", got)
	}
	if got := list(TeamListQuery{Query: "ОТЧЁТ"}); !slices.Equal(got, []string{"Приказ об отчётности"}) {
		t.Errorf("поиск без учёта регистра: %v", got)
	}
	// «%» и «_» в поиске — обычные символы
	if got := list(TeamListQuery{Query: "100%_"}); !slices.Equal(got, []string{"Инцидент 100%_уверен"}) {
		t.Errorf("поиск со спецсимволами: %v", got)
	}
	if got := list(TeamListQuery{Query: "%"}); len(got) != 1 {
		t.Errorf("«%%» не должен находить всё: %v", got)
	}
	if got := list(TeamListQuery{Query: *d1.Code}); len(got) != 1 {
		t.Errorf("поиск по шифру: %v", got)
	}
	r, _ := e.svc.TeamList(ctx, a.owner, TeamListQuery{Mine: true})
	if r.Total != 2 {
		t.Errorf("«мои» у владельца: %d", r.Total)
	}
	r, _ = e.svc.TeamList(ctx, a.director, TeamListQuery{Mine: true})
	if r.Total != 0 {
		t.Errorf("у Директората своих нет: %d", r.Total)
	}

	for name, q := range map[string]TeamListQuery{
		"status": {Status: "deleted"}, "type": {Type: "cat"}, "q": {Query: strings.Repeat("я", 101)},
		"page": {Page: -1}, "per_page": {PerPage: 101},
	} {
		_, err := e.svc.TeamList(ctx, a.director, q)
		var qe *QueryError
		if !errors.As(err, &qe) || qe.Field != name {
			t.Errorf("%s: ожидалась QueryError, получено %v", name, err)
		}
	}
	// страницы
	for i := 0; i < 5; i++ {
		e.draft(a.owner, fmt.Sprintf("Добавочный %d", i))
	}
	p1, _ := e.svc.TeamList(ctx, a.owner, TeamListQuery{PerPage: 3})
	p3, _ := e.svc.TeamList(ctx, a.owner, TeamListQuery{PerPage: 3, Page: 3})
	if p1.Total != 7 || p1.Pages != 3 || len(p1.Items) != 3 || len(p3.Items) != 1 {
		t.Errorf("страницы: %+v / %+v", p1, p3)
	}
}

// ---------------------------------------------------------------- создание

func TestCreateDraft(t *testing.T) {
	e := newEnv(t)
	a := e.actors()

	d := e.draft(a.owner, "Первый")
	if d.Status != "draft" || d.Revision != 1 || d.Author == nil || *d.Author != "owner" || !d.CanEdit || d.Lock != nil {
		t.Errorf("новый документ: %+v", d)
	}
	if vs := e.versions(d.ID); !slices.Equal(kindsOf(vs), []string{"create"}) || vs[0].Permanent != true || vs[0].Revision != 1 {
		t.Errorf("история после создания: %v", kindsOf(vs))
	}

	// читатели черновика не видят: ни по шифру, ни в каталоге, даже Директорат-читатель по обычному API — только как команда
	if _, err := e.svc.Get(ctx, viewer(6), *d.Code); !errors.Is(err, ErrNotFound) {
		t.Errorf("черновик виден читателю: %v", err)
	}

	// объект без шифра — можно: номер присвоится при публикации
	obj, err := e.svc.TeamCreate(ctx, a.owner, CreateInput{Type: "object", Content: memoContent("Безымянный")})
	if err != nil || obj.Code != nil || obj.Slug != nil {
		t.Fatalf("объект без шифра: %+v, %v", obj, err)
	}
	// ...а у остальных типов шифр обязателен
	_, err = e.svc.TeamCreate(ctx, a.owner, CreateInput{Type: "order", Content: memoContent("Без шифра")})
	if !hasPath(err, "code") {
		t.Errorf("приказ без шифра: %v", err)
	}
	// шифр другого типа, чужие свойства, пустое название, нет даты
	for name, in := range map[string]CreateInput{
		"code":     {Type: "order", Code: "ИНЦ-1982-07", Content: memoContent("x")},
		"props":    {Type: "memo", Code: "МЕМО-999", Content: Content{Title: "x", Composed: &Composed{Year: 1979}, Props: &Props{Category: "entity"}, Blocks: []InputBlock{}}},
		"title":    {Type: "memo", Code: "МЕМО-998", Content: memoContent("")},
		"composed": {Type: "memo", Code: "МЕМО-997", Content: Content{Title: "x", Blocks: []InputBlock{}}},
		"type":     {Type: "cat", Code: "МЕМО-996", Content: memoContent("x")},
	} {
		if _, err := e.svc.TeamCreate(ctx, a.owner, in); !hasPath(err, name) {
			t.Errorf("ожидалось замечание к %q: %v", name, err)
		}
	}
	// неверный блок — замечание с путём внутри блока
	bad := memoContent("x", InputBlock{ID: "b1", Type: "paragraph", Data: json.RawMessage(`{"text": 5}`)})
	if _, err := e.svc.TeamCreate(ctx, a.owner, CreateInput{Type: "memo", Code: "МЕМО-995", Content: bad}); err == nil {
		t.Error("блок с неверными данными должен отвергаться")
	}
	if n := e.count("SELECT count(*) FROM documents WHERE code IN ('МЕМО-999','МЕМО-998','МЕМО-997','МЕМО-996','МЕМО-995','ИНЦ-1982-07')"); n != 0 {
		t.Errorf("отвергнутые документы сохранились: %d", n)
	}

	// занятый шифр
	_, err = e.svc.TeamCreate(ctx, a.other, CreateInput{Type: "memo", Code: *d.Code, Content: memoContent("Дубль")})
	if !errors.Is(err, ErrCodeTaken) {
		t.Errorf("занятый шифр: %v", err)
	}
	// шифр в другой раскладке — тот же шифр
	_, err = e.svc.TeamCreate(ctx, a.other, CreateInput{Type: "memo", Code: strings.ToLower(strings.Replace(*d.Code, "МЕМО", "memo", 1)), Content: memoContent("Дубль")})
	if !errors.Is(err, ErrCodeTaken) {
		t.Errorf("шифр в другой раскладке: %v", err)
	}
	// без права писать
	for _, who := range []Actor{a.moderator, a.plain} {
		if _, err := e.svc.TeamCreate(ctx, who, CreateInput{Type: "memo", Code: "МЕМО-900", Content: memoContent("x")}); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s: создание без права: %v", who.Login, err)
		}
	}
}

// ---------------------------------------------------------------- сохранение

func TestSaveCreatesRevisionAndRollingSnapshot(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")

	e.advance(time.Minute)
	r := mustSave(t, e, a.owner, d, memoContent("Черновик, правка 1", paragraph("b1", "Новый текст"), paragraph("b2", "Ещё")))
	if !r.Changed || r.Document.Revision != 2 || r.Document.Content.Title != "Черновик, правка 1" || len(r.Document.Content.Blocks) != 2 {
		t.Fatalf("после сохранения: %+v", r.Document)
	}
	if !r.Document.UpdatedAt.After(d.UpdatedAt) {
		t.Error("updated_at не изменился")
	}
	vs := e.versions(d.ID)
	if !slices.Equal(kindsOf(vs), []string{"create", "save"}) || vs[1].Permanent || vs[1].Revision != 2 {
		t.Errorf("история: %v", kindsOf(vs))
	}
	if r.Document.Lock == nil || !r.Document.Lock.Mine {
		t.Error("сохранение должно взять документ в работу")
	}

	// то же содержимое — не изменение: редакция и история не растут
	same := mustSave(t, e, a.owner, r.Document, r.Document.Content)
	if same.Changed || same.Document.Revision != 2 || len(e.versions(d.ID)) != 2 {
		t.Errorf("сохранение без изменений: %+v, версий %d", same, len(e.versions(d.ID)))
	}
	// порядок ключей и пустые значения не в счёт: тот же смысл в другой записи
	c := r.Document.Content
	c.Level = nil
	c.Grif = ""
	if again := mustSave(t, e, a.owner, r.Document, c); again.Changed {
		t.Error("равнозначная запись содержимого создала редакцию")
	}
}

func TestSaveRejectsInvalidContentWithoutTrace(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")

	bad := memoContent("", InputBlock{ID: "b1", Type: "heading", Data: json.RawMessage(`{"depth": 9, "text": ""}`)})
	_, err := e.svc.TeamSave(ctx, a.owner, d.ID, d.Revision, bad)
	var ve *ValidationError
	if !errors.As(err, &ve) || !hasPath(err, "title") || !hasPath(err, "blocks[0].data.depth") {
		t.Fatalf("ожидались замечания к названию и блоку: %v", err)
	}
	after, _ := e.svc.TeamGet(ctx, a.owner, d.ID)
	if after.Revision != 1 || after.Content.Title != "Черновик" || len(e.versions(d.ID)) != 1 {
		t.Errorf("отклонённое сохранение оставило след: ревизия %d, версий %d", after.Revision, len(e.versions(d.ID)))
	}
	// проверка тех же правил, что у загрузки из файла
	for name, c := range map[string]Content{
		"level":         {Title: "x", Level: intp(9), Composed: &Composed{Year: 1979}, Blocks: []InputBlock{}},
		"direct_link":   {Title: "x", DirectLink: "maybe", Composed: &Composed{Year: 1979}, Blocks: []InputBlock{}},
		"composed.year": {Title: "x", Composed: &Composed{Year: 1800}, Blocks: []InputBlock{}},
	} {
		if _, err := e.svc.TeamSave(ctx, a.owner, d.ID, 1, c); !hasPath(err, name) {
			t.Errorf("ожидалось замечание к %q: %v", name, err)
		}
	}
	if _, err := e.svc.TeamSave(ctx, a.owner, d.ID, 1, Content{Title: "x", Composed: &Composed{Year: 1979}, Blocks: []InputBlock{{ID: "b1", Type: "image", Data: json.RawMessage(`{}`)}}}); !hasPath(err, "blocks[0].type") {
		t.Errorf("блок image должен отклоняться до этапа 6: %v", err)
	}
}

func TestSaveConflictAndPermissions(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")
	mustSave(t, e, a.owner, d, memoContent("Правка 1")) // редакция 2

	// редактор открыл редакцию 1, а документ уже изменён
	_, err := e.svc.TeamSave(ctx, a.owner, d.ID, 1, memoContent("Устаревшая правка"))
	var ce *ConflictError
	if !errors.As(err, &ce) || ce.CurrentRevision != 2 {
		t.Errorf("устаревшая редакция: %v", err)
	}
	// ...из будущего — тоже конфликт, а не запись
	if _, err = e.svc.TeamSave(ctx, a.owner, d.ID, 9, memoContent("Из будущего")); !errors.As(err, &ce) {
		t.Errorf("редакция из будущего: %v", err)
	}
	// чужой Автор: не видит — 404; Редактор чужой черновик тоже не видит
	for _, who := range []Actor{a.other, a.editor} {
		if _, err := e.svc.TeamSave(ctx, who, d.ID, 2, memoContent("Взлом")); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s чужой черновик: %v", who.Login, err)
		}
	}
	// без права писать, но владелец: видит, править нельзя
	e.db.Exec("UPDATE documents SET author_id = ? WHERE id = ?", a.moderator.UserID, d.ID)
	if _, err := e.svc.TeamSave(ctx, a.moderator, d.ID, 2, memoContent("Без права")); !errors.Is(err, ErrForbidden) {
		t.Errorf("владелец без права писать: %v", err)
	}
	if got, _ := e.svc.TeamGet(ctx, a.director, d.ID); got.Content.Title != "Правка 1" {
		t.Errorf("отклонённые правки изменили документ: %q", got.Content.Title)
	}
	if _, err := e.svc.TeamSave(ctx, a.owner, 999999, 1, memoContent("x")); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующий документ: %v", err)
	}
}

func TestEditPublishedInPlace(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Опубликованный")
	e.setStatus(d, StatusPublished)
	code := *d.Code
	cur, _ := e.svc.TeamGet(ctx, a.editor, d.ID)

	// Автор (владелец) опубликованное не правит
	if _, err := e.svc.TeamSave(ctx, a.owner, d.ID, cur.Revision, memoContent("Автор правит")); !errors.Is(err, ErrForbidden) {
		t.Errorf("автор правит опубликованное: %v", err)
	}
	// Редактор правит на месте: читатели видят новое сразу, в истории — вечный снимок «edit»
	e.advance(time.Minute)
	r := mustSave(t, e, a.editor, cur, memoContent("Опубликованный, исправлено", paragraph("b1", "Исправленный текст")))
	if !r.Changed || r.Document.Status != "published" {
		t.Fatalf("правка: %+v", r.Document)
	}
	seen, err := e.svc.Get(ctx, viewer(0), code)
	if err != nil || seen.Title != "Опубликованный, исправлено" {
		t.Fatalf("читатель после правки: %v %+v", err, seen)
	}
	vs := e.versions(d.ID)
	last := vs[len(vs)-1]
	if last.Kind != "edit" || !last.Permanent || last.AuthorID == nil || *last.AuthorID != a.editor.UserID {
		t.Errorf("снимок правки: %+v", last)
	}
	// Директорат тоже вправе; правка архивного — те же права
	e.setStatus(d, StatusArchived)
	cur, _ = e.svc.TeamGet(ctx, a.director, d.ID)
	e.svc.TeamAutosave(ctx, a.editor, d.ID, []byte(`{"title":"x"}`)) // замок Редактора
	if _, err := e.svc.TeamSave(ctx, a.director, d.ID, cur.Revision, memoContent("Архив, правка")); err == nil {
		t.Error("пока документ у Редактора в работе, Директорат должен получить LockedError")
	} else if _, ok := err.(*LockedError); !ok {
		t.Errorf("ожидался LockedError: %v", err)
	}
}

// ---------------------------------------------------------------- автосохранение

func TestAutosaveDoesNotTouchDocumentAndIsOfferedAsDraft(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")
	e.setStatus(d, StatusPublished) // опубликован: автосохранения читателям видны быть не должны
	cur, _ := e.svc.TeamGet(ctx, a.director, d.ID)

	half := `{"title":"Незаконченное назв","composed":{"year":1979},"blocks":[{"id":"b1","type":"paragraph","data":{"text":""}}]}`
	r, err := e.svc.TeamAutosave(ctx, a.director, d.ID, []byte(half))
	if err != nil || !r.Saved || r.VersionID == 0 || r.Lock == nil || !r.Lock.Mine {
		t.Fatalf("автосохранение: %+v %v", r, err)
	}
	// документ и читатели не затронуты (в том числе пустой блок, который на сохранении не прошёл бы)
	after, _ := e.svc.TeamGet(ctx, a.director, d.ID)
	if after.Revision != cur.Revision || after.Content.Title != "Черновик" || !after.UpdatedAt.Equal(cur.UpdatedAt) {
		t.Errorf("автосохранение изменило документ: %+v", after)
	}
	if seen, _ := e.svc.Get(ctx, viewer(0), *d.Code); seen.Title != "Черновик" {
		t.Errorf("читатель видит автосохранение: %q", seen.Title)
	}
	// ...но редактору предлагается как несохранённые правки
	if after.Draft == nil || after.Draft.Content.Title != "Незаконченное назв" || after.Draft.Author == nil || *after.Draft.Author != "director" {
		t.Fatalf("несохранённые правки не предложены: %+v", after.Draft)
	}

	// то же содержимое ещё раз — ничего нового не пишется, замок продлевается
	e.advance(5 * time.Minute)
	n := len(e.versions(d.ID))
	again, err := e.svc.TeamAutosave(ctx, a.director, d.ID, []byte(half))
	if err != nil || again.Saved || len(e.versions(d.ID)) != n {
		t.Errorf("повторное автосохранение: %+v %v (версий %d → %d)", again, err, n, len(e.versions(d.ID)))
	}
	if !again.Lock.ExpiresAt.After(r.Lock.ExpiresAt) {
		t.Error("автосохранение должно продлевать замок")
	}

	// после сохранения редакция выросла — старые автосохранения уже не предлагаются
	saved := mustSave(t, e, a.director, after, memoContent("Итог"))
	if saved.Document.Draft != nil {
		t.Errorf("после сохранения предлагаются устаревшие правки: %+v", saved.Document.Draft)
	}
	// автосохранение, равное живому содержимому, не предлагается
	live, _ := json.Marshal(saved.Document.Content)
	if _, err := e.svc.TeamAutosave(ctx, a.director, d.ID, live); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.svc.TeamGet(ctx, a.director, d.ID); got.Draft != nil {
		t.Errorf("правки, равные документу, предложены как несохранённые: %+v", got.Draft)
	}
}

func TestAutosaveValidationAndPermissions(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")

	for name, raw := range map[string]string{
		"не JSON": `{`, "массив": `[]`, "тип поля": `{"title": 5}`, "лишнее": `{"title":"x"} {}`, "нет блока": `{"blocks": "нет"}`,
	} {
		if _, err := e.svc.TeamAutosave(ctx, a.owner, d.ID, []byte(raw)); err == nil {
			t.Errorf("%s: должно отвергаться", name)
		}
	}
	// неизвестные поля не мешают (редактор нового поколения) и отбрасываются
	if _, err := e.svc.TeamAutosave(ctx, a.owner, d.ID, []byte(`{"title":"x","future":1}`)); err != nil {
		t.Errorf("неизвестное поле: %v", err)
	}
	big := `{"title":"` + strings.Repeat("я", MaxInputBytes) + `"}`
	if _, err := e.svc.TeamAutosave(ctx, a.owner, d.ID, []byte(big)); err == nil {
		t.Error("слишком большое содержимое должно отвергаться")
	}
	if _, err := e.svc.TeamAutosave(ctx, a.other, d.ID, []byte(`{"title":"x"}`)); !errors.Is(err, ErrNotFound) {
		t.Errorf("чужой черновик: %v", err)
	}
	if _, err := e.svc.TeamAutosave(ctx, a.moderator, d.ID, []byte(`{"title":"x"}`)); !errors.Is(err, ErrNotFound) {
		t.Errorf("без прав: %v", err)
	}
}

func TestRollingSnapshotsKeepLastThirtyAndNeverDeletePermanent(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")
	cur := d
	// 25 сохранений и 25 автосохранений вперемешку
	for i := 0; i < 25; i++ {
		e.advance(time.Second)
		r := mustSave(t, e, a.owner, cur, memoContent(fmt.Sprintf("Правка %d", i)))
		cur = r.Document
		if _, err := e.svc.TeamAutosave(ctx, a.owner, d.ID, []byte(fmt.Sprintf(`{"title":"Автосохранение %d"}`, i))); err != nil {
			t.Fatal(err)
		}
	}
	e.setStatus(cur, StatusReview) // вечный снимок

	var rolling, permanent int64
	e.db.Raw("SELECT count(*) FROM document_versions WHERE document_id = ? AND NOT permanent", d.ID).Scan(&rolling)
	e.db.Raw("SELECT count(*) FROM document_versions WHERE document_id = ? AND permanent", d.ID).Scan(&permanent)
	if rolling != MaxRollingVersions {
		t.Errorf("скользящих снимков %d, ожидалось %d", rolling, MaxRollingVersions)
	}
	if permanent != 2 { // создание и смена статуса
		t.Errorf("вечных снимков %d, ожидалось 2", permanent)
	}
	// остались именно самые новые
	var oldest string
	e.db.Raw(`SELECT content->>'title' FROM document_versions WHERE document_id = ? AND NOT permanent ORDER BY id LIMIT 1`, d.ID).Scan(&oldest)
	if oldest != "Автосохранение 10" && oldest != "Правка 10" && oldest != "Правка 9" && oldest != "Автосохранение 9" {
		t.Errorf("самый старый оставшийся снимок: %q", oldest)
	}
	// у другого документа своя квота
	other := e.draft(a.other, "Другой")
	mustSave(t, e, a.other, other, memoContent("Правка"))
	var otherRolling int64
	e.db.Raw("SELECT count(*) FROM document_versions WHERE document_id = ? AND NOT permanent", other.ID).Scan(&otherRolling)
	if otherRolling != 1 {
		t.Errorf("у другого документа скользящих %d", otherRolling)
	}
	// снимки без автора-владельца и правки опубликованного тоже вечные
	e.setStatus(cur, StatusPublished)
	e.advance(LockTTL + time.Minute) // замок автора истёк — документ может взять Редактор
	for i := 0; i < 3; i++ {
		e.advance(time.Second)
		pub, _ := e.svc.TeamGet(ctx, a.editor, d.ID)
		mustSave(t, e, a.editor, pub, memoContent(fmt.Sprintf("Опубликованное %d", i)))
	}
	e.db.Raw("SELECT count(*) FROM document_versions WHERE document_id = ? AND permanent", d.ID).Scan(&permanent)
	if permanent != 2+1+3 { // + публикация + три правки
		t.Errorf("вечных снимков после правок опубликованного %d, ожидалось 6", permanent)
	}
}

// ---------------------------------------------------------------- замки

func TestLockLifecycle(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")
	e.setStatus(d, StatusReview) // на проверке правят и Автор, и Редактор

	l, err := e.svc.TakeLock(ctx, a.owner, d.ID)
	if err != nil || !l.Mine || l.Holder != "owner" || !l.ExpiresAt.Equal(e.clock().Add(LockTTL)) {
		t.Fatalf("взять: %+v %v", l, err)
	}
	// другой человек видит, кто правит, и не может ни взять, ни сохранить, ни автосохранить, ни откатить
	if seen, _ := e.svc.TeamGet(ctx, a.editor, d.ID); seen.Lock == nil || seen.Lock.Mine || seen.Lock.Holder != "owner" {
		t.Errorf("замок глазами другого: %+v", seen.Lock)
	}
	var le *LockedError
	if _, err := e.svc.TakeLock(ctx, a.editor, d.ID); !errors.As(err, &le) || le.Holder != "owner" {
		t.Errorf("взять чужое: %v", err)
	}
	cur, _ := e.svc.TeamGet(ctx, a.editor, d.ID)
	if _, err := e.svc.TeamSave(ctx, a.editor, d.ID, cur.Revision, memoContent("Чужая правка")); !errors.As(err, &le) {
		t.Errorf("сохранение при чужом замке: %v", err)
	}
	if _, err := e.svc.TeamAutosave(ctx, a.editor, d.ID, []byte(`{"title":"x"}`)); !errors.As(err, &le) {
		t.Errorf("автосохранение при чужом замке: %v", err)
	}
	vs := e.versions(d.ID)
	if _, err := e.svc.TeamRestore(ctx, a.editor, d.ID, vs[0].ID); !errors.As(err, &le) {
		t.Errorf("откат при чужом замке: %v", err)
	}
	if got, _ := e.svc.TeamGet(ctx, a.owner, d.ID); got.Revision != cur.Revision || got.Content.Title != "Черновик" {
		t.Errorf("чужой замок не защитил документ: %+v", got)
	}

	// свой замок продлевается, время «взят» не меняется
	e.advance(10 * time.Minute)
	l2, err := e.svc.TakeLock(ctx, a.owner, d.ID)
	if err != nil || !l2.AcquiredAt.Equal(l.AcquiredAt) || !l2.ExpiresAt.Equal(e.clock().Add(LockTTL)) {
		t.Errorf("продление: %+v %v", l2, err)
	}
	// 14 минут спустя ещё держит, 15 — уже нет
	e.advance(LockTTL - time.Second)
	if _, err := e.svc.TakeLock(ctx, a.editor, d.ID); !errors.As(err, &le) {
		t.Errorf("за секунду до срока замок должен держать: %v", err)
	}
	e.advance(2 * time.Second)
	if seen, _ := e.svc.TeamGet(ctx, a.editor, d.ID); seen.Lock != nil {
		t.Errorf("просроченный замок показан: %+v", seen.Lock)
	}
	took, err := e.svc.TakeLock(ctx, a.editor, d.ID)
	if err != nil || took.Holder != "editor" {
		t.Errorf("после срока документ должен взять другой: %+v %v", took, err)
	}
	// теперь прежний владелец — чужой
	if _, err := e.svc.TakeLock(ctx, a.owner, d.ID); !errors.As(err, &le) || le.Holder != "editor" {
		t.Errorf("прежний владелец: %v", err)
	}
	// в списке замок виден
	list, _ := e.svc.TeamList(ctx, a.owner, TeamListQuery{})
	if list.Items[0].Lock == nil || list.Items[0].Lock.Holder != "editor" || list.Items[0].Lock.Mine {
		t.Errorf("замок в списке: %+v", list.Items[0].Lock)
	}
}

func TestSaveAcquiresFreeLockAndRelease(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")
	e.setStatus(d, StatusReview)

	if got, _ := e.svc.TeamGet(ctx, a.owner, d.ID); got.Lock != nil {
		t.Fatal("новый документ уже взят в работу")
	}
	r := mustSave(t, e, a.owner, d, memoContent("Правка"))
	if r.Document.Lock == nil || !r.Document.Lock.Mine {
		t.Fatal("сохранение не взяло замок")
	}
	// снять чужое без права нельзя; Редактор и Директорат — можно; повтор безопасен
	if err := e.svc.ReleaseLock(ctx, a.plain, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("без доступа: %v", err)
	}
	if err := e.svc.ReleaseLock(ctx, a.editor, d.ID); err != nil {
		t.Fatalf("Редактор снимает чужой замок: %v", err)
	}
	if got, _ := e.svc.TeamGet(ctx, a.owner, d.ID); got.Lock != nil {
		t.Error("замок не снят")
	}
	if err := e.svc.ReleaseLock(ctx, a.owner, d.ID); err != nil {
		t.Errorf("повторное снятие: %v", err)
	}
	// свой замок снимает владелец; чужой замок Автор без прав — нет
	if _, err := e.svc.TakeLock(ctx, a.editor, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ReleaseLock(ctx, a.owner, d.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("Автор снимает чужой замок: %v", err)
	}
	if err := e.svc.ReleaseLock(ctx, a.editor, d.ID); err != nil {
		t.Errorf("владелец замка снимает свой: %v", err)
	}
	// просроченная строка убирается без ошибки
	if _, err := e.svc.TakeLock(ctx, a.editor, d.ID); err != nil {
		t.Fatal(err)
	}
	e.advance(LockTTL + time.Minute)
	if err := e.svc.ReleaseLock(ctx, a.owner, d.ID); err != nil {
		t.Errorf("снятие просроченного: %v", err)
	}
	if n := e.count("SELECT count(*) FROM document_locks"); n != 0 {
		t.Errorf("строка замка осталась: %d", n)
	}
}

func TestLockRequiresEditRights(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")
	e.setStatus(d, StatusPublished)
	// владелец видит опубликованное, но править (а значит и брать в работу) не вправе
	if _, err := e.svc.TakeLock(ctx, a.owner, d.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("Автор берёт опубликованное: %v", err)
	}
	if _, err := e.svc.TakeLock(ctx, a.other, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("чужой Автор: %v", err)
	}
	if _, err := e.svc.TakeLock(ctx, a.editor, d.ID); err != nil {
		t.Errorf("Редактор: %v", err)
	}
}

func TestOnlyOneOfManyTakesTheLock(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")
	e.setStatus(d, StatusReview)
	claimants := []Actor{a.owner, a.editor, a.director}

	var wg sync.WaitGroup
	results := make(chan string, 30)
	for i := 0; i < 30; i++ {
		who := claimants[i%len(claimants)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.svc.TakeLock(ctx, who, d.ID); err == nil {
				results <- who.Login
			}
		}()
	}
	wg.Wait()
	close(results)
	winners := map[string]bool{}
	for w := range results {
		winners[w] = true
	}
	if len(winners) != 1 {
		t.Errorf("замок одновременно получили: %v", winners)
	}
}

// ---------------------------------------------------------------- откат и история

func TestRestoreRollsBackAsNewRevision(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Версия 1")
	e.setStatus(d, StatusPublished)
	cur, _ := e.svc.TeamGet(ctx, a.editor, d.ID)
	first := e.versions(d.ID)[0] // создание: «Версия 1»

	cur = mustSave(t, e, a.editor, cur, memoContent("Версия 2", paragraph("b1", "Второй текст"))).Document
	cur = mustSave(t, e, a.editor, cur, memoContent("Версия 3", paragraph("b1", "Третий текст"), paragraph("b2", "Дополнение"))).Document
	if cur.Revision != 3 {
		t.Fatalf("редакция %d", cur.Revision)
	}

	r, err := e.svc.TeamRestore(ctx, a.editor, d.ID, first.ID)
	if err != nil || !r.Changed {
		t.Fatalf("откат: %+v %v", r, err)
	}
	if r.Document.Content.Title != "Версия 1" || r.Document.Revision != 4 || len(r.Document.Content.Blocks) != 1 || r.Document.Status != "published" {
		t.Errorf("после отката: %q, редакция %d, блоков %d, статус %s", r.Document.Content.Title, r.Document.Revision, len(r.Document.Content.Blocks), r.Document.Status)
	}
	vs := e.versions(d.ID)
	last := vs[len(vs)-1]
	if last.Kind != "rollback" || !last.Permanent || !strings.Contains(last.Note, fmt.Sprintf("версии %d", first.ID)) {
		t.Errorf("снимок отката: %+v", last)
	}
	// история не переписана: прежние версии на месте, читатель видит откатанное
	if want := []string{"create", "status", "edit", "edit", "rollback"}; !slices.Equal(kindsOf(vs), want) {
		t.Errorf("история %v, ожидалось %v", kindsOf(vs), want)
	}
	if seen, _ := e.svc.Get(ctx, viewer(0), *d.Code); seen.Title != "Версия 1" {
		t.Errorf("читатель видит %q", seen.Title)
	}
	// откат к тому, что уже есть, — не изменение
	again, err := e.svc.TeamRestore(ctx, a.editor, d.ID, first.ID)
	if err != nil || again.Changed || again.Document.Revision != 4 {
		t.Errorf("повторный откат: %+v %v", again, err)
	}
	// права те же, что на правку: Автор опубликованное не откатывает, чужой не видит
	if _, err := e.svc.TeamRestore(ctx, a.owner, d.ID, first.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("Автор откатывает опубликованное: %v", err)
	}
	if _, err := e.svc.TeamRestore(ctx, a.other, d.ID, first.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("чужой Автор: %v", err)
	}
	if _, err := e.svc.TeamRestore(ctx, a.editor, d.ID, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующая версия: %v", err)
	}
	// снимок чужого документа по его номеру не откатить
	other := e.draft(a.editor, "Чужой")
	foreign := e.versions(other.ID)[0]
	if _, err := e.svc.TeamRestore(ctx, a.editor, d.ID, foreign.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("снимок другого документа: %v", err)
	}
}

func TestRestoreOfIncompleteAutosaveIsRejectedWithProblems(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")
	res, err := e.svc.TeamAutosave(ctx, a.owner, d.ID, []byte(`{"title":"","composed":{"year":1979},"blocks":[]}`))
	if err != nil || !res.Saved {
		t.Fatal(err)
	}
	_, err = e.svc.TeamRestore(ctx, a.owner, d.ID, res.VersionID)
	if !hasPath(err, "title") {
		t.Errorf("неполное автосохранение должно давать замечания: %v", err)
	}
	if got, _ := e.svc.TeamGet(ctx, a.owner, d.ID); got.Revision != 1 || got.Content.Title != "Черновик" {
		t.Errorf("отклонённый откат изменил документ: %+v", got)
	}
}

func TestVersionsHistoryListAndAccess(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")
	cur := d
	for i := 0; i < 4; i++ {
		e.advance(time.Minute)
		cur = mustSave(t, e, a.owner, cur, memoContent(fmt.Sprintf("Правка %d", i))).Document
	}
	e.setStatus(cur, StatusReview)

	page, err := e.svc.Versions(ctx, a.owner, d.ID, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 6 || page.Pages != 2 || len(page.Items) != 3 {
		t.Fatalf("страница истории: %+v", page)
	}
	// новые сверху; статус — самый свежий
	if page.Items[0].Kind != "status" || page.Items[0].Status != "review" || page.Items[0].KindName != "Смена статуса" {
		t.Errorf("первая строка: %+v", page.Items[0])
	}
	if page.Items[1].Author == nil || *page.Items[1].Author != "owner" || page.Items[1].Permanent {
		t.Errorf("строка сохранения: %+v", page.Items[1])
	}
	if page.Items[0].Author != nil {
		t.Errorf("смена статуса командой не должна приписываться человеку: %+v", page.Items[0])
	}

	full, err := e.svc.GetVersion(ctx, a.owner, d.ID, page.Items[1].ID)
	if err != nil || !strings.HasPrefix(full.Content.Title, "Правка") || full.Content.Composed == nil {
		t.Errorf("снимок: %+v %v", full, err)
	}
	// чужой не видит ни истории, ни снимков — ничего не раскрывается
	for _, who := range []Actor{a.other, a.plain} {
		if _, err := e.svc.Versions(ctx, who, d.ID, 1, 50); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: история чужого: %v", who.Login, err)
		}
		if _, err := e.svc.GetVersion(ctx, who, d.ID, page.Items[1].ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: снимок чужого: %v", who.Login, err)
		}
		if _, err := e.svc.DiffVersions(ctx, who, d.ID, page.Items[1].ID, 0); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: разница чужого: %v", who.Login, err)
		}
	}
	// удалённый автор: история остаётся, автора нет
	e.db.Exec("DELETE FROM users WHERE id = ?", a.owner.UserID)
	page, _ = e.svc.Versions(ctx, a.director, d.ID, 1, 50)
	for _, it := range page.Items {
		if it.Author != nil {
			t.Errorf("после удаления автора в истории остался %q", *it.Author)
		}
	}
	if _, err := e.svc.Versions(ctx, a.director, d.ID, -1, 50); err == nil {
		t.Error("отрицательная страница должна отвергаться")
	}
	if _, err := e.svc.Versions(ctx, a.director, d.ID, 1, 101); err == nil {
		t.Error("слишком большая страница должна отвергаться")
	}
}

func TestImportAndSetStatusLeaveSnapshots(t *testing.T) {
	e := newEnv(t)
	e.user("importer")
	e.imp(obj("О-041", "Документ", "draft", 0, `,"props":{"danger_class":2}`), ImportOptions{AuthorLogin: "importer"})
	var id int64
	e.db.Raw("SELECT id FROM documents WHERE code = 'О-041'").Scan(&id)

	e.imp(obj("О-041", "Документ, обновлённый", "draft", 0, `,"props":{"danger_class":3}`))
	vs := e.versions(id)
	if !slices.Equal(kindsOf(vs), []string{"import", "import"}) || vs[0].AuthorID == nil || vs[1].AuthorID != nil {
		t.Fatalf("снимки загрузки: %v", kindsOf(vs))
	}
	if !strings.Contains(string(vs[1].Content), "обновлённый") || vs[1].Revision != 2 {
		t.Errorf("снимок второй загрузки: %s (редакция %d)", vs[1].Content, vs[1].Revision)
	}

	if _, err := e.svc.SetStatus(ctx, "О-041", StatusReview); err != nil {
		t.Fatal(err)
	}
	vs = e.versions(id)
	if last := vs[len(vs)-1]; last.Kind != "status" || last.Note != "draft → review" || last.Status != "review" || !last.Permanent {
		t.Errorf("снимок смены статуса: %+v", last)
	}
	// тот же статус — ничего не пишется
	n := len(vs)
	if _, err := e.svc.SetStatus(ctx, "О-041", StatusReview); err != nil || len(e.versions(id)) != n {
		t.Errorf("повторная смена на тот же статус: %v, версий %d → %d", err, n, len(e.versions(id)))
	}
	// пробный прогон в историю не пишет
	if _, err := e.svc.Import(ctx, []byte(obj("О-041", "Пробный", "draft", 0, "")), ImportOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if len(e.versions(id)) != n {
		t.Error("пробный прогон оставил снимок")
	}
	// удаление документа убирает историю и замок
	e.svc.Delete(ctx, "О-041")
	if c := e.count("SELECT count(*) FROM document_versions WHERE document_id = ?", id); c != 0 {
		t.Errorf("после удаления документа осталось снимков: %d", c)
	}
}

// ---------------------------------------------------------------- разница

func blockOf(id, typ, data string, level *int) InputBlock {
	return InputBlock{ID: id, Type: typ, Level: level, Data: json.RawMessage(data)}
}

func TestDiffContent(t *testing.T) {
	base := Content{Title: "А", Composed: &Composed{Year: 1979}, Blocks: []InputBlock{
		blockOf("a", "paragraph", `{"text":"один"}`, nil),
		blockOf("b", "paragraph", `{"text":"два"}`, nil),
		blockOf("c", "paragraph", `{"text":"три"}`, nil),
		blockOf("d", "stamp", `{"text":"ИЗЪЯТО","tone":"red","tilt":0}`, nil),
	}}

	// одинаковое: порядок ключей и пробелы в data не важны
	same := base
	same.Blocks = append([]InputBlock(nil), base.Blocks...)
	same.Blocks[0] = blockOf("a", "paragraph", `{ "text" : "один" }`, intp(0))
	if d := DiffContent(base, same); !d.Same || d.Unchanged != 4 || len(d.Blocks) != 0 || len(d.Fields) != 0 {
		t.Errorf("равные содержимые: %+v", d)
	}

	changed := Content{Title: "Б", Level: intp(3), Composed: &Composed{Year: 1980, Month: intp(5)}, Grif: "Другой", DirectLink: "forbidden",
		Blocks: []InputBlock{
			blockOf("a", "paragraph", `{"text":"один"}`, nil),                         // не изменился
			blockOf("b", "paragraph", `{"text":"два, правка"}`, nil),                  // изменился текст
			blockOf("d", "stamp", `{"text":"ИЗЪЯТО","tone":"red","tilt":0}`, intp(4)), // изменился уровень
			blockOf("e", "paragraph", `{"text":"новый"}`, nil),                        // добавлен; «c» удалён
		}}
	d := DiffContent(base, changed)
	fields := map[string]bool{}
	for _, f := range d.Fields {
		fields[f.Field] = true
	}
	for _, want := range []string{"title", "level", "grif", "direct_link", "composed"} {
		if !fields[want] {
			t.Errorf("поле %q не помечено изменённым: %+v", want, d.Fields)
		}
	}
	got := map[string]string{}
	for _, b := range d.Blocks {
		got[b.ID] = b.Change
	}
	want := map[string]string{"b": "changed", "d": "changed", "e": "added", "c": "removed"}
	for id, ch := range want {
		if got[id] != ch {
			t.Errorf("блок %s: %q, ожидалось %q (%+v)", id, got[id], ch, got)
		}
	}
	if len(got) != len(want) || d.Unchanged != 1 || d.Same {
		t.Errorf("итог: %+v, неизменённых %d", got, d.Unchanged)
	}
	for _, b := range d.Blocks {
		switch b.Change {
		case "changed":
			if b.Before == nil || b.After == nil {
				t.Errorf("у changed нет до/после: %+v", b)
			}
		case "added":
			if b.After == nil || b.Before != nil {
				t.Errorf("у added: %+v", b)
			}
		case "removed":
			if b.Before == nil || b.After != nil {
				t.Errorf("у removed: %+v", b)
			}
		}
	}
}

func TestDiffDetectsMovedBlocks(t *testing.T) {
	mk := func(ids ...string) Content {
		c := Content{Title: "x", Composed: &Composed{Year: 1979}}
		for _, id := range ids {
			c.Blocks = append(c.Blocks, blockOf(id, "paragraph", fmt.Sprintf(`{"text":%q}`, id), nil))
		}
		return c
	}
	moved := func(from, to Content) []string {
		var out []string
		for _, b := range DiffContent(from, to).Blocks {
			if b.Change == "moved" {
				out = append(out, b.ID)
			}
		}
		slices.Sort(out)
		return out
	}
	// один блок перенесён в конец: «moved» только он, а не все, что сдвинулись
	if got := moved(mk("a", "b", "c", "d"), mk("b", "c", "d", "a")); !slices.Equal(got, []string{"a"}) {
		t.Errorf("перенос в конец: %v", got)
	}
	if got := moved(mk("a", "b", "c", "d"), mk("d", "a", "b", "c")); !slices.Equal(got, []string{"d"}) {
		t.Errorf("перенос в начало: %v", got)
	}
	if got := moved(mk("a", "b", "c"), mk("a", "b", "c")); len(got) != 0 {
		t.Errorf("без перестановок: %v", got)
	}
	// полный разворот: остаётся один неподвижный, остальные — moved
	if got := moved(mk("a", "b", "c"), mk("c", "b", "a")); len(got) != 2 {
		t.Errorf("разворот: %v", got)
	}
	// добавление в середину не превращает соседей в «moved»
	if got := moved(mk("a", "b"), mk("a", "x", "b")); len(got) != 0 {
		t.Errorf("вставка: %v", got)
	}
}

func TestDiffVersionsAgainstLiveAndAgainstVersion(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Первое название")
	first := e.versions(d.ID)[0]
	mustSave(t, e, a.owner, d, memoContent("Второе название", paragraph("b1", "Первый абзац."), paragraph("b2", "Добавлен")))

	df, err := e.svc.DiffVersions(ctx, a.owner, d.ID, first.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if df.Same || len(df.Fields) != 1 || df.Fields[0].Field != "title" || df.Fields[0].Before != "Первое название" || df.Fields[0].After != "Второе название" {
		t.Errorf("разница с текущим: %+v", df)
	}
	if len(df.Blocks) != 1 || df.Blocks[0].ID != "b2" || df.Blocks[0].Change != "added" || df.Unchanged != 1 {
		t.Errorf("блоки: %+v", df.Blocks)
	}
	second := e.versions(d.ID)[1]
	back, err := e.svc.DiffVersions(ctx, a.owner, d.ID, second.ID, first.ID)
	if err != nil || back.Blocks[0].Change != "removed" {
		t.Errorf("разница в обратную сторону: %+v %v", back, err)
	}
	if self, _ := e.svc.DiffVersions(ctx, a.owner, d.ID, second.ID, 0); !self.Same {
		t.Errorf("версия против самой себя (она же текущая): %+v", self)
	}
	if _, err := e.svc.DiffVersions(ctx, a.owner, d.ID, 999999, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующая версия: %v", err)
	}
}

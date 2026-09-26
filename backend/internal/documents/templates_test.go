package documents

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"kupol/internal/audit"
)

func tplBlocks() []InputBlock {
	return []InputBlock{
		rawBlock("h", "heading", `{"text":"Общие сведения"}`), // глубина по умолчанию 2 — сервер запишет её явно
		paragraph("p", "Описание."),
	}
}

func docTemplate(name string) TemplateInput {
	lvl := 2
	return TemplateInput{Kind: "document", Name: name, Description: "Заготовка объекта", DocType: "object",
		Content: TemplateContent{Title: "Объект «…»", Level: &lvl, DirectLink: "forbidden", Grif: "КУПОЛ · СЛУЖЕБНОЕ", Blocks: tplBlocks()}}
}

func setTemplate(name string) TemplateInput {
	return TemplateInput{Kind: "blockset", Name: name, Content: TemplateContent{Blocks: tplBlocks()}}
}

func (e *env) tpl(a Actor, in TemplateInput) *TemplateFull {
	e.t.Helper()
	t, err := e.svc.TemplateCreate(ctx, a, in)
	if err != nil {
		e.t.Fatalf("TemplateCreate(%q): %v", in.Name, err)
	}
	return t
}

func TestTemplateRightsMatrix(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	archivist := Actor{UserID: e.user("archivist"), Login: "archivist", CanManageTemplates: true}
	existing := e.tpl(archivist, docTemplate("Существующий"))

	for name, who := range map[string]struct {
		actor Actor
		can   bool
	}{
		"Автор": {a.owner, false}, "модератор": {a.moderator, false}, "посторонний": {a.plain, false},
		"Архивариус": {archivist, true}, "Директорат": {a.director, true},
	} {
		_, createErr := e.svc.TemplateCreate(ctx, who.actor, docTemplate("От "+name))
		_, updateErr := e.svc.TemplateUpdate(ctx, who.actor, existing.ID, "Новое имя "+name, "", nil)
		if who.can {
			if createErr != nil || updateErr != nil {
				t.Errorf("%s: создание %v, правка %v", name, createErr, updateErr)
			}
		} else if !errors.Is(createErr, ErrForbidden) || !errors.Is(updateErr, ErrForbidden) || !errors.Is(e.svc.TemplateDelete(ctx, who.actor, existing.ID), ErrForbidden) {
			t.Errorf("%s: чужому шаблон вести нельзя: создание %v, правка %v", name, createErr, updateErr)
		}
		// читать могут все члены команды, но кнопки правки показываются только тем, кто вправе
		got, err := e.svc.TemplateGet(ctx, who.actor, existing.ID)
		if err != nil || got.CanEdit != who.can {
			t.Errorf("%s: чтение %v, can_edit=%v", name, err, got != nil && got.CanEdit)
		}
	}
}

func TestTemplateCreateStoresCanonicalContent(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	doc := e.tpl(a.director, docTemplate("Объект-заготовка"))
	if doc.Kind != "document" || doc.DocType == nil || *doc.DocType != "object" || doc.DocTypeName != "Объект" || doc.Blocks != 2 {
		t.Fatalf("шаблон документа: %+v", doc.TemplateItem)
	}
	if doc.Content.Title != "Объект «…»" || doc.Content.Level == nil || *doc.Content.Level != 2 || doc.Content.DirectLink != "forbidden" || doc.Content.Grif != "КУПОЛ · СЛУЖЕБНОЕ" {
		t.Errorf("поля документа: %+v", doc.Content)
	}
	if !strings.Contains(string(doc.Content.Blocks[0].Data), `"depth": 2`) {
		t.Errorf("блок не приведён к каноническому виду (глубина заголовка не записана): %s", doc.Content.Blocks[0].Data)
	}
	if doc.Author == nil || *doc.Author != "director" {
		t.Errorf("автор шаблона: %v", doc.Author)
	}

	set := e.tpl(a.director, setTemplate("Шапка и заголовок"))
	if set.Kind != "blockset" || set.DocType != nil || set.DocTypeName != "" || set.Content.Title != "" || set.Content.Level != nil {
		t.Errorf("набор блоков: %+v", set)
	}

	// шаблон действительно применим: документ из него проходит обычную проверку
	c := doc.Content
	created, err := e.svc.TeamCreate(ctx, a.owner, CreateInput{Type: *doc.DocType, Content: Content{Title: c.Title, Level: c.Level, DirectLink: c.DirectLink, Grif: c.Grif, Composed: &Composed{Year: 1979}, Blocks: c.Blocks}})
	if err != nil {
		t.Fatalf("документ из шаблона не создаётся: %v", err)
	}
	if created.Content.Title != c.Title || len(created.Content.Blocks) != 2 {
		t.Errorf("документ из шаблона: %+v", created.Content)
	}
}

func TestTemplateValidation(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	paths := func(err error) string {
		t.Helper()
		ve, ok := err.(*ValidationError)
		if !ok {
			t.Fatalf("ожидалась ошибка проверки, получено %v", err)
		}
		out := []string{}
		for _, p := range ve.Problems {
			out = append(out, p.Path)
		}
		return strings.Join(out, ",")
	}
	mut := func(base TemplateInput, f func(*TemplateInput)) TemplateInput { f(&base); return base }
	badLevel := 9
	cases := []struct {
		name string
		in   TemplateInput
		want string // путь замечания (подстрока)
	}{
		{"пустое название", mut(docTemplate("x"), func(i *TemplateInput) { i.Name = "   " }), "name"},
		{"длинное название", mut(docTemplate("x"), func(i *TemplateInput) { i.Name = strings.Repeat("я", 101) }), "name"},
		{"длинное описание", mut(docTemplate("x"), func(i *TemplateInput) { i.Description = strings.Repeat("я", 501) }), "description"},
		{"неизвестный вид", mut(docTemplate("x"), func(i *TemplateInput) { i.Kind = "folder" }), "kind"},
		{"неизвестный тип документа", mut(docTemplate("x"), func(i *TemplateInput) { i.DocType = "poem" }), "doc_type"},
		{"нет типа у шаблона документа", mut(docTemplate("x"), func(i *TemplateInput) { i.DocType = "" }), "doc_type"},
		{"тип у набора блоков", mut(setTemplate("x"), func(i *TemplateInput) { i.DocType = "object" }), "doc_type"},
		{"название у набора блоков", mut(setTemplate("x"), func(i *TemplateInput) { i.Content.Title = "Заголовок" }), "content"},
		{"допуск у набора блоков", mut(setTemplate("x"), func(i *TemplateInput) { i.Content.Level = &badLevel }), "content"},
		{"уровень вне 0–7", mut(docTemplate("x"), func(i *TemplateInput) { i.Content.Level = &badLevel }), "content.level"},
		{"режим прямой ссылки", mut(docTemplate("x"), func(i *TemplateInput) { i.Content.DirectLink = "maybe" }), "content.direct_link"},
		{"длинный гриф", mut(docTemplate("x"), func(i *TemplateInput) { i.Content.Grif = strings.Repeat("я", 101) }), "content.grif"},
		{"нет блоков", mut(docTemplate("x"), func(i *TemplateInput) { i.Content.Blocks = nil }), "content.blocks"},
		{"пустой штамп", mut(setTemplate("x"), func(i *TemplateInput) { i.Content.Blocks = []InputBlock{rawBlock("s", "stamp", `{"text":""}`)} }), "content.blocks[0].data.text"},
		{"неизвестный блок", mut(setTemplate("x"), func(i *TemplateInput) { i.Content.Blocks = []InputBlock{rawBlock("q", "hologram", `{}`)} }), "content.blocks[0].type"},
		{"повтор идентификатора", mut(setTemplate("x"), func(i *TemplateInput) {
			i.Content.Blocks = []InputBlock{paragraph("same", "а"), paragraph("same", "б")}
		}), "content.blocks[1].id"},
	}
	for _, c := range cases {
		_, err := e.svc.TemplateCreate(ctx, a.director, c.in)
		if got := paths(err); !strings.Contains(got, c.want) {
			t.Errorf("%s: замечания %q, ожидался путь %q", c.name, got, c.want)
		}
	}
	// в наборе блоков блоков не больше предела
	many := make([]InputBlock, maxBlocksetBlocks+1)
	for i := range many {
		many[i] = paragraph(fmt.Sprintf("b%d", i), "текст")
	}
	_, err := e.svc.TemplateCreate(ctx, a.director, mut(setTemplate("много"), func(i *TemplateInput) { i.Content.Blocks = many }))
	if got := paths(err); !strings.Contains(got, "content.blocks") {
		t.Errorf("слишком большой набор: %q", got)
	}
	if n := e.count("SELECT count(*) FROM templates"); n != 0 {
		t.Errorf("ошибочные шаблоны сохранились: %d", n)
	}
}

func TestTemplateNamesAreUniquePerKind(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	first := e.tpl(a.director, docTemplate("Типовой объект"))
	for _, dup := range []string{"Типовой объект", "  типовой ОБЪЕКТ  "} {
		if _, err := e.svc.TemplateCreate(ctx, a.director, docTemplate(dup)); !errors.Is(err, ErrTemplateNameTaken) {
			t.Errorf("повтор %q: %v", dup, err)
		}
	}
	// то же название у набора блоков — другой вид, можно
	e.tpl(a.director, setTemplate("Типовой объект"))
	second := e.tpl(a.director, docTemplate("Другой"))
	if _, err := e.svc.TemplateUpdate(ctx, a.director, second.ID, "типовой объект", "", nil); !errors.Is(err, ErrTemplateNameTaken) {
		t.Errorf("переименование в занятое: %v", err)
	}
	// себя же переименовать (регистр) можно
	if _, err := e.svc.TemplateUpdate(ctx, a.director, first.ID, "ТИПОВОЙ объект", "", nil); err != nil {
		t.Errorf("смена регистра у самого себя: %v", err)
	}
}

func TestTemplateUpdateAndDelete(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	t0 := e.tpl(a.director, docTemplate("Исходное"))
	e.advance(60_000_000_000)

	// только название и описание: содержимое не тронуто
	up, err := e.svc.TemplateUpdate(ctx, a.director, t0.ID, "  Новое название  ", "Новое описание", nil)
	if err != nil || up.Name != "Новое название" || up.Description != "Новое описание" || up.Blocks != 2 || !up.UpdatedAt.After(t0.UpdatedAt) {
		t.Fatalf("правка названия: %v %+v", err, up)
	}
	if up.Kind != "document" || up.DocType == nil || *up.DocType != "object" {
		t.Errorf("вид и тип менять нельзя: %+v", up.TemplateItem)
	}
	// содержимое заменяется целиком и проверяется
	repl := TemplateContent{Title: "Иначе", Blocks: []InputBlock{paragraph("x", "Единственный.")}}
	up, err = e.svc.TemplateUpdate(ctx, a.director, t0.ID, "Новое название", "", &repl)
	if err != nil || up.Blocks != 1 || up.Content.Title != "Иначе" {
		t.Fatalf("замена содержимого: %v %+v", err, up)
	}
	bad := TemplateContent{Blocks: []InputBlock{rawBlock("s", "stamp", `{"text":""}`)}}
	if _, err := e.svc.TemplateUpdate(ctx, a.director, t0.ID, "Новое название", "", &bad); err == nil {
		t.Error("неверное содержимое принято")
	}
	if got, _ := e.svc.TemplateGet(ctx, a.director, t0.ID); got.Blocks != 1 {
		t.Errorf("неудачная замена испортила шаблон: %+v", got.TemplateItem)
	}
	// у набора блоков нельзя завести название документа
	set := e.tpl(a.director, setTemplate("Набор"))
	if _, err := e.svc.TemplateUpdate(ctx, a.director, set.ID, "Набор", "", &TemplateContent{Title: "Нельзя", Blocks: tplBlocks()}); err == nil {
		t.Error("набору блоков добавили название документа")
	}

	if _, err := e.svc.TemplateUpdate(ctx, a.director, 999999, "х", "", nil); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("правка несуществующего: %v", err)
	}
	if err := e.svc.TemplateDelete(ctx, a.director, t0.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.TemplateGet(ctx, a.director, t0.ID); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("шаблон после удаления: %v", err)
	}
	if err := e.svc.TemplateDelete(ctx, a.director, t0.ID); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("повторное удаление: %v", err)
	}

	// журнал: заведён, изменён (дважды до ошибки — только успешные), удалён
	for action, want := range map[audit.Action]int{audit.TemplateCreated: 2, audit.TemplateUpdated: 2, audit.TemplateDeleted: 1} {
		if rows := e.auditRows(action); len(rows) != want {
			t.Errorf("журнал %s: %d записей, ожидалось %d", action, len(rows), want)
		}
	}
}

func TestTemplateListIsSortedFilteredAndCountsBlocks(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	e.tpl(a.director, docTemplate("Ясный объект"))
	e.tpl(a.director, docTemplate("Бледный объект"))
	e.tpl(a.director, setTemplate("Шапка"))

	all, err := e.svc.TemplateList(ctx, a.owner, "")
	if err != nil || len(all) != 3 {
		t.Fatalf("список: %v %d", err, len(all))
	}
	if all[0].Name != "Бледный объект" || all[1].Name != "Шапка" || all[2].Name != "Ясный объект" {
		t.Errorf("порядок (по алфавиту, по-русски): %q %q %q", all[0].Name, all[1].Name, all[2].Name)
	}
	docs, _ := e.svc.TemplateList(ctx, a.owner, "document")
	sets, _ := e.svc.TemplateList(ctx, a.owner, "blockset")
	if len(docs) != 2 || len(sets) != 1 || sets[0].Blocks != 2 || sets[0].CanEdit {
		t.Errorf("фильтр по виду: %d, %d, %+v", len(docs), len(sets), sets)
	}
	if _, err := e.svc.TemplateList(ctx, a.owner, "folder"); err == nil {
		t.Error("неизвестный вид принят")
	} else if qe, ok := err.(*QueryError); !ok || qe.Field != "kind" {
		t.Errorf("ошибка фильтра: %v", err)
	}
}

func TestTemplateAuthorAnonymisedOnAccountDeletion(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	tp := e.tpl(a.director, setTemplate("Останется"))
	if err := e.db.Exec("DELETE FROM users WHERE id = ?", a.director.UserID).Error; err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.TemplateGet(ctx, a.owner, tp.ID)
	if err != nil || got.Author != nil || got.Name != "Останется" {
		t.Errorf("шаблон после удаления автора: %v %+v", err, got)
	}
}

func TestTemplateSchemaConstraints(t *testing.T) {
	e := newEnv(t)
	now := e.clock()
	for name, q := range map[string]string{
		"набор блоков с типом":      `INSERT INTO templates (kind, name, doc_type, content, created_at, updated_at) VALUES ('blockset', 'a', 'object', '{}', ?, ?)`,
		"шаблон документа без типа": `INSERT INTO templates (kind, name, content, created_at, updated_at) VALUES ('document', 'b', '{}', ?, ?)`,
		"неизвестный вид":           `INSERT INTO templates (kind, name, content, created_at, updated_at) VALUES ('folder', 'c', '{}', ?, ?)`,
		"пустое название":           `INSERT INTO templates (kind, name, content, created_at, updated_at) VALUES ('blockset', '  ', '{}', ?, ?)`,
		"содержимое не объект":      `INSERT INTO templates (kind, name, content, created_at, updated_at) VALUES ('blockset', 'd', '[]', ?, ?)`,
		"длинное описание":          `INSERT INTO templates (kind, name, description, content, created_at, updated_at) VALUES ('blockset', 'e', repeat('я', 501), '{}', ?, ?)`,
	} {
		if err := e.db.Exec(q, now, now).Error; err == nil {
			t.Errorf("%s: запись принята", name)
		}
	}
	ok := `INSERT INTO templates (kind, name, content, created_at, updated_at) VALUES ('blockset', 'Набор', '{}', ?, ?)`
	if err := e.db.Exec(ok, now, now).Error; err != nil {
		t.Fatalf("корректная запись отклонена: %v", err)
	}
	if err := e.db.Exec(`INSERT INTO templates (kind, name, content, created_at, updated_at) VALUES ('blockset', ' НАБОР ', '{}', ?, ?)`, now, now).Error; err == nil {
		t.Error("повтор названия (регистр и пробелы) принят на уровне базы")
	}
}

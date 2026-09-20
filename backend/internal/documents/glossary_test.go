package documents

import (
	"errors"
	"strings"
	"testing"

	"kupol/internal/audit"
)

func term(t, d string, aliases ...string) TermInput {
	return TermInput{Term: t, Definition: d, Aliases: aliases}
}

func (e *env) term(a Actor, in TermInput) *TermOut {
	e.t.Helper()
	o, err := e.svc.GlossaryCreate(ctx, a, in)
	if err != nil {
		e.t.Fatalf("GlossaryCreate(%q): %v", in.Term, err)
	}
	return o
}

func TestGlossaryRightsMatrix(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	archivist := Actor{UserID: e.user("archivist"), Login: "archivist", CanManageGlossary: true}
	existing := e.term(archivist, term("Купол", "Комитет управления."))

	for name, who := range map[string]struct {
		actor Actor
		can   bool
	}{
		"Автор": {a.owner, false}, "модератор": {a.moderator, false}, "посторонний": {a.plain, false},
		"Архивариус": {archivist, true}, "Директорат": {a.director, true},
	} {
		_, createErr := e.svc.GlossaryCreate(ctx, who.actor, term("Термин "+name, "Определение."))
		_, updateErr := e.svc.GlossaryUpdate(ctx, who.actor, existing.ID, term("Купол", "Правка от "+name))
		if who.can {
			if createErr != nil || updateErr != nil {
				t.Errorf("%s: создание %v, правка %v", name, createErr, updateErr)
			}
		} else if !errors.Is(createErr, ErrForbidden) || !errors.Is(updateErr, ErrForbidden) || !errors.Is(e.svc.GlossaryDelete(ctx, who.actor, existing.ID), ErrForbidden) {
			t.Errorf("%s: вести глоссарий нельзя: создание %v, правка %v", name, createErr, updateErr)
		}
		list, err := e.svc.GlossaryList(ctx, who.actor, "")
		if err != nil || len(list) == 0 || list[0].CanEdit != who.can {
			t.Errorf("%s: чтение %v, can_edit=%v", name, err, len(list) > 0 && list[0].CanEdit)
		}
	}
}

func TestGlossaryNormalisesAndValidates(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	o := e.term(a.director, term("  ЦАК  ", "  Центральный архив Купола.  ", " Центральный архив ", "цак", "ЦАК", "  ", "Архив"))
	if o.Term != "ЦАК" || o.Definition != "Центральный архив Купола." {
		t.Errorf("пробелы по краям не срезаны: %+v", o)
	}
	// пустые и повторные (с термином и между собой, без учёта регистра) написания убраны, порядок сохранён
	if strings.Join(o.Aliases, "|") != "Центральный архив|Архив" {
		t.Errorf("написания: %v", o.Aliases)
	}
	if o.Author == nil || *o.Author != "director" || !o.CanEdit {
		t.Errorf("автор и права: %+v", o)
	}
	if bare := e.term(a.director, term("Отдел", "Подразделение.")); bare.Aliases == nil || len(bare.Aliases) != 0 {
		t.Errorf("без написаний должен быть пустой список, а не null: %#v", bare.Aliases)
	}

	many := make([]string, maxGlossaryAliases+1)
	for i := range many {
		many[i] = strings.Repeat("я", i+1)
	}
	for name, c := range map[string]struct {
		in   TermInput
		path string
	}{ // ключ — описание случая
		"пустой термин":       {term("  ", "Определение."), "term"},
		"длинный термин":      {term(strings.Repeat("я", 101), "Определение."), "term"},
		"пустое определение":  {term("Слово", "   "), "definition"},
		"длинное определение": {term("Слово", strings.Repeat("я", 2001)), "definition"},
		"слишком много":       {TermInput{Term: "Слово", Definition: "Определение.", Aliases: many}, "aliases"},
		"длинное написание":   {term("Слово", "Определение.", strings.Repeat("я", 101)), "aliases"},
		"управляющие символы": {term("Сло\x00во", "Определение."), "term"},
	} {
		_, err := e.svc.GlossaryCreate(ctx, a.director, c.in)
		ve, ok := err.(*ValidationError)
		if !ok || len(ve.Problems) == 0 || ve.Problems[0].Path != c.path {
			t.Errorf("%s: ожидалось замечание к %q, получено %v", name, c.path, err)
		}
	}
}

func TestGlossaryTermsAreUniqueWithoutCase(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	first := e.term(a.director, term("Надзиратель", "Уровень допуска 4."))
	for _, dup := range []string{"Надзиратель", "  надзиратель  ", "НАДЗИРАТЕЛЬ"} {
		if _, err := e.svc.GlossaryCreate(ctx, a.director, term(dup, "Дубль.")); !errors.Is(err, ErrTermTaken) {
			t.Errorf("повтор %q: %v", dup, err)
		}
	}
	second := e.term(a.director, term("Куратор", "Уровень допуска 5."))
	if _, err := e.svc.GlossaryUpdate(ctx, a.director, second.ID, term("надзиратель", "Переименование в занятое.")); !errors.Is(err, ErrTermTaken) {
		t.Errorf("переименование в занятое: %v", err)
	}
	// себя же переписать (регистр) можно
	if _, err := e.svc.GlossaryUpdate(ctx, a.director, first.ID, term("НАДЗИРАТЕЛЬ", "Уровень допуска 4, «надзиратель».")); err != nil {
		t.Errorf("смена регистра у самого себя: %v", err)
	}
	// написание одного термина может совпасть с другим термином: это справочные варианты, а не сущности
	if _, err := e.svc.GlossaryCreate(ctx, a.director, term("Совет", "Особый Совет.", "куратор")); err != nil {
		t.Errorf("написание, совпавшее с чужим термином: %v", err)
	}
}

func TestGlossarySearchSortAndCrud(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	e.term(a.director, term("Яма", "Провал в грунте.", "провал"))
	e.term(a.director, term("Архив", "Хранилище документов.", "ЦАК"))
	e.term(a.director, term("Ёж", "Существо с колючками; см. Яма."))

	all, _ := e.svc.GlossaryList(ctx, a.owner, "")
	names := []string{}
	for _, o := range all {
		names = append(names, o.Term)
	}
	if strings.Join(names, ",") != "Архив,Ёж,Яма" {
		t.Errorf("порядок (по алфавиту, по-русски): %v", names)
	}
	for q, want := range map[string]string{
		"арх":        "Архив", // по термину
		"цак":        "Архив", // по другому написанию, без учёта регистра
		"хранилищ":   "Архив", // по определению
		"колюч":      "Ёж",    // по определению
		"ПРОВАЛ":     "Яма",   // и написание, и определение одного термина — но термин в списке один
		"нет такого": "",
	} {
		got, err := e.svc.GlossaryList(ctx, a.owner, q)
		if err != nil {
			t.Fatal(err)
		}
		found := []string{}
		for _, o := range got {
			found = append(found, o.Term)
		}
		if strings.Join(found, ",") != want {
			t.Errorf("поиск %q: %v, ожидалось %q", q, found, want)
		}
	}
	// спецсимволы LIKE не работают как шаблоны
	if got, _ := e.svc.GlossaryList(ctx, a.owner, "%"); len(got) != 0 {
		t.Errorf("«%%» нашёл %d терминов", len(got))
	}
	if _, err := e.svc.GlossaryList(ctx, a.owner, strings.Repeat("я", 101)); err == nil {
		t.Error("слишком длинный запрос принят")
	}

	// правка заменяет всё, включая написания
	up, err := e.svc.GlossaryUpdate(ctx, a.director, all[0].ID, term("Архив", "Новое определение.", "Хранилище"))
	if err != nil || up.Definition != "Новое определение." || len(up.Aliases) != 1 || up.Aliases[0] != "Хранилище" {
		t.Fatalf("правка: %v %+v", err, up)
	}
	if _, err := e.svc.GlossaryUpdate(ctx, a.director, 999999, term("Х", "Y")); !errors.Is(err, ErrTermNotFound) {
		t.Errorf("правка несуществующего: %v", err)
	}
	if err := e.svc.GlossaryDelete(ctx, a.director, all[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.GlossaryDelete(ctx, a.director, all[0].ID); !errors.Is(err, ErrTermNotFound) {
		t.Errorf("повторное удаление: %v", err)
	}
	for action, want := range map[audit.Action]int{audit.GlossaryCreated: 3, audit.GlossaryUpdated: 1, audit.GlossaryDeleted: 1} {
		if rows := e.auditRows(action); len(rows) != want {
			t.Errorf("журнал %s: %d записей, ожидалось %d", action, len(rows), want)
		}
	}
}

func TestGlossaryAuthorAnonymisedAndSchemaConstraints(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	o := e.term(a.director, term("Останется", "Запись переживёт автора."))
	if err := e.db.Exec("DELETE FROM users WHERE id = ?", a.director.UserID).Error; err != nil {
		t.Fatal(err)
	}
	list, err := e.svc.GlossaryList(ctx, a.owner, "")
	if err != nil || len(list) != 1 || list[0].ID != o.ID || list[0].Author != nil {
		t.Errorf("термин после удаления автора: %v %+v", err, list)
	}

	now := e.clock()
	for name, q := range map[string]string{
		"пустой термин":           `INSERT INTO glossary_terms (term, definition, created_at, updated_at) VALUES ('  ', 'x', ?, ?)`,
		"пустое определение":      `INSERT INTO glossary_terms (term, definition, created_at, updated_at) VALUES ('a', '  ', ?, ?)`,
		"написания не массив":     `INSERT INTO glossary_terms (term, definition, aliases, created_at, updated_at) VALUES ('b', 'x', '{}', ?, ?)`,
		"слишком много написаний": `INSERT INTO glossary_terms (term, definition, aliases, created_at, updated_at) VALUES ('c', 'x', '["1","2","3","4","5","6","7","8","9","10","11"]', ?, ?)`,
	} {
		if err := e.db.Exec(q, now, now).Error; err == nil {
			t.Errorf("%s: запись принята", name)
		}
	}
	if err := e.db.Exec(`INSERT INTO glossary_terms (term, definition, created_at, updated_at) VALUES ('Ok', 'x', ?, ?)`, now, now).Error; err != nil {
		t.Fatalf("корректная запись отклонена: %v", err)
	}
	if err := e.db.Exec(`INSERT INTO glossary_terms (term, definition, created_at, updated_at) VALUES (' OK ', 'x', ?, ?)`, now, now).Error; err == nil {
		t.Error("повтор термина (регистр и пробелы) принят на уровне базы")
	}
}

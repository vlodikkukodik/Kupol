package documents

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"kupol/internal/audit"
)

func ev(year int, title string, level int) TimelineInput {
	return TimelineInput{Year: year, Title: title, Level: level}
}

func (e *env) event(a Actor, in TimelineInput) *TimelineEventOut {
	e.t.Helper()
	o, err := e.svc.TimelineCreate(ctx, a, in)
	if err != nil {
		e.t.Fatalf("TimelineCreate(%q): %v", in.Title, err)
	}
	return o
}

func titles(items []TimelineItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}

func TestTimelineRightsMatrix(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	archivist := Actor{UserID: e.user("archivist"), Login: "archivist", CanManageTimeline: true}
	existing := e.event(archivist, ev(1974, "Основан КУПОЛ", 0))

	for name, who := range map[string]struct {
		actor Actor
		can   bool
	}{
		"Автор": {a.owner, false}, "модератор": {a.moderator, false}, "посторонний": {a.plain, false},
		"Редактор без права": {a.editor, false}, "Архивариус": {archivist, true}, "Директорат": {a.director, true},
	} {
		_, createErr := e.svc.TimelineCreate(ctx, who.actor, ev(1980, "Событие "+name, 0))
		_, updateErr := e.svc.TimelineUpdate(ctx, who.actor, existing.ID, ev(1974, "Правка "+name, 0))
		if who.can {
			if createErr != nil || updateErr != nil {
				t.Errorf("%s: создание %v, правка %v", name, createErr, updateErr)
			}
		} else if !errors.Is(createErr, ErrForbidden) || !errors.Is(updateErr, ErrForbidden) || !errors.Is(e.svc.TimelineDelete(ctx, who.actor, existing.ID), ErrForbidden) {
			t.Errorf("%s: вести хронологию нельзя: создание %v, правка %v", name, createErr, updateErr)
		}
		list, err := e.svc.TimelineList(ctx, who.actor)
		if err != nil || len(list) == 0 || list[0].CanEdit != who.can {
			t.Errorf("%s: чтение %v, can_edit=%v", name, err, len(list) > 0 && list[0].CanEdit)
		}
	}
}

func TestTimelineValidation(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	m, d := 2, 30
	m13, d0 := 13, 0
	feb29 := 29
	for name, in := range map[string]TimelineInput{
		"год мал":           {Year: 1899, Title: "Х"},
		"год велик":         {Year: 2100, Title: "Х"},
		"месяц 13":          {Year: 1980, Month: &m13, Title: "Х"},
		"день без месяца":   {Year: 1980, Day: &d0, Title: "Х"},
		"30 февраля":        {Year: 1980, Month: &m, Day: &d, Title: "Х"},
		"29 февраля 1981":   {Year: 1981, Month: &m, Day: &feb29, Title: "Х"},
		"пустое название":   {Year: 1980, Title: "  "},
		"длинное название":  {Year: 1980, Title: strings.Repeat("я", maxTimelineTitle+1)},
		"длинный текст":     {Year: 1980, Title: "Х", Body: strings.Repeat("я", maxTimelineBody+1)},
		"допуск 8":          {Year: 1980, Title: "Х", Level: 8},
		"допуск -1":         {Year: 1980, Title: "Х", Level: -1},
		"не шифр":           {Year: 1980, Title: "Х", DocumentCode: "мусор"},
		"управляющие знаки": {Year: 1980, Title: "Х\x00"},
	} {
		var ve *ValidationError
		if _, err := e.svc.TimelineCreate(ctx, a.director, in); !errors.As(err, &ve) || len(ve.Problems) == 0 {
			t.Errorf("%s: ожидались замечания, получено %v", name, err)
		}
	}
	if n := e.count("SELECT count(*) FROM timeline_events"); n != 0 {
		t.Errorf("отклонённые события сохранены: %d", n)
	}
	// 29 февраля високосного года — можно; шифр приводится к каноническому виду; пробелы срезаны
	m29 := 2
	o := e.event(a.director, TimelineInput{Year: 1980, Month: &m29, Day: &feb29, Title: "  Високосный день  ", Body: "  Текст.  ", DocumentCode: "o-41"})
	if o.Title != "Високосный день" || o.Body != "Текст." || o.DocumentCode != "О-041" || o.Day == nil || *o.Day != 29 {
		t.Errorf("событие: %+v", o)
	}
	if _, err := e.svc.TimelineUpdate(ctx, a.director, 999999, ev(1980, "Х", 0)); !errors.Is(err, ErrEventNotFound) {
		t.Errorf("правка несуществующего: %v", err)
	}
	if err := e.svc.TimelineDelete(ctx, a.director, 999999); !errors.Is(err, ErrEventNotFound) {
		t.Errorf("удаление несуществующего: %v", err)
	}
}

// Шкала читателя: события не выше допуска, по порядку дат (год, месяц, день; без месяца — раньше датированных в этом году).
func TestTimelineShowsOnlyEventsUpToViewerLevel(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	for level := 0; level <= 7; level++ {
		e.event(a.director, ev(1974+level, fmt.Sprintf("Событие уровня %d секрет%dмарка", level, level), level))
	}
	for viewerLevel := 0; viewerLevel <= 7; viewerLevel++ {
		items, err := e.svc.Timeline(ctx, viewerAt(viewerLevel))
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != viewerLevel+1 {
			t.Errorf("уровень %d: событий %d, ожидалось %d", viewerLevel, len(items), viewerLevel+1)
		}
		raw := fmt.Sprintf("%+v", items)
		for hidden := viewerLevel + 1; hidden <= 7; hidden++ {
			if strings.Contains(raw, fmt.Sprintf("секрет%dмарка", hidden)) {
				t.Errorf("уровень %d: в шкале событие уровня %d", viewerLevel, hidden)
			}
		}
	}

	// порядок дат
	e2 := newEnv(t)
	a2 := e2.actors()
	m3, m5, d1 := 3, 5, 1
	e2.event(a2.director, TimelineInput{Year: 1980, Month: &m5, Title: "май"})
	e2.event(a2.director, TimelineInput{Year: 1980, Title: "весь год"})
	e2.event(a2.director, TimelineInput{Year: 1979, Title: "прошлый год"})
	e2.event(a2.director, TimelineInput{Year: 1980, Month: &m3, Day: &d1, Title: "1 марта"})
	e2.event(a2.director, TimelineInput{Year: 1980, Month: &m3, Title: "март"})
	items, _ := e2.svc.Timeline(ctx, Guest)
	if got := titles(items); !slices.Equal(got, []string{"прошлый год", "весь год", "март", "1 марта", "май"}) {
		t.Errorf("порядок: %v", got)
	}
}

// Ссылка на документ видна, только если читатель вправе открыть документ; иначе события — без ссылки, а не с «обрывком».
func TestTimelineDocumentLinkNeverRevealsHiddenDocument(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	e.imp(obj("О-840", "Открытый документ", "published", 0, blocks(para("a", nil, "текст"))))
	e.imp(obj("О-841", "Закрытый документ секретдок", "published", 4, blocks(para("a", nil, "текст"))))
	e.imp(obj("О-842", "Черновик секретчерновик", "draft", 0, blocks(para("a", nil, "текст"))))
	e.event(a.director, TimelineInput{Year: 1980, Title: "Про открытый", DocumentCode: "О-840"})
	e.event(a.director, TimelineInput{Year: 1981, Title: "Про закрытый", DocumentCode: "О-841"})
	e.event(a.director, TimelineInput{Year: 1982, Title: "Про черновик", DocumentCode: "О-842"})
	e.event(a.director, TimelineInput{Year: 1983, Title: "Про несуществующий", DocumentCode: "О-899"})

	for level := 0; level <= 7; level++ {
		items, _ := e.svc.Timeline(ctx, viewerAt(level))
		if len(items) != 4 {
			t.Fatalf("уровень %d: событий %d", level, len(items))
		}
		links := map[string]string{}
		for _, it := range items {
			if it.Document != nil {
				links[it.Title] = it.Document.Code
			}
		}
		want := map[string]string{"Про открытый": "О-840"}
		if level >= 4 {
			want["Про закрытый"] = "О-841"
		}
		if level == 7 {
			want["Про черновик"] = "О-842"
		}
		if fmt.Sprint(links) != fmt.Sprint(want) {
			t.Errorf("уровень %d: ссылки %v, ожидалось %v", level, links, want)
		}
		raw := fmt.Sprintf("%+v", items)
		if level < 4 && (strings.Contains(raw, "секретдок") || strings.Contains(raw, "О-841")) {
			t.Errorf("уровень %d: в шкале закрытый документ: %s", level, raw)
		}
		if level < 7 && (strings.Contains(raw, "секретчерновик") || strings.Contains(raw, "О-842")) {
			t.Errorf("уровень %d: в шкале черновик", level)
		}
	}
}

func TestTimelineCrudIsAuditedAndPersisted(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	o := e.event(a.director, TimelineInput{Year: 1974, Title: "Основан КУПОЛ", Body: "Совет Министров СССР."})
	if o.Author == nil || *o.Author != "director" || o.Level != 0 {
		t.Errorf("событие: %+v", o)
	}
	u, err := e.svc.TimelineUpdate(ctx, a.director, o.ID, TimelineInput{Year: 1975, Title: "Перенесено", Level: 2})
	if err != nil || u.Year != 1975 || u.Title != "Перенесено" || u.Level != 2 || u.Body != "" {
		t.Fatalf("правка: %+v %v", u, err)
	}
	if items, _ := e.svc.Timeline(ctx, Guest); len(items) != 0 {
		t.Errorf("событие уровня 2 видно гостю: %v", items)
	}
	if err := e.svc.TimelineDelete(ctx, a.director, o.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.count("SELECT count(*) FROM timeline_events"); n != 0 {
		t.Errorf("событий после удаления %d", n)
	}
	for action, want := range map[audit.Action]string{audit.TimelineCreated: "Добавлено событие хронологии", audit.TimelineUpdated: "Изменено событие хронологии", audit.TimelineDeleted: "Удалено событие хронологии"} {
		rows := e.auditRows(action)
		if len(rows) != 1 || rows[0].Title != want || rows[0].Actor == nil || *rows[0].Actor != "director" {
			t.Errorf("журнал %s: %+v", action, rows)
		}
	}
}

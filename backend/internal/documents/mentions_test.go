package documents

import (
	"fmt"
	"slices"
	"testing"
)

func linkJSON(id string, level int, target string) string {
	return fmt.Sprintf(`{"id":%q,"type":"doc_link","level":%d,"data":{"code":%q,"note":"примечание"}}`, id, level, target)
}

func mentionCodes(ms []Mention) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Code
	}
	return out
}

func (e *env) mentions(v Viewer, code string) []Mention {
	e.t.Helper()
	d, err := e.svc.Get(ctx, v, code)
	if err != nil {
		e.t.Fatalf("Get(%s) уровня %d: %v", code, v.Level(), err)
	}
	return d.MentionedIn
}

// Кто на кого ссылается и кто это видит: документ-источник и блок-ссылка проверяются отдельно.
func TestMentionsRespectSourceDocumentAndBlockLevels(t *testing.T) {
	e := newEnv(t)
	e.imp(obj("О-800", "Цель", "published", 0, blocks(para("a", nil, "текст"), linkJSON("self", 0, "О-800"))))
	e.imp(obj("О-801", "Открытый источник", "published", 0, blocks(linkJSON("l", 0, "О-800"), linkJSON("l2", 0, "О-899"))))
	e.imp(obj("О-802", "Источник уровня 3", "published", 3, blocks(linkJSON("l", 0, "О-800"))))
	e.imp(obj("О-803", "Закрытая ссылка", "published", 0, blocks(linkJSON("l", 4, "О-800"))))
	e.imp(obj("О-804", "Черновик-источник", "draft", 0, blocks(linkJSON("l", 0, "О-800"))))
	e.imp(obj("О-805", "Две ссылки", "published", 0, blocks(linkJSON("l", 0, "О-800"), linkJSON("m", 0, "О-800"))))
	e.imp(obj("О-806", "Ссылка на другое", "published", 0, blocks(linkJSON("l", 0, "О-801"))))

	for level := 0; level <= 7; level++ {
		v := viewerAt(level)
		want := []string{"О-801", "О-805"}
		if level >= 3 {
			want = append(want, "О-802")
		}
		if level >= 4 {
			want = append(want, "О-803")
		}
		if level == 7 {
			want = append(want, "О-804") // черновик — только Директорату
		}
		slices.Sort(want)
		got := mentionCodes(e.mentions(v, "О-800"))
		if !slices.Equal(got, want) {
			t.Errorf("уровень %d: упоминается в %v, ожидалось %v", level, got, want)
		}
		// названия и шифры того, что не положено, в ответе нет вовсе
		d, _ := e.svc.Get(ctx, v, "О-800")
		for _, m := range d.MentionedIn {
			if m.Slug == "" || m.TypeName != "Объект" || m.Title == "" {
				t.Errorf("уровень %d: неполное упоминание %+v", level, m)
			}
		}
	}
	// ссылка на самого себя не считается; на документ без входящих — пусто, но не ошибка
	if got := mentionCodes(e.mentions(viewerAt(7), "О-806")); len(got) != 0 {
		t.Errorf("О-806 упоминается в %v", got)
	}
	if got := mentionCodes(e.mentions(Guest, "О-801")); !slices.Equal(got, []string{"О-806"}) {
		t.Errorf("О-801 упоминается в %v", got)
	}

	// ссылка на документ, которого ещё нет, начинает работать, когда он появляется
	e.imp(obj("О-899", "Появился позже", "published", 0, blocks(para("a", nil, "текст"))))
	if got := mentionCodes(e.mentions(Guest, "О-899")); !slices.Equal(got, []string{"О-801"}) {
		t.Errorf("О-899 упоминается в %v", got)
	}

	// порядок — естественный по шифру: О-9 раньше О-10
	e.imp(obj("О-9", "Девятый", "published", 0, blocks(linkJSON("l", 0, "О-899"))))
	e.imp(obj("О-10", "Десятый", "published", 0, blocks(linkJSON("l", 0, "О-899"))))
	if got := mentionCodes(e.mentions(Guest, "О-899")); !slices.Equal(got, []string{"О-009", "О-010", "О-801"}) {
		t.Errorf("порядок: %v", got)
	}
}

func TestMentionsFollowEveryWriteAndRebuild(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	e.imp(obj("О-810", "Цель", "published", 0, blocks(para("a", nil, "текст"))))
	// источник — меморандум с шифром: у черновика Объекта шифра ещё нет, и ссылаться на него из списка нечем
	src, err := e.svc.TeamCreate(ctx, a.owner, CreateInput{Type: "memo", Code: "МЕМО-810", Content: memoContent("Источник", linkedParagraph(t, "О-810")...)})
	if err != nil {
		t.Fatal(err)
	}
	if got := mentionCodes(e.mentions(viewerAt(7), "О-810")); len(got) != 1 {
		t.Fatalf("после создания черновика: %v", got)
	}
	// сохранение без ссылки убирает упоминание, с ссылкой — возвращает
	res := mustSave(t, e, a.owner, src, memoContent("Источник", paragraph("b1", "без ссылок")))
	if got := mentionCodes(e.mentions(viewerAt(7), "О-810")); len(got) != 0 {
		t.Errorf("после сохранения без ссылки: %v", got)
	}
	res = mustSave(t, e, a.owner, res.Document, memoContent("Источник", linkedParagraph(t, "О-810")...))
	if got := mentionCodes(e.mentions(viewerAt(7), "О-810")); len(got) != 1 {
		t.Errorf("после возврата ссылки: %v", got)
	}
	// публикация: источник становится виден всем, а не только Директорату
	if got := mentionCodes(e.mentions(Guest, "О-810")); len(got) != 0 {
		t.Errorf("черновик виден гостю: %v", got)
	}
	d := e.submit(a.owner, res.Document)
	d = e.decide(a.editor, d, VerdictApprove, "")
	if got := mentionCodes(e.mentions(Guest, "О-810")); len(got) != 1 || got[0] != *d.Code {
		t.Errorf("после публикации: %v", got)
	}
	// архив скрывает упоминание, не трогая таблицу
	e.setStatus(d, StatusArchived)
	if got := mentionCodes(e.mentions(Guest, "О-810")); len(got) != 0 {
		t.Errorf("архивный источник виден гостю: %v", got)
	}

	// перестройка восстанавливает связи
	if err := e.db.Exec("DELETE FROM document_links").Error; err != nil {
		t.Fatal(err)
	}
	if err := e.db.Exec("UPDATE document_search_state SET version = 0").Error; err != nil {
		t.Fatal(err)
	}
	if got := mentionCodes(e.mentions(viewerAt(7), "О-810")); len(got) != 0 {
		t.Fatalf("без связей: %v", got)
	}
	if err := e.svc.EnsureSearchIndex(ctx); err != nil {
		t.Fatal(err)
	}
	if got := mentionCodes(e.mentions(viewerAt(7), "О-810")); len(got) != 1 {
		t.Errorf("после перестройки: %v", got)
	}

	// удаление источника убирает строки
	if err := e.svc.Delete(ctx, *d.Code); err != nil {
		t.Fatal(err)
	}
	if n := e.count("SELECT count(*) FROM document_links"); n != 0 {
		t.Errorf("после удаления источника связей %d", n)
	}
}

func linkedParagraph(t *testing.T, target string) []InputBlock {
	t.Helper()
	return []InputBlock{paragraph("b1", "текст"), linkBlock("lk", target)}
}

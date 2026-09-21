package documents

import (
	"encoding/json"
	"strings"
	"testing"
)

func (e *env) reader(login string, level int) Viewer {
	e.t.Helper()
	return Viewer{UserID: e.user(login), UserLevel: level}
}

// Лист ознакомления: сколько разных зарегистрированных читателей открыли опубликованный документ. Без имён, гости и повторные чтения не в счёте.
func TestReadCountCountsDistinctRegisteredReaders(t *testing.T) {
	e := newEnv(t)
	e.imp(obj("О-850", "Читаемый", "published", 0, blocks(para("a", nil, "текст"))))
	get := func(v Viewer) *OutDocument {
		d, err := e.svc.Get(ctx, v, "О-850")
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if n := get(Guest).ReadCount; n != 0 {
		t.Errorf("гость первым: %d", n)
	}
	a, b := e.reader("reader_a", 1), e.reader("reader_b", 2)
	if n := get(a).ReadCount; n != 1 {
		t.Errorf("первый читатель уже в счёте, получено %d", n)
	}
	if n := get(a).ReadCount; n != 1 {
		t.Errorf("повторное чтение тем же человеком: %d", n)
	}
	if n := get(b).ReadCount; n != 2 {
		t.Errorf("второй читатель: %d", n)
	}
	if n := get(Guest).ReadCount; n != 2 {
		t.Errorf("гость видит тот же счёт и в него не входит: %d", n)
	}
	// имена в ответе не появляются
	raw, _ := json.Marshal(get(Guest))
	for _, name := range []string{"reader_a", "reader_b"} {
		if strings.Contains(string(raw), name) {
			t.Errorf("в ответе есть логин читателя %s", name)
		}
	}
	// черновик читает только Директорат, и в лист ознакомления это не идёт (читаются только опубликованные)
	e.imp(obj("О-851", "Черновик", "draft", 0, blocks(para("a", nil, "текст"))))
	boss := Viewer{UserID: e.user("boss"), Directorate: true}
	if d, err := e.svc.Get(ctx, boss, "О-851"); err != nil || d.ReadCount != 0 {
		t.Errorf("черновик: %v %v", d, err)
	}
}

func TestCopyNumberIsPerReader(t *testing.T) {
	e := newEnv(t)
	e.imp(obj("О-852", "Экземпляр", "published", 0, blocks(para("a", nil, "текст"))))
	guest, _ := e.svc.Get(ctx, Guest, "О-852")
	if guest.CopyNumber != "" { // у Гражданина номера нет: интерфейс подписывает «б/н» на своём языке
		t.Errorf("гость: %q", guest.CopyNumber)
	}
	a, b := e.reader("copy_a", 1), e.reader("copy_b", 3)
	da, _ := e.svc.Get(ctx, a, "О-852")
	db, _ := e.svc.Get(ctx, b, "О-852")
	if len(da.CopyNumber) != 4 || da.CopyNumber == db.CopyNumber {
		t.Errorf("номера экземпляров: %q и %q", da.CopyNumber, db.CopyNumber)
	}
	// один и тот же читатель всегда получает свой номер
	again, _ := e.svc.Get(ctx, a, "О-852")
	if again.CopyNumber != da.CopyNumber {
		t.Errorf("номер меняется: %q → %q", da.CopyNumber, again.CopyNumber)
	}
	// уровень допуска и статус на номер не влияют
	boss := Viewer{UserID: a.UserID, Directorate: true}
	if d, _ := e.svc.Get(ctx, boss, "О-852"); d.CopyNumber != da.CopyNumber {
		t.Errorf("номер зависит от допуска: %q", d.CopyNumber)
	}
}

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kupol/internal/documents"
	"kupol/internal/testutil"
)

func newDocCLI(t *testing.T) (*documents.Service, func(args ...string) (string, error), func(name, content string) string) {
	t.Helper()
	db := testutil.NewMigratedDB(t)
	svc := documents.NewService(db, testutil.Logger(), nil)
	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := runDoc(context.Background(), svc, args, &out)
		return out.String(), err
	}
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// автор для --author
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at)
		VALUES ('Vladislav', 'h', 'b', ?, ?)`, now, now).Error; err != nil {
		t.Fatal(err)
	}
	return svc, run, write
}

const oneObject = `{"code":"О-41","type":"object","title":"Объект «Купол»","status":"published","level":0,"composed":{"year":1979},
	"blocks":[{"type":"paragraph","data":{"text":"Описание"}},{"type":"doc_link","data":{"code":"О-99"}}]}`

func TestDocImportListExport(t *testing.T) {
	svc, run, write := newDocCLI(t)
	file := write("o41.json", oneObject)

	out, err := run("import", "--author", "Vladislav", file)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"О-041", "создан", "редакция 1", "published", "Объект «Купол»", "⚠ ссылка на О-099"} {
		if !strings.Contains(out, want) {
			t.Errorf("в выводе загрузки нет %q:\n%s", want, out)
		}
	}
	d, err := svc.Get(context.Background(), documents.Guest, "О-41")
	if err != nil || d.Author == nil || *d.Author != "Vladislav" {
		t.Errorf("документ и автор: %v %+v", err, d)
	}

	// повторная загрузка — обновление
	out, err = run("import", file)
	if err != nil || !strings.Contains(out, "обновлён") || !strings.Contains(out, "редакция 2") {
		t.Errorf("повторная загрузка: %v\n%s", err, out)
	}

	out, err = run("list")
	if err != nil || !strings.Contains(out, "О-041") || !strings.Contains(out, "всего: 1") {
		t.Errorf("list: %v\n%s", err, out)
	}
	if out, _ = run("list", "--status", "draft"); !strings.Contains(out, "всего: 0") {
		t.Errorf("list --status draft: %s", out)
	}

	out, err = run("export", "o-41")
	if err != nil || !strings.Contains(out, `"code": "О-041"`) || !strings.Contains(out, "Описание") {
		t.Errorf("export: %v\n%s", err, out)
	}
	// выгрузку можно загрузить обратно
	back := write("back.json", out)
	if out, err := run("import", "--dry-run", back); err != nil || !strings.Contains(out, "будет обновлён") {
		t.Errorf("загрузка выгрузки: %v\n%s", err, out)
	}
}

func TestDocImportAutoNumberHint(t *testing.T) {
	_, run, write := newDocCLI(t)
	file := write("new.json", `{"type":"object","title":"Без номера","status":"published","composed":{"year":1980}}`)
	out, err := run("import", file)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "О-001") || !strings.Contains(out, `присвоен номер О-001: добавьте в файл "code": "О-001"`) {
		t.Errorf("подсказка про присвоенный номер:\n%s", out)
	}
}

func TestDocImportDryRunSavesNothing(t *testing.T) {
	svc, run, write := newDocCLI(t)
	out, err := run("import", "--dry-run", write("o.json", oneObject))
	if err != nil || !strings.Contains(out, "проверка пройдена") || !strings.Contains(out, "будет создан") {
		t.Errorf("dry-run: %v\n%s", err, out)
	}
	if _, err := svc.Get(context.Background(), documents.DirectorateViewer, "О-41"); err == nil {
		t.Error("пробный прогон сохранил документ")
	}
}

func TestDocImportPrintsAllProblems(t *testing.T) {
	_, run, write := newDocCLI(t)
	out, err := run("import", write("bad.json", `{"code":"О-1","type":"object","title":"","composed":{"year":1979},
		"blocks":[{"type":"hologram"},{"type":"heading","data":{"depth":9,"text":"З"}}]}`))
	if err == nil || !strings.Contains(err.Error(), "не прошёл проверку") {
		t.Fatalf("ожидалась ошибка проверки: %v", err)
	}
	for _, want := range []string{"title: не может быть пустым", "blocks[0].type: неизвестный тип блока", "blocks[1].data.depth: глубина заголовка"} {
		if !strings.Contains(out, want) {
			t.Errorf("в выводе нет %q:\n%s", want, out)
		}
	}
	// медиа-блоки — понятное сообщение
	out, _ = run("import", write("img.json", `{"code":"О-2","type":"object","title":"Т","composed":{"year":1979},"blocks":[{"type":"image"}]}`))
	if !strings.Contains(out, "загрузкой файлов (этап 6)") {
		t.Errorf("сообщение про картинки:\n%s", out)
	}
}

func TestDocSetStatusAndDelete(t *testing.T) {
	svc, run, write := newDocCLI(t)
	run("import", write("o.json", strings.Replace(oneObject, `"published"`, `"draft"`, 1)))
	if _, err := svc.Get(context.Background(), documents.Guest, "О-41"); err == nil {
		t.Fatal("черновик виден гостю")
	}

	out, err := run("set-status", "о-41", "published")
	if err != nil || !strings.Contains(out, "О-041: статус «published»") {
		t.Errorf("set-status: %v\n%s", err, out)
	}
	if _, err := svc.Get(context.Background(), documents.Guest, "О-41"); err != nil {
		t.Errorf("после публикации: %v", err)
	}
	if _, err := run("set-status", "О-41", "hidden"); err == nil {
		t.Error("неизвестный статус принят")
	}
	if _, err := run("set-status", "О-99", "published"); err == nil || !strings.Contains(err.Error(), "не найден") {
		t.Errorf("несуществующий документ: %v", err)
	}

	// удаление — только с подтверждением
	if _, err := run("delete", "О-41"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("удаление без --yes: %v", err)
	}
	if _, err := svc.Get(context.Background(), documents.Guest, "О-41"); err != nil {
		t.Error("удалено без подтверждения")
	}
	if out, err := run("delete", "О-41", "--yes"); err != nil || !strings.Contains(out, "удалён") {
		t.Errorf("delete: %v %s", err, out)
	}
	if _, err := run("delete", "О-41", "--yes"); err == nil {
		t.Error("повторное удаление должно сообщать об ошибке")
	}
}

func TestDocUsageErrors(t *testing.T) {
	_, run, _ := newDocCLI(t)
	for _, args := range [][]string{
		{}, {"import"}, {"import", "--nope", "f.json"}, {"import", "--author"}, {"export"}, {"export", "a", "b"},
		{"list", "--status"}, {"list", "x"}, {"set-status", "О-1"}, {"fly"},
	} {
		if _, err := run(args...); err == nil {
			t.Errorf("%v должно завершаться ошибкой", args)
		}
	}
	if _, err := run("import", "/нет/такого/файла.json"); err == nil {
		t.Error("несуществующий файл принят")
	}
	if _, err := run("export", "О-999"); err == nil || !strings.Contains(err.Error(), "не найден") {
		t.Errorf("export несуществующего: %v", err)
	}
}

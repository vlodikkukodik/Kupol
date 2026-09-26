package documents

import (
	"encoding/json"
	"testing"
	"time"

	"kupol/internal/audit"
)

func (e *env) auditRows(action audit.Action) []audit.Row {
	e.t.Helper()
	rows, err := audit.List(ctx, e.db, audit.Query{Action: action})
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func TestAuditRecordsBrokenLockWithBothPeople(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")
	e.setStatus(d, StatusReview)
	if _, err := e.svc.TakeLock(ctx, a.owner, d.ID); err != nil {
		t.Fatal(err)
	}

	// свой замок снимается без следа в журнале: это обычная работа
	if err := e.svc.ReleaseLock(ctx, a.owner, d.ID); err != nil {
		t.Fatal(err)
	}
	if rows := e.auditRows(audit.LockBroken); len(rows) != 0 {
		t.Fatalf("снятие своего замка попало в журнал: %+v", rows)
	}

	// чужой замок: Редактор снял замок, который держал Автор — это событие журнала
	if _, err := e.svc.TakeLock(ctx, a.owner, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ReleaseLock(ctx, a.editor, d.ID); err != nil {
		t.Fatal(err)
	}
	rows := e.auditRows(audit.LockBroken)
	if len(rows) != 1 {
		t.Fatalf("событий %d, ожидалось 1: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.Actor == nil || *r.Actor != "editor" || r.Target == nil || *r.Target != "owner" || r.DocumentID == nil || *r.DocumentID != d.ID {
		t.Errorf("кто, над кем, какой документ: %+v", r)
	}
	if r.Title != "Снят чужой замок" || !r.At.Equal(e.clock()) {
		t.Errorf("название или время: %+v", r)
	}
}

func TestAuditRecordsRollbackAndPublishedEditsButNotDraftSaves(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Версия 1")
	cur, _ := e.svc.TeamGet(ctx, a.owner, d.ID)

	// обычное сохранение черновика — не событие: оно и так в скользящих версиях
	cur = mustSave(t, e, a.owner, cur, memoContent("Версия 1б")).Document
	if err := e.svc.ReleaseLock(ctx, a.owner, d.ID); err != nil { // сохранение взяло замок Автора
		t.Fatal(err)
	}
	if n := len(e.auditRows(audit.PublishedEdited)) + len(e.auditRows(audit.DocumentRolledBack)); n != 0 {
		t.Fatalf("сохранение черновика попало в журнал: %d", n)
	}

	e.setStatus(d, StatusPublished)
	e.advance(time.Minute)
	cur, _ = e.svc.TeamGet(ctx, a.editor, d.ID)
	first := e.versions(d.ID)[0]
	cur = mustSave(t, e, a.editor, cur, memoContent("Версия 2", paragraph("b1", "Правка"))).Document

	edits := e.auditRows(audit.PublishedEdited)
	if len(edits) != 1 || edits[0].Actor == nil || *edits[0].Actor != "editor" {
		t.Fatalf("правка опубликованного: %+v", edits)
	}
	var det map[string]any
	if err := json.Unmarshal([]byte(edits[0].Details), &det); err != nil || det["status"] != "published" || det["revision"] != float64(cur.Revision) {
		t.Errorf("подробности правки: %s %v", edits[0].Details, err)
	}

	// «сохранить то же самое» — не изменение, значит и не событие
	mustSave(t, e, a.editor, cur, memoContent("Версия 2", paragraph("b1", "Правка")))
	if n := len(e.auditRows(audit.PublishedEdited)); n != 1 {
		t.Errorf("сохранение без изменений записано в журнал: %d", n)
	}

	if _, err := e.svc.TeamRestore(ctx, a.editor, d.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	rolls := e.auditRows(audit.DocumentRolledBack)
	if len(rolls) != 1 || rolls[0].Actor == nil || *rolls[0].Actor != "editor" {
		t.Fatalf("откат: %+v", rolls)
	}
	if err := json.Unmarshal([]byte(rolls[0].Details), &det); err != nil || det["version_id"] != float64(first.ID) {
		t.Errorf("подробности отката: %s %v", rolls[0].Details, err)
	}
	// откат к тому, что уже есть, — не изменение
	if _, err := e.svc.TeamRestore(ctx, a.editor, d.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(e.auditRows(audit.DocumentRolledBack)); n != 1 {
		t.Errorf("повторный откат записан в журнал: %d", n)
	}
}

func TestAuditRowsAreAnonymisedWhenTheDocumentOrPeopleAreGone(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.draft(a.owner, "Черновик")
	e.setStatus(d, StatusReview)
	if _, err := e.svc.TakeLock(ctx, a.owner, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ReleaseLock(ctx, a.editor, d.ID); err != nil {
		t.Fatal(err)
	}

	// «сдать дело в архив»: аккаунты удаляются, запись остаётся, имён в ней больше нет
	if err := e.db.Exec("DELETE FROM users WHERE id IN (?, ?)", a.owner.UserID, a.editor.UserID).Error; err != nil {
		t.Fatal(err)
	}
	rows := e.auditRows(audit.LockBroken)
	if len(rows) != 1 {
		t.Fatalf("запись пропала вместе с аккаунтами: %+v", rows)
	}
	if rows[0].Actor != nil || rows[0].Target != nil {
		t.Errorf("имена остались после удаления аккаунтов: %+v", rows[0])
	}
}

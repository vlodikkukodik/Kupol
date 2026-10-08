package documents

import "testing"

// TestTeamDeleteRemovesDocumentAndFreesCode — Директорат и Редактор (CanEditPublished) удаляют документ
// безвозвратно; шифр после удаления сразу свободен для нового дела.
func TestTeamDeleteRemovesDocumentAndFreesCode(t *testing.T) {
	e := newEnv(t)
	acts := e.actors()
	e.imp(`{"code":"О-1","type":"object","title":"Удаляемое","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},"blocks":[]}`)
	var id int64
	if err := e.db.Raw(`SELECT id FROM documents WHERE code = ?`, "О-001").Scan(&id).Error; err != nil {
		t.Fatal(err)
	}

	if err := e.svc.TeamDelete(ctx, acts.director, id); err != nil {
		t.Fatalf("Директорат: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM documents WHERE id = ?`, id); n != 0 {
		t.Fatalf("документ не удалён")
	}

	// шифр свободен: тот же код снова годится для импорта
	rep := e.imp(`{"code":"О-1","type":"object","title":"Новое дело на том же шифре","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},"blocks":[]}`)
	if len(rep.Items) != 1 || rep.Items[0].Code != "О-001" {
		t.Fatalf("шифр не освободился: %+v", rep)
	}

	// Редактор (CanEditPublished) тоже вправе удалить
	var id2 int64
	if err := e.db.Raw(`SELECT id FROM documents WHERE code = ?`, "О-001").Scan(&id2).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.svc.TeamDelete(ctx, acts.editor, id2); err != nil {
		t.Fatalf("Редактор: %v", err)
	}
}

// TestTeamDeleteForbiddenWithoutRight — автор своего же черновика без CanEditPublished не может удалить.
func TestTeamDeleteForbiddenWithoutRight(t *testing.T) {
	e := newEnv(t)
	acts := e.actors()
	d := e.draft(acts.owner, "Черновик")
	if err := e.svc.TeamDelete(ctx, acts.owner, d.ID); err != ErrForbidden {
		t.Fatalf("владелец: ожидался ErrForbidden, получено %v", err)
	}
	// «other» не видит чужой черновик вовсе (CanView возвращает ErrNotFound раньше проверки права удалять)
	if err := e.svc.TeamDelete(ctx, acts.other, d.ID); err != ErrNotFound {
		t.Fatalf("другой автор: ожидался ErrNotFound, получено %v", err)
	}
}

// TestTeamDeleteRespectsLock — документ, взятый в работу другим человеком, не удаляется.
func TestTeamDeleteRespectsLock(t *testing.T) {
	e := newEnv(t)
	acts := e.actors()
	e.imp(`{"code":"О-1","type":"object","title":"Дело","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},"blocks":[]}`)
	var id int64
	if err := e.db.Raw(`SELECT id FROM documents WHERE code = ?`, "О-001").Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.TeamSave(ctx, acts.editor, id, 1, Content{Title: "Дело", Composed: &Composed{Year: 1981}, Blocks: []InputBlock{}}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.TeamDelete(ctx, acts.director, id); !isLocked(err) {
		t.Fatalf("ожидался LockedError, получено %v", err)
	}
}

func isLocked(err error) bool {
	_, ok := err.(*LockedError)
	return ok
}

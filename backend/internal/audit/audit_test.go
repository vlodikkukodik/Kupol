package audit_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"kupol/internal/audit"
	"kupol/internal/testutil"
)

var ctx = context.Background()

func TestRecordAndListNewestFirst(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	var admin, user int64
	for login, dst := range map[string]*int64{"admin": &admin, "vera": &user} {
		if err := db.Raw(`INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at)
			VALUES (?, 'h', 'b', ?, ?) RETURNING id`, login, now, now).Scan(dst).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := audit.Record(db, now, audit.RoleGranted, audit.Event{ActorID: &admin, TargetUserID: &user, Details: audit.Details("role", "editor")}); err != nil {
		t.Fatal(err)
	}
	if err := audit.Record(db, now.Add(time.Minute), audit.RoleRevoked, audit.Event{ActorID: &admin, TargetUserID: &user}); err != nil {
		t.Fatal(err)
	}
	if err := audit.Record(db, now.Add(2*time.Minute), audit.PasswordReset, audit.Event{TargetUserID: &user}); err != nil {
		t.Fatal(err)
	}

	rows, err := audit.List(ctx, db, audit.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Action != "account.password_reset" || rows[2].Action != "role.granted" {
		t.Fatalf("порядок или число записей: %+v", rows)
	}
	if rows[0].Actor != nil || rows[0].Target == nil || *rows[0].Target != "vera" {
		t.Errorf("сброс пароля командой на сервере: без исполнителя, над vera: %+v", rows[0])
	}
	if rows[2].Actor == nil || *rows[2].Actor != "admin" || rows[2].Title != "Выдана роль" {
		t.Errorf("выдача роли: %+v", rows[2])
	}
	var det map[string]string
	if err := json.Unmarshal([]byte(rows[2].Details), &det); err != nil || det["role"] != "editor" {
		t.Errorf("подробности: %s %v", rows[2].Details, err)
	}
	if rows[1].Details != "{}" {
		t.Errorf("подробности по умолчанию — пустой объект: %q", rows[1].Details)
	}

	// отбор по действию и предел выборки
	only, err := audit.List(ctx, db, audit.Query{Action: audit.RoleRevoked})
	if err != nil || len(only) != 1 || only[0].Action != "role.revoked" {
		t.Errorf("отбор по действию: %+v %v", only, err)
	}
	one, err := audit.List(ctx, db, audit.Query{Limit: 1})
	if err != nil || len(one) != 1 || one[0].Action != "account.password_reset" {
		t.Errorf("Limit: %+v %v", one, err)
	}
	for _, bad := range []int{-1, 501} {
		if _, err := audit.List(ctx, db, audit.Query{Limit: bad}); err == nil {
			t.Errorf("Limit %d должен отвергаться", bad)
		}
	}
}

func TestRecordRejectsBrokenEvents(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	now := time.Now()
	if err := audit.Record(db, now, "", audit.Event{}); err == nil {
		t.Error("пустое действие должно отвергаться")
	}
	if err := audit.Record(db, now, audit.Action(strings.Repeat("x", 65)), audit.Event{}); err == nil {
		t.Error("слишком длинное действие должно отвергаться базой")
	}
	if err := audit.Record(db, now, audit.RoleGranted, audit.Event{Details: `["не", "объект"]`}); err == nil {
		t.Error("подробности — только JSON-объект")
	}
	missing := int64(999999)
	if err := audit.Record(db, now, audit.RoleGranted, audit.Event{ActorID: &missing}); err == nil {
		t.Error("несуществующий исполнитель должен отвергаться")
	}
}

func TestRecordIsPartOfTheTransaction(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	tx := db.Begin()
	if err := audit.Record(tx, time.Now(), audit.RoleGranted, audit.Event{}); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	rows, err := audit.List(ctx, db, audit.Query{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("запись пережила откат транзакции: %+v %v", rows, err)
	}
}

func TestDetailsBuildsJSONAndTitlesAreKnown(t *testing.T) {
	if got := audit.Details("a", 1, "b", "x"); got != `{"a":1,"b":"x"}` {
		t.Errorf("Details: %s", got)
	}
	for _, a := range []audit.Action{
		audit.RoleGranted, audit.RoleRevoked, audit.LockBroken, audit.DocumentRolledBack,
		audit.PublishedEdited, audit.PasswordChanged, audit.AccessRestored, audit.PasswordReset,
	} {
		if a.Title() == string(a) {
			t.Errorf("у события %s нет человеческого названия", a)
		}
	}
	if audit.Action("custom.thing").Title() != "custom.thing" {
		t.Error("неизвестное событие показывается машинным именем")
	}
}

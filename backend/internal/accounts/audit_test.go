package accounts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"kupol/internal/audit"
)

func (e *env) auditRows(action audit.Action) []audit.Row {
	e.t.Helper()
	rows, err := audit.List(context.Background(), e.db, audit.Query{Action: action, Limit: 100})
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func TestAuditRecordsRoleChangesOnlyWhenTheyHappen(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.register("Vladislav", "секретный пароль 1")
	admin := e.register("Direktor", "секретный пароль 2")
	by := e.userID(admin.User.Login)

	if _, _, err := e.svc.GrantRole(ctx, "vladislav", RoleEditor, &by); err != nil {
		t.Fatal(err)
	}
	// повторная выдача ничего не меняет — значит, и в журнале ничего нового
	if _, changed, _ := e.svc.GrantRole(ctx, "vladislav", RoleEditor, &by); changed {
		t.Fatal("повторная выдача изменила роли")
	}
	granted := e.auditRows(audit.RoleGranted)
	if len(granted) != 1 {
		t.Fatalf("выдач в журнале %d, ожидалась 1: %+v", len(granted), granted)
	}
	g := granted[0]
	var det map[string]string
	if err := json.Unmarshal([]byte(g.Details), &det); err != nil || det["role"] != "editor" {
		t.Errorf("подробности: %s %v", g.Details, err)
	}
	if g.Actor == nil || *g.Actor != "Direktor" || g.Target == nil || *g.Target != "Vladislav" {
		t.Errorf("кто и кому: %+v", g)
	}

	// выдача командой на сервере — без исполнителя
	if _, _, err := e.svc.GrantRole(ctx, "vladislav", RoleAuthor, nil); err != nil {
		t.Fatal(err)
	}
	var byServer *audit.Row
	for _, r := range e.auditRows(audit.RoleGranted) {
		if r.Actor == nil {
			byServer = &r
		}
	}
	if byServer == nil {
		t.Error("выдача командой на сервере не записана")
	}

	if _, _, err := e.svc.RevokeRole(ctx, "vladislav", RoleEditor, &by); err != nil {
		t.Fatal(err)
	}
	if _, changed, _ := e.svc.RevokeRole(ctx, "vladislav", RoleEditor, &by); changed {
		t.Fatal("повторное снятие изменило роли")
	}
	if rows := e.auditRows(audit.RoleRevoked); len(rows) != 1 || rows[0].Title != "Снята роль" {
		t.Errorf("снятия в журнале: %+v", rows)
	}
}

func TestAuditRecordsPasswordEventsAndNeverSecrets(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	reg := e.register("Vladislav", "старый пароль")
	cur, err := e.svc.Authenticate(ctx, reg.Token)
	if err != nil {
		t.Fatal(err)
	}

	if err := e.svc.ChangePassword(ctx, reg.User.ID, cur.Session.ID, "старый пароль", "новый пароль 2", e.freshIP()); err != nil {
		t.Fatal(err)
	}
	// неудачная смена (неверный текущий пароль) — не событие журнала
	if err := e.svc.ChangePassword(ctx, reg.User.ID, cur.Session.ID, "не тот", "ещё пароль 3", e.freshIP()); err == nil {
		t.Fatal("смена с неверным паролем прошла")
	}
	if rows := e.auditRows(audit.PasswordChanged); len(rows) != 1 || rows[0].Actor == nil || *rows[0].Actor != "Vladislav" {
		t.Fatalf("смена пароля: %+v", rows)
	}

	// восстановление по резервному коду
	if _, err := e.svc.RestoreAccess(ctx, "Vladislav", reg.BackupCode, "после восстановления 4", e.freshIP()); err != nil {
		t.Fatal(err)
	}
	if rows := e.auditRows(audit.AccessRestored); len(rows) != 1 {
		t.Errorf("восстановление: %+v", rows)
	}
	// неверный код — не событие
	if _, err := e.svc.RestoreAccess(ctx, "Vladislav", "KUPOL-AAAA-AAAA-AAAA-AAAA", "ещё другой пароль 5", e.freshIP()); err == nil {
		t.Fatal("восстановление по чужому коду прошло")
	}
	if rows := e.auditRows(audit.AccessRestored); len(rows) != 1 {
		t.Errorf("неудачное восстановление попало в журнал: %+v", rows)
	}

	// сброс пароля автором на сервере — без исполнителя
	temp, err := e.svc.AdminResetPassword(ctx, "Vladislav")
	if err != nil {
		t.Fatal(err)
	}
	rows := e.auditRows(audit.PasswordReset)
	if len(rows) != 1 || rows[0].Actor != nil || rows[0].Target == nil || *rows[0].Target != "Vladislav" {
		t.Errorf("сброс: %+v", rows)
	}

	// ни пароли, ни коды в журнал не попадают
	all, err := audit.List(ctx, e.db, audit.Query{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	dump, _ := json.Marshal(all)
	for _, secret := range []string{"старый пароль", "новый пароль 2", "после восстановления 4", temp, reg.BackupCode} {
		if strings.Contains(string(dump), secret) {
			t.Errorf("в журнале обнаружен секрет %q", secret)
		}
	}
}

func TestAuditIsAnonymisedWhenTheAccountIsDeleted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	reg := e.register("Vladislav", "секретный пароль 1")
	if err := e.svc.DeleteAccount(ctx, reg.User.ID, "секретный пароль 1", e.freshIP()); err != nil {
		t.Fatal(err)
	}
	// событие остаётся, а имени в нём больше нет
	e.register("Second", "секретный пароль 2")
	e2 := e.userID("Second")
	if _, _, err := e.svc.GrantRole(ctx, "Second", RoleAuthor, &e2); err != nil {
		t.Fatal(err)
	}
	if err := e.db.Exec("DELETE FROM users WHERE id = ?", e2).Error; err != nil {
		t.Fatal(err)
	}
	rows := e.auditRows(audit.RoleGranted)
	if len(rows) != 1 || rows[0].Actor != nil || rows[0].Target != nil {
		t.Errorf("после удаления аккаунта имена должны исчезнуть, запись — остаться: %+v", rows)
	}
}

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"kupol/internal/accounts"
	"kupol/internal/config"
	"kupol/internal/testutil"
)

func TestAuditCommand(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewMigratedDB(t)
	svc, _, err := newAccounts(config.Config{Limits: config.DefaultLimits()}, db, testutil.Logger())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at)
		VALUES ('Vladislav', 'x', 'y', ?, ?)`, now, now).Error; err != nil {
		t.Fatal(err)
	}
	// роль выдаётся и снимается так, как это делает автор на сервере: без исполнителя
	if _, _, err := svc.GrantRole(ctx, "Vladislav", accounts.RoleEditor, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.RevokeRole(ctx, "Vladislav", accounts.RoleEditor, nil); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := runAudit(ctx, db, args, &out)
		return out.String(), err
	}

	out, err := run()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"role.granted", "role.revoked", "исполнитель: сервер", "над: Vladislav", `{"role": "editor"}`} {
		if !strings.Contains(out, want) {
			t.Errorf("в журнале нет %q:\n%s", want, out)
		}
	}
	// новые сверху
	if strings.Index(out, "role.revoked") > strings.Index(out, "role.granted") {
		t.Errorf("порядок: новые должны быть сверху:\n%s", out)
	}

	only, err := run("--action", "role.granted", "--limit", "5")
	if err != nil || strings.Contains(only, "role.revoked") || !strings.Contains(only, "role.granted") {
		t.Errorf("отбор по событию: %v\n%s", err, only)
	}
	none, err := run("--action", "lock.broken")
	if err != nil || !strings.Contains(none, "журнал пуст") {
		t.Errorf("пустой отбор: %v\n%s", err, none)
	}

	for _, bad := range [][]string{{"--limit"}, {"--limit", "0"}, {"--limit", "501"}, {"--limit", "x"}, {"--doc", "-1"}, {"--nope", "1"}} {
		if _, err := run(bad...); err == nil {
			t.Errorf("%v должно отвергаться", bad)
		}
	}
}

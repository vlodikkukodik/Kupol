package main

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"kupol/internal/accounts"
	"kupol/internal/config"
	"kupol/internal/testutil"
)

func newUserCLI(t *testing.T) (*accounts.Service, func(args ...string) (string, error)) {
	t.Helper()
	db := testutil.NewMigratedDB(t)
	cfg := config.Config{Limits: config.DefaultLimits()}
	svc, _, err := newAccounts(cfg, db, testutil.Logger())
	if err != nil {
		t.Fatal(err)
	}
	// Пользователь создаётся прямой вставкой: CLI пароль не проверяет, а сброс задаёт новый.
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at)
		VALUES ('Vladislav', 'x', 'y', ?, ?)`, now, now).Error; err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := runUser(context.Background(), svc, args, &out)
		return out.String(), err
	}
	return svc, run
}

func TestUserShow(t *testing.T) {
	_, run := newUserCLI(t)
	out, err := run("show", "vladislav") // без учёта регистра
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Vladislav", "1 (Посетитель)", "Директорат:   false", "последний вход:  —"} {
		if !strings.Contains(out, want) {
			t.Errorf("в выводе нет %q:\n%s", want, out)
		}
	}
	if _, err := run("show", "nobody"); err == nil || !strings.Contains(err.Error(), `"nobody" не найден`) {
		t.Errorf("несуществующий пользователь: %v", err)
	}
}

func TestUserSetLevel(t *testing.T) {
	svc, run := newUserCLI(t)
	out, err := run("set-level", "Vladislav", "5")
	if err != nil || !strings.Contains(out, "уровень 5 (Куратор)") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	u, _ := svc.Find(context.Background(), "Vladislav")
	if u.Level != 5 {
		t.Errorf("в БД уровень %d", u.Level)
	}
	for _, bad := range [][]string{
		{"set-level", "Vladislav", "0"}, {"set-level", "Vladislav", "7"}, {"set-level", "Vladislav", "много"},
		{"set-level", "Vladislav"}, {"set-level", "nobody", "3"},
	} {
		if _, err := run(bad...); err == nil {
			t.Errorf("%v должно завершаться ошибкой", bad)
		}
	}
	if u, _ = svc.Find(context.Background(), "Vladislav"); u.Level != 5 {
		t.Errorf("неудачные команды изменили уровень: %d", u.Level)
	}
}

func TestUserSetDirectorate(t *testing.T) {
	svc, run := newUserCLI(t)
	for arg, wantFlag := range map[string]bool{"on": true, "OFF": false, "On": true} {
		out, err := run("set-directorate", "Vladislav", arg)
		if err != nil {
			t.Fatalf("%s: %v", arg, err)
		}
		if u, _ := svc.Find(context.Background(), "Vladislav"); u.Directorate != wantFlag {
			t.Errorf("%s: Directorate = %v, ожидалось %v (вывод %q)", arg, u.Directorate, wantFlag, out)
		}
	}
	if _, err := run("set-directorate", "Vladislav", "yes"); err == nil {
		t.Error("значение, кроме on/off, должно отвергаться")
	}
	if _, err := run("set-directorate", "Vladislav"); err == nil {
		t.Error("без аргумента on/off должно отвергаться")
	}
}

func TestUserSetRole(t *testing.T) {
	svc, run := newUserCLI(t)
	roles := func() []accounts.Role {
		m, err := svc.MemberByLogin(context.Background(), "Vladislav")
		if err != nil {
			t.Fatal(err)
		}
		return m.Roles
	}

	out, err := run("set-role", "vladislav", "editor", "on") // логин без учёта регистра
	if err != nil || !strings.Contains(out, "роль editor (Редактор) выдана") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if out, err = run("set-role", "Vladislav", "author", "ON"); err != nil || !strings.Contains(out, "роль author (Автор) выдана") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got := roles(); len(got) != 2 || got[0] != accounts.RoleAuthor || got[1] != accounts.RoleEditor {
		t.Errorf("роли %v", got)
	}

	// повтор — не ошибка, но CLI честно говорит, что ничего не изменилось
	if out, err = run("set-role", "Vladislav", "editor", "on"); err != nil || !strings.Contains(out, "ничего не изменилось") {
		t.Errorf("повторная выдача: out=%q err=%v", out, err)
	}

	// show показывает роли
	if out, err = run("show", "Vladislav"); err != nil || !strings.Contains(out, "author (Автор), editor (Редактор)") {
		t.Errorf("show: out=%q err=%v", out, err)
	}

	if out, err = run("set-role", "Vladislav", "editor", "off"); err != nil || !strings.Contains(out, "роль editor (Редактор) снята") {
		t.Fatalf("снятие: out=%q err=%v", out, err)
	}
	if out, err = run("set-role", "Vladislav", "editor", "off"); err != nil || !strings.Contains(out, "ничего не изменилось") {
		t.Errorf("повторное снятие: out=%q err=%v", out, err)
	}
	if got := roles(); len(got) != 1 || got[0] != accounts.RoleAuthor {
		t.Errorf("роли после снятия: %v", got)
	}

	for _, bad := range [][]string{
		{"set-role", "Vladislav", "admin", "on"},
		{"set-role", "Vladislav", "directorate", "on"}, // Директорат выдаётся set-directorate, не ролью
		{"set-role", "Vladislav", "Editor", "on"},
		{"set-role", "Vladislav", "editor", "yes"},
		{"set-role", "Vladislav", "editor"},
		{"set-role", "Vladislav"},
		{"set-role", "nobody", "editor", "on"},
	} {
		if _, err := run(bad...); err == nil {
			t.Errorf("%v должно завершаться ошибкой", bad)
		}
	}
	if got := roles(); len(got) != 1 {
		t.Errorf("неудачные команды изменили роли: %v", got)
	}
	// у пользователя без ролей show печатает прочерк
	if _, err := run("set-role", "Vladislav", "author", "off"); err != nil {
		t.Fatal(err)
	}
	if out, _ = run("show", "Vladislav"); !strings.Contains(out, "роли:         —") {
		t.Errorf("show без ролей:\n%s", out)
	}
}

func TestUserResetPassword(t *testing.T) {
	svc, run := newUserCLI(t)
	out, err := run("reset-password", "vladislav")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`Временный пароль \(показывается один раз\): (\S{16})\n`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("в выводе нет временного пароля:\n%s", out)
	}
	res, err := svc.Login(context.Background(), "Vladislav", m[1], accounts.ClientInfo{IP: "203.0.113.5"})
	if err != nil {
		t.Fatalf("вход с временным паролем: %v", err)
	}
	if res.User.Login != "Vladislav" {
		t.Errorf("вошёл %q", res.User.Login)
	}
}

func TestUserUsageErrors(t *testing.T) {
	_, run := newUserCLI(t)
	for _, args := range [][]string{
		{}, {"show"}, {"show", "a", "b"}, {"reset-password", "Vladislav", "extra"}, {"fly", "Vladislav"},
	} {
		if _, err := run(args...); err == nil {
			t.Errorf("%v должно завершаться ошибкой", args)
		}
	}
	_, err := run("fly", "Vladislav")
	if err == nil || !strings.Contains(err.Error(), "неизвестная команда") || !strings.Contains(err.Error(), "использование") {
		t.Errorf("неизвестная команда должна печатать справку: %v", err)
	}
}

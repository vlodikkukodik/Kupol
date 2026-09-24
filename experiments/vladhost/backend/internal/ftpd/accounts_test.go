package ftpd_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jlaffaye/ftp"

	"vladhost/internal/sites"
)

var bg = context.Background()

// account заводит дополнительный аккаунт сайта фикстуры и возвращает логин и пароль.
func (f *fixture) account(name, dir string, readOnly bool) (id int64, login, password string) {
	f.t.Helper()
	a, site, pw, err := f.svc.CreateFTPAccount(bg, f.site.UserID, f.site.ID, name, dir, readOnly)
	if err != nil {
		f.t.Fatalf("создание аккаунта %q: %v", name, err)
	}
	return a.ID, f.svc.FTPAccountUsername(site.Host, a.Name), pw
}

func (f *fixture) loginAs(user, pass string) *ftp.ServerConn {
	f.t.Helper()
	c, err := f.dial(user, pass)
	if err != nil {
		f.t.Fatalf("вход %s: %v", user, err)
	}
	f.t.Cleanup(func() { _ = c.Quit() })
	return c
}

func names(entries []*ftp.Entry) []string {
	var out []string
	for _, e := range entries {
		if e.Name != "." && e.Name != ".." {
			out = append(out, e.Name)
		}
	}
	return out
}

func TestAccountIsConfinedToItsFolder(t *testing.T) {
	f := setup(t, 1<<20)
	if err := os.MkdirAll(f.pub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.pub, "index.html"), []byte("main"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, login, pw := f.account("deploy", "app/dist", false)
	if login != "deploy.blog.john" {
		t.Fatalf("логин %q", login)
	}
	// Папка создаётся вместе с аккаунтом.
	if fi, err := os.Stat(filepath.Join(f.pub, "app", "dist")); err != nil || !fi.IsDir() {
		t.Fatalf("папка аккаунта не создана: %v", err)
	}

	c := f.loginAs(login, pw)
	if err := c.Stor("page.html", strings.NewReader("hi")); err != nil {
		t.Fatalf("STOR: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.pub, "app", "dist", "page.html")); string(b) != "hi" {
		t.Fatal("файл должен лечь в папку аккаунта")
	}
	list, err := c.List("")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(list); len(got) != 1 || got[0] != "page.html" {
		t.Fatalf("аккаунт видит только свою папку, а увидел %v", got)
	}
	// Файлов основного сайта не видно и выйти наверх нельзя.
	for _, p := range []string{"../index.html", "/../index.html", "../../index.html", "index.html", "/app/dist/../../index.html"} {
		if r, err := c.Retr(p); err == nil {
			b, _ := io.ReadAll(r)
			_ = r.Close()
			t.Errorf("RETR %q вернул %q", p, b)
		}
	}
	// «..» у корня FTP-каталога остаётся в корне (как chroot): файл может лечь только внутрь папки аккаунта.
	_ = c.Stor("../escaped.html", strings.NewReader("x"))
	if _, err := os.Stat(filepath.Join(f.pub, "escaped.html")); err == nil {
		t.Fatal("файл оказался вне папки аккаунта")
	}
	if _, err := os.Stat(filepath.Join(f.pub, "app", "escaped.html")); err == nil {
		t.Fatal("файл оказался выше папки аккаунта")
	}

	// Основной доступ видит всё, включая файлы аккаунта.
	m := f.login()
	if r, err := m.Retr("app/dist/page.html"); err != nil {
		t.Fatalf("основной доступ: %v", err)
	} else {
		_ = r.Close()
	}
}

func TestAccountWholeSiteWhenNoFolder(t *testing.T) {
	f := setup(t, 1<<20)
	_, login, pw := f.account("all", "", false)
	c := f.loginAs(login, pw)
	if err := c.Stor("index.html", strings.NewReader("v")); err != nil || f.disk("index.html") != "v" {
		t.Fatalf("аккаунт без папки работает с корнем сайта: %v", err)
	}
}

func TestReadOnlyAccount(t *testing.T) {
	f := setup(t, 1<<20)
	if err := os.MkdirAll(filepath.Join(f.pub, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.pub, "docs", "a.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, login, pw := f.account("viewer", "docs", true)
	c := f.loginAs(login, pw)

	r, err := c.Retr("a.txt")
	if err != nil {
		t.Fatalf("чтение должно работать: %v", err)
	}
	if b, _ := io.ReadAll(r); string(b) != "data" {
		t.Fatalf("прочитано %q", b)
	}
	_ = r.Close()
	if _, err := c.List(""); err != nil {
		t.Fatalf("список: %v", err)
	}

	if err := c.Stor("new.txt", strings.NewReader("x")); err == nil {
		t.Error("STOR прошёл")
	}
	if err := c.Stor("a.txt", strings.NewReader("overwritten")); err == nil {
		t.Error("перезапись прошла")
	}
	if err := c.Append("a.txt", strings.NewReader("more")); err == nil {
		t.Error("APPE прошёл")
	}
	if err := c.MakeDir("sub"); err == nil {
		t.Error("MKD прошёл")
	}
	if err := c.Delete("a.txt"); err == nil {
		t.Error("DELE прошёл")
	}
	if err := c.Rename("a.txt", "b.txt"); err == nil {
		t.Error("RNFR/RNTO прошёл")
	}
	if b, _ := os.ReadFile(filepath.Join(f.pub, "docs", "a.txt")); string(b) != "data" {
		t.Fatalf("файл изменён: %q", b)
	}
	if _, err := os.Stat(filepath.Join(f.pub, "docs", "sub")); err == nil {
		t.Fatal("папка создана")
	}
}

func TestDisabledAccountCannotLoginAndOpenSessionsClose(t *testing.T) {
	f := setup(t, 1<<20)
	id, login, pw := f.account("temp", "", false)
	c := f.loginAs(login, pw)
	if err := c.Stor("a.txt", strings.NewReader("1")); err != nil {
		t.Fatal(err)
	}

	off := false
	if _, _, err := f.svc.UpdateFTPAccount(bg, f.site.UserID, f.site.ID, id, sites.FTPAccountPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if err := c.Stor("b.txt", strings.NewReader("2")); err == nil {
		t.Error("открытая сессия отключённого аккаунта продолжила работать")
	}
	if _, err := f.dial(login, pw); err == nil {
		t.Error("отключённый аккаунт вошёл")
	}
	// Основной доступ не пострадал.
	f.login()

	on := true
	if _, _, err := f.svc.UpdateFTPAccount(bg, f.site.UserID, f.site.ID, id, sites.FTPAccountPatch{Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	f.loginAs(login, pw)
}

func TestChangingModeOrFolderClosesSessions(t *testing.T) {
	f := setup(t, 1<<20)
	id, login, pw := f.account("dev", "a", false)
	c := f.loginAs(login, pw)
	ro := true
	if _, _, err := f.svc.UpdateFTPAccount(bg, f.site.UserID, f.site.ID, id, sites.FTPAccountPatch{ReadOnly: &ro}); err != nil {
		t.Fatal(err)
	}
	if err := c.Stor("x.txt", strings.NewReader("1")); err == nil {
		t.Error("сессия со старыми правами продолжила писать")
	}
	c2 := f.loginAs(login, pw)
	if err := c2.Stor("x.txt", strings.NewReader("1")); err == nil {
		t.Error("новая сессия обязана быть только для чтения")
	}

	dir := "b/c"
	if _, _, err := f.svc.UpdateFTPAccount(bg, f.site.UserID, f.site.ID, id, sites.FTPAccountPatch{Dir: &dir, ReadOnly: new(bool)}); err != nil {
		t.Fatal(err)
	}
	c3 := f.loginAs(login, pw)
	if err := c3.Stor("y.txt", strings.NewReader("2")); err != nil {
		t.Fatal(err)
	}
	if f.disk("b/c/y.txt") != "2" {
		t.Fatal("после смены папки файл должен лечь в новую папку")
	}
}

func TestPasswordResetInvalidatesOldPasswordAndSessions(t *testing.T) {
	f := setup(t, 1<<20)
	id, login, oldPw := f.account("ci", "", false)
	c := f.loginAs(login, oldPw)
	_, _, newPw, err := f.svc.ResetFTPAccountPassword(bg, f.site.UserID, f.site.ID, id)
	if err != nil || newPw == "" || newPw == oldPw {
		t.Fatalf("новый пароль: %q %v", newPw, err)
	}
	if err := c.Stor("a.txt", strings.NewReader("1")); err == nil {
		t.Error("сессия со старым паролем продолжила работать")
	}
	if _, err := f.dial(login, oldPw); err == nil {
		t.Error("старый пароль ещё действует")
	}
	f.loginAs(login, newPw)
}

func TestDeletedAccountCannotLogin(t *testing.T) {
	f := setup(t, 1<<20)
	id, login, pw := f.account("gone", "", false)
	c := f.loginAs(login, pw)
	if err := f.svc.DeleteFTPAccount(bg, f.site.UserID, f.site.ID, id); err != nil {
		t.Fatal(err)
	}
	if err := c.Stor("a.txt", strings.NewReader("1")); err == nil {
		t.Error("сессия удалённого аккаунта работает")
	}
	if _, err := f.dial(login, pw); err == nil {
		t.Error("удалённый аккаунт вошёл")
	}
	if err := f.svc.DeleteFTPAccount(bg, f.site.UserID, f.site.ID, id); !errors.Is(err, sites.ErrFTPAccountAbsent) {
		t.Errorf("повторное удаление: %v", err)
	}
}

func TestSiteDeletionClosesAccountSessions(t *testing.T) {
	f := setup(t, 1<<20)
	_, login, pw := f.account("x", "", false)
	c := f.loginAs(login, pw)
	if err := f.svc.Delete(bg, f.site.UserID, f.site.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.Stor("a.txt", strings.NewReader("1")); err == nil {
		t.Error("сессия аккаунта удалённого сайта работает")
	}
	if _, err := f.dial(login, pw); err == nil {
		t.Error("аккаунт удалённого сайта вошёл")
	}
}

func TestCredentialsAreNotInterchangeable(t *testing.T) {
	f := setup(t, 1<<20)
	_, login, pw := f.account("deploy", "", false)
	for _, c := range []struct{ user, pass string }{
		{login, f.password},            // пароль основного доступа к аккаунту
		{f.user, pw},                   // пароль аккаунта к основному логину
		{login, "wrong"},               // неверный пароль
		{"nobody.blog.john", pw},       // нет такого аккаунта
		{"deploy.blog.mary", pw},       // чужой сайт с тем же именем аккаунта
		{"DEPLOY.BLOG.JOHN ", "wrong"}, // регистр и пробелы нормализуются, но пароль всё равно неверный
		{"deploy..blog.john", pw},
		{"deploy.blog.john.extra", pw},
	} {
		if _, err := f.dial(c.user, c.pass); err == nil {
			t.Errorf("вход %q прошёл", c.user)
		}
	}
	// Регистр логина не важен, как и у основного доступа.
	f.loginAs("Deploy.Blog.John", pw)
}

func TestMainAccessChangesDoNotAffectAccounts(t *testing.T) {
	f := setup(t, 1<<20)
	_, login, pw := f.account("keep", "", false)
	c := f.loginAs(login, pw)
	if _, err := f.svc.DisableFTP(bg, f.site.UserID, f.site.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.Stor("a.txt", strings.NewReader("1")); err != nil {
		t.Fatalf("отключение основного доступа не должно рвать дополнительные аккаунты: %v", err)
	}
	if _, err := f.dial(f.user, f.password); err == nil {
		t.Fatal("основной доступ должен быть отключён")
	}
}

func TestAccountFolderIsRecreatedAfterDeletion(t *testing.T) {
	f := setup(t, 1<<20)
	_, login, pw := f.account("web", "site", false)
	c := f.loginAs(login, pw)
	if err := os.RemoveAll(filepath.Join(f.pub, "site")); err != nil { // как после деплоя архивом без этой папки
		t.Fatal(err)
	}
	if err := c.Stor("a.txt", strings.NewReader("1")); err != nil {
		t.Fatalf("папка аккаунта должна создаваться заново: %v", err)
	}
	if f.disk("site/a.txt") != "1" {
		t.Fatal("файл не в папке аккаунта")
	}
}

func TestAccountCannotFollowSymlinkOutOfItsFolder(t *testing.T) {
	f := setup(t, 1<<20)
	_, login, pw := f.account("s", "sub", false)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(f.pub, "sub", "link")); err != nil {
		t.Skip("симлинки недоступны:", err)
	}
	// Ссылка на папку рядом внутри сайта, но вне папки аккаунта.
	if err := os.WriteFile(filepath.Join(f.pub, "top.txt"), []byte("top"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.pub, "top.txt"), filepath.Join(f.pub, "sub", "toplink")); err != nil {
		t.Fatal(err)
	}
	c := f.loginAs(login, pw)
	for _, p := range []string{"link/secret.txt", "toplink"} {
		if r, err := c.Retr(p); err == nil {
			b, _ := io.ReadAll(r)
			_ = r.Close()
			t.Errorf("RETR %q через ссылку вернул %q", p, b)
		}
	}
	if err := c.Stor("link/planted.txt", strings.NewReader("x")); err == nil {
		t.Error("запись через ссылку наружу прошла")
	}
	if _, err := os.Stat(filepath.Join(outside, "planted.txt")); err == nil {
		t.Fatal("файл записан за пределы сайта")
	}
}

func TestAccountValidationAndLimits(t *testing.T) {
	f := setup(t, 1<<20)
	uid, sid := f.site.UserID, f.site.ID

	for _, name := range []string{"", "-a", "a-", "a.b", "A b", "a_b", strings.Repeat("a", 25), "имя", "a/b", ".."} {
		if _, _, _, err := f.svc.CreateFTPAccount(bg, uid, sid, name, "", false); !errors.Is(err, sites.ErrFTPNameInvalid) {
			t.Errorf("имя %q: %v", name, err)
		}
	}
	for _, name := range []string{"a", "1", "deploy-2", strings.Repeat("a", 24)} {
		if _, _, _, err := f.svc.CreateFTPAccount(bg, uid, sid, name, "", false); err != nil {
			t.Errorf("имя %q должно подходить: %v", name, err)
		}
	}
	// Имя без учёта регистра: «Deploy» и «deploy» — один аккаунт.
	if _, _, _, err := f.svc.CreateFTPAccount(bg, uid, sid, "DEPLOY-2", "", false); !errors.Is(err, sites.ErrFTPNameTaken) {
		t.Errorf("повтор имени: %v", err)
	}
}

func TestAccountLimitPerSite(t *testing.T) {
	f := setup(t, 1<<20)
	for i := range sites.MaxFTPAccounts {
		f.account("acc"+string(rune('a'+i)), "", false)
	}
	if _, _, _, err := f.svc.CreateFTPAccount(bg, f.site.UserID, f.site.ID, "extra", "", false); !errors.Is(err, sites.ErrFTPAccountLimit) {
		t.Fatalf("лимит: %v", err)
	}
}

func TestAccountFolderValidation(t *testing.T) {
	f := setup(t, 1<<20)
	if err := os.MkdirAll(f.pub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.pub, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := []string{"..", "../x", "a/../../x", ".git", "a/.ssh", ".htaccess", "file.txt", "file.txt/sub", "a\\b", "a\x00b", strings.Repeat("a/", 9) + "z", strings.Repeat("d", 201)}
	for i, dir := range bad {
		if _, _, _, err := f.svc.CreateFTPAccount(bg, f.site.UserID, f.site.ID, "t"+string(rune('a'+i)), dir, false); !errors.Is(err, sites.ErrFTPDirInvalid) {
			t.Errorf("папка %q: %v", dir, err)
		}
	}
	// Допустимые записи нормализуются.
	for dir, want := range map[string]string{"": "", "/": "", " /a//b/ ": "a/b", "./x": "x", "a/./b": "a/b"} {
		a, _, _, err := f.svc.CreateFTPAccount(bg, f.site.UserID, f.site.ID, "n"+strings.ReplaceAll(strings.ReplaceAll(want, "/", ""), ".", "")+"x", dir, false)
		if err != nil {
			t.Errorf("папка %q: %v", dir, err)
			continue
		}
		if a.Dir != want {
			t.Errorf("папка %q → %q, ожидали %q", dir, a.Dir, want)
		}
		_ = f.svc.DeleteFTPAccount(bg, f.site.UserID, f.site.ID, a.ID)
	}
}

func TestOtherUsersCannotManageAccounts(t *testing.T) {
	f := setup(t, 1<<20)
	id, _, _ := f.account("mine", "", false)
	stranger := f.site.UserID + 1000
	if _, _, _, err := f.svc.CreateFTPAccount(bg, stranger, f.site.ID, "hack", "", false); !errors.Is(err, sites.ErrNotFound) {
		t.Errorf("создание: %v", err)
	}
	off := false
	if _, _, err := f.svc.UpdateFTPAccount(bg, stranger, f.site.ID, id, sites.FTPAccountPatch{Enabled: &off}); !errors.Is(err, sites.ErrNotFound) {
		t.Errorf("изменение: %v", err)
	}
	if _, _, _, err := f.svc.ResetFTPAccountPassword(bg, stranger, f.site.ID, id); !errors.Is(err, sites.ErrNotFound) {
		t.Errorf("сброс пароля: %v", err)
	}
	if err := f.svc.DeleteFTPAccount(bg, stranger, f.site.ID, id); !errors.Is(err, sites.ErrNotFound) {
		t.Errorf("удаление: %v", err)
	}
	// Аккаунт другого сайта по чужому id тоже не находится: id привязан к сайту в запросе.
	if _, _, err := f.svc.UpdateFTPAccount(bg, f.site.UserID, f.site.ID+999, id, sites.FTPAccountPatch{Enabled: &off}); !errors.Is(err, sites.ErrNotFound) {
		t.Errorf("чужой сайт: %v", err)
	}
	if _, _, err := f.svc.UpdateFTPAccount(bg, f.site.UserID, f.site.ID, id+999, sites.FTPAccountPatch{Enabled: &off}); !errors.Is(err, sites.ErrFTPAccountAbsent) {
		t.Errorf("чужой аккаунт: %v", err)
	}
}

func TestAccountLastLoginAndListing(t *testing.T) {
	f := setup(t, 1<<20)
	_, login, pw := f.account("stamp", "", false)
	list, err := f.svc.FTPAccountsByUser(bg, f.site.UserID)
	if err != nil || len(list[f.site.ID]) != 1 || list[f.site.ID][0].LastLoginAt != nil {
		t.Fatalf("до входа: %+v %v", list, err)
	}
	f.loginAs(login, pw)
	list, _ = f.svc.FTPAccountsByUser(bg, f.site.UserID)
	if list[f.site.ID][0].LastLoginAt == nil {
		t.Fatal("время последнего входа не записано")
	}
}

func TestAccountsShareQuotaWithSite(t *testing.T) {
	f := setup(t, 100)
	_, login, pw := f.account("q", "", false)
	m := f.login()
	if err := m.Stor("big.bin", strings.NewReader(strings.Repeat("x", 80))); err != nil {
		t.Fatal(err)
	}
	a := f.loginAs(login, pw)
	if err := a.Stor("more.bin", strings.NewReader(strings.Repeat("y", 40))); err == nil {
		t.Fatal("квота сайта общая для всех его FTP-доступов")
	}
}

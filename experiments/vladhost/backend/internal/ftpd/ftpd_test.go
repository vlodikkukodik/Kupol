package ftpd_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jlaffaye/ftp"

	"vladhost/internal/auth"
	"vladhost/internal/config"
	"vladhost/internal/ftpd"
	"vladhost/internal/sites"
	"vladhost/internal/testdb"
)

type fixture struct {
	t        *testing.T
	svc      *sites.Service
	site     *sites.Site
	user     string
	password string
	addr     string
	pub      string // каталог public сайта на диске
	root     string
}

// setup поднимает настоящий FTP-сервер (FTPS, самоподписанный сертификат) на свободном порту.
func setup(t *testing.T, quota int64) *fixture {
	t.Helper()
	db := testdb.Open(t)
	root := t.TempDir()
	svc := sites.NewService(db, root, "vladinc.ru", "", sites.Limits{MaxSites: 1, DiskQuotaBytes: quota})

	authSvc := auth.NewService(db, []byte(strings.Repeat("s", 32)), time.Minute, time.Hour)
	u, err := authSvc.CreateAdmin(context.Background(), "john@example.com", "john", "password123")
	if err != nil {
		t.Fatal(err)
	}
	site, err := svc.Create(context.Background(), *u, "blog")
	if err != nil {
		t.Fatal(err)
	}
	site, pw, err := svc.EnableFTP(context.Background(), u.ID, site.ID)
	if err != nil {
		t.Fatal(err)
	}

	srv, err := ftpd.New(svc, config.FTPConfig{
		Addr: "127.0.0.1:0", Host: "ftp.vladinc.ru", PublicIP: "127.0.0.1", PassiveStart: 42100, PassiveEnd: 42200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Stop() })

	return &fixture{
		t: t, svc: svc, site: site, user: svc.FTPUsername(site.Host), password: pw, addr: srv.Addr(),
		pub: filepath.Join(root, site.Host, "public"), root: root,
	}
}

func (f *fixture) dial(user, pass string) (*ftp.ServerConn, error) {
	c, err := ftp.Dial(f.addr, ftp.DialWithTimeout(5*time.Second),
		ftp.DialWithExplicitTLS(&tls.Config{InsecureSkipVerify: true})) //nolint:gosec // самоподписанный сертификат теста
	if err != nil {
		return nil, err
	}
	if err := c.Login(user, pass); err != nil {
		_ = c.Quit()
		return nil, err
	}
	return c, nil
}

func (f *fixture) login() *ftp.ServerConn {
	f.t.Helper()
	c, err := f.dial(f.user, f.password)
	if err != nil {
		f.t.Fatalf("вход: %v", err)
	}
	f.t.Cleanup(func() { _ = c.Quit() })
	return c
}

func (f *fixture) disk(rel string) string {
	b, err := os.ReadFile(filepath.Join(f.pub, rel))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

func TestFTPFlow(t *testing.T) {
	f := setup(t, 1<<20)
	c := f.login()

	if err := c.Stor("index.html", strings.NewReader("<h1>ftp</h1>")); err != nil {
		t.Fatalf("STOR: %v", err)
	}
	if f.disk("index.html") != "<h1>ftp</h1>" {
		t.Fatal("файл не на диске")
	}
	// Перезапись целиком, а не дописывание.
	if err := c.Stor("index.html", strings.NewReader("v2")); err != nil || f.disk("index.html") != "v2" {
		t.Fatalf("перезапись: %v %q", err, f.disk("index.html"))
	}

	if err := c.MakeDir("css"); err != nil {
		t.Fatalf("MKD: %v", err)
	}
	if err := c.Stor("css/a.css", strings.NewReader("b{}")); err != nil {
		t.Fatalf("STOR во вложенную папку: %v", err)
	}

	r, err := c.Retr("css/a.css")
	if err != nil {
		t.Fatalf("RETR: %v", err)
	}
	got, _ := io.ReadAll(r)
	_ = r.Close()
	if string(got) != "b{}" {
		t.Fatalf("скачано %q", got)
	}

	entries, err := c.List("/")
	if err != nil {
		t.Fatalf("LIST: %v", err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	if !names["index.html"] || !names["css"] || len(names) != 2 {
		t.Fatalf("список: %v", names)
	}

	if err := c.Rename("css/a.css", "css/b.css"); err != nil {
		t.Fatalf("RNFR/RNTO: %v", err)
	}
	if f.disk("css/b.css") != "b{}" {
		t.Fatal("файл не переименован")
	}
	if err := c.RemoveDir("css"); err == nil {
		t.Fatal("RMD непустой папки должен завершаться ошибкой")
	}
	if err := c.Delete("css/b.css"); err != nil {
		t.Fatalf("DELE: %v", err)
	}
	if err := c.RemoveDir("css"); err != nil {
		t.Fatalf("RMD: %v", err)
	}
	if err := c.RemoveDir("/"); err == nil {
		t.Fatal("корень сайта удалить нельзя")
	}

	// После закрытия последней сессии занятое место записывается в БД.
	_ = c.Quit()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s, err := f.svc.Get(context.Background(), f.site.UserID, f.site.ID)
		if err != nil {
			t.Fatal(err)
		}
		if s.DiskBytes == int64(len("v2")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("disk_bytes = %d, ожидали %d", s.DiskBytes, len("v2"))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestFTPRequiresTLS(t *testing.T) {
	f := setup(t, 1<<20)
	c, err := ftp.Dial(f.addr, ftp.DialWithTimeout(5*time.Second)) // без AUTH TLS
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Quit() }()
	if err := c.Login(f.user, f.password); err == nil {
		t.Fatal("вход без TLS должен быть отклонён: пароль ушёл бы открытым текстом")
	}
}

func TestFTPAuth(t *testing.T) {
	f := setup(t, 1<<20)

	for name, cred := range map[string][2]string{
		"неверный пароль":       {f.user, "wrong-password"},
		"неизвестный логин":     {"ghost.john", f.password},
		"логин из другого мира": {"../etc", f.password},
		"пустой":      {"", ""},
		"полный host": {f.site.Host, f.password}, // логин без общего домена
	} {
		if c, err := f.dial(cred[0], cred[1]); err == nil {
			_ = c.Quit()
			t.Errorf("%s: вход не должен пройти", name)
		}
	}
	// Логин не зависит от регистра.
	if c, err := f.dial(strings.ToUpper(f.user), f.password); err != nil {
		t.Errorf("логин в верхнем регистре: %v", err)
	} else {
		_ = c.Quit()
	}

	// Пароль аккаунта на панели не подходит к FTP: у FTP свой пароль.
	if c, err := f.dial(f.user, "password123"); err == nil {
		_ = c.Quit()
		t.Error("пароль аккаунта не должен открывать FTP")
	}

	// Смена пароля: старый перестаёт работать.
	_, newPW, err := f.svc.EnableFTP(context.Background(), f.site.UserID, f.site.ID)
	if err != nil || newPW == f.password {
		t.Fatalf("новый пароль: %v", err)
	}
	if c, err := f.dial(f.user, f.password); err == nil {
		_ = c.Quit()
		t.Error("старый пароль после смены должен быть отклонён")
	}
	c, err := f.dial(f.user, newPW)
	if err != nil {
		t.Fatalf("новый пароль: %v", err)
	}
	_ = c.Quit()

	// Отключение FTP закрывает доступ.
	if _, err := f.svc.DisableFTP(context.Background(), f.site.UserID, f.site.ID); err != nil {
		t.Fatal(err)
	}
	if c, err := f.dial(f.user, newPW); err == nil {
		_ = c.Quit()
		t.Error("после отключения вход должен быть отклонён")
	}
}

func TestFTPBruteForceBlocked(t *testing.T) {
	f := setup(t, 1<<20)
	for range 10 {
		if c, err := f.dial(f.user, "bad-password"); err == nil {
			_ = c.Quit()
		}
	}
	// После серии неудач даже верный пароль с этого адреса временно не принимается.
	if c, err := f.dial(f.user, f.password); err == nil {
		_ = c.Quit()
		t.Fatal("после 10 неудачных попыток вход должен блокироваться")
	}
}

func TestFTPCannotEscapeSite(t *testing.T) {
	f := setup(t, 1<<20)
	c := f.login()

	secret := filepath.Join(f.root, "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP-SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Симлинки, подложенные в обход FTP (например, старым багом): наружу они вести не должны.
	if err := os.Symlink(secret, filepath.Join(f.pub, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.root, filepath.Join(f.pub, "linkdir")); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"../secret.txt", "../../secret.txt", "a/../../secret.txt", "/../secret.txt", "link.txt", "linkdir/secret.txt"} {
		if r, err := c.Retr(p); err == nil {
			data, _ := io.ReadAll(r)
			_ = r.Close()
			if strings.Contains(string(data), "TOP-SECRET") {
				t.Errorf("RETR %q отдал файл за пределами сайта", p)
			}
		}
	}
	for _, p := range []string{"../evil.txt", "../../evil.txt", "linkdir/evil.txt"} {
		_ = c.Stor(p, strings.NewReader("pwn"))
	}
	if _, err := os.Stat(filepath.Join(f.root, "evil.txt")); err == nil {
		t.Fatal("STOR записал файл за пределы сайта")
	}
	if b, _ := os.ReadFile(secret); string(b) != "TOP-SECRET" {
		t.Fatal("внешний файл изменён")
	}
	// CWD за пределы корня не уводит: остаёмся внутри.
	_ = c.ChangeDir("../..")
	if dir, err := c.CurrentDir(); err != nil || dir != "/" {
		t.Fatalf("CWD ../.. → %q %v", dir, err)
	}
}

func TestFTPQuota(t *testing.T) {
	f := setup(t, 1<<20) // 1 МиБ
	c := f.login()

	big := bytes.Repeat([]byte("A"), 2<<20)
	if err := c.Stor("big.bin", bytes.NewReader(big)); err == nil {
		t.Fatal("загрузка 2 МиБ при квоте 1 МиБ должна завершаться ошибкой")
	}
	if used := dirSize(t, f.pub); used > 1<<20 {
		t.Fatalf("квота нарушена: на диске %d байт", used)
	}
	// Ошибка не блокирует работу: место освобождается удалением и можно загрузить снова.
	if err := c.Delete("big.bin"); err != nil {
		t.Fatalf("удаление недогруженного файла: %v", err)
	}
	if err := c.Stor("ok.txt", strings.NewReader("small")); err != nil {
		t.Fatalf("после освобождения места: %v", err)
	}

	// Две параллельные сессии делят одну квоту: вместе больше 1 МиБ не влезет.
	c2 := f.login()
	part := bytes.Repeat([]byte("B"), 700<<10)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, conn := range []*ftp.ServerConn{c, c2} {
		wg.Go(func() {
			errs[i] = conn.Stor("part"+string(rune('a'+i)), bytes.NewReader(part))
		})
	}
	wg.Wait()
	if errs[0] == nil && errs[1] == nil {
		t.Fatal("обе загрузки по 700 КиБ прошли — квота 1 МиБ не действует между сессиями")
	}
	if used := dirSize(t, f.pub); used > 1<<20 {
		t.Fatalf("квота нарушена параллельными загрузками: %d байт", used)
	}
}

func TestFTPRevokeDuringSession(t *testing.T) {
	f := setup(t, 1<<20)
	c := f.login()
	if err := c.Stor("a.txt", strings.NewReader("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.DisableFTP(context.Background(), f.site.UserID, f.site.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.Stor("b.txt", strings.NewReader("b")); err == nil {
		t.Fatal("после отзыва доступа открытая сессия не должна писать")
	}
	if _, err := os.Stat(filepath.Join(f.pub, "b.txt")); err == nil {
		t.Fatal("файл записан после отзыва доступа")
	}
	if _, err := c.List("/"); err == nil {
		t.Fatal("после отзыва доступа открытая сессия не должна читать")
	}
}

func dirSize(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

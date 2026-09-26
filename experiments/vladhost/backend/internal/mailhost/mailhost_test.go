package mailhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GehirnInc/crypt/sha512_crypt"

	"vladhost/internal/apperr"
	"vladhost/internal/auth"
	"vladhost/internal/runtimes"
	"vladhost/internal/sites"
	"vladhost/internal/testdb"
)

var bg = context.Background()

// realHelper — исполнитель, который запускает настоящий deploy/bin/mail-sync.py на временных каталогах: так проверяется всё, что панель
// пишет в state.json, тем же кодом, который на сервере раскладывает файлы для exim и dovecot.
type realHelper struct {
	t         *testing.T
	dir       string // папка обмена (mail/state.json)
	mail      string // /etc/vladhost/mail
	vmail     string // /var/vmail
	mu        sync.Mutex
	calls     int
	fail      string
	down      bool
	lastOut   string
	failures  int    // сколько первых вызовов отказать
	mainlog   string // журнал exim, который читает mail-log.py
	logDomain string // для какого домена в последний раз просили журнал
}

func (h *realHelper) Do(_ context.Context, r runtimes.Request, _ time.Duration) (runtimes.Result, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	if h.down {
		return runtimes.Result{}, errors.New("no answer")
	}
	if h.failures > 0 {
		h.failures--
		return runtimes.Result{Error: "io_13"}, nil
	}
	if h.fail != "" {
		return runtimes.Result{Error: h.fail}, nil
	}
	if r.Action == ActionMailLog {
		out, err := exec.Command("python3", "../../../deploy/bin/mail-log.py", "--domain", r.Host, "--limit", fmt.Sprint(r.ID), "--log", h.mainlog, "--queue-cmd", "true").Output()
		if err != nil {
			return runtimes.Result{Error: "bad_args"}, nil
		}
		h.logDomain = r.Host
		return runtimes.Result{OK: true, Output: string(out)}, nil
	}
	if r.Action != ActionMailSync {
		h.t.Fatalf("неожиданное действие %q", r.Action)
	}
	script, _ := filepath.Abs("../../../deploy/bin/mail-sync.py")
	out, err := exec.Command("python3", script, "--state", filepath.Join(h.dir, "mail", "state.json"), "--mail-dir", h.mail, "--vmail-dir", h.vmail,
		"--usage", filepath.Join(h.dir, "mail", "usage.json"), "--no-chown").CombinedOutput()
	h.lastOut = strings.TrimSpace(string(out))
	if err != nil || h.lastOut != "ok" {
		return runtimes.Result{Error: strings.TrimPrefix(h.lastOut, "error="), Output: h.lastOut}, nil
	}
	return runtimes.Result{OK: true}, nil
}

type fakeSites struct {
	mu      sync.Mutex
	domains map[int64][]sites.Domain
}

func (f *fakeSites) DomainsByUser(_ context.Context, uid int64) (map[int64][]sites.Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return map[int64][]sites.Domain{1: append([]sites.Domain(nil), f.domains[uid]...)}, nil
}

type fakeDNS struct {
	mx  map[string][]string
	txt map[string][]string
}

func (f fakeDNS) LookupMX(_ context.Context, n string) ([]string, error)  { return f.mx[n], nil }
func (f fakeDNS) LookupTXT(_ context.Context, n string) ([]string, error) { return f.txt[n], nil }

type env struct {
	t      *testing.T
	svc    *Service
	helper *realHelper
	sites  *fakeSites
	auth   *auth.Service
	user   auth.User
	dns    fakeDNS
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("нужен python3 для настоящего mail-sync.py")
	}
	db := testdb.Open(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rt", "mail"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &realHelper{t: t, dir: filepath.Join(root, "rt"), mainlog: filepath.Join(root, "mainlog"), mail: filepath.Join(root, "etc-mail"), vmail: filepath.Join(root, "vmail")}
	fs := &fakeSites{domains: map[int64][]sites.Domain{}}
	dns := fakeDNS{mx: map[string][]string{}, txt: map[string][]string{}}
	e := &env{t: t, helper: h, sites: fs, dns: dns}
	e.svc = New(db, Config{Host: "mail.vladinc.ru", ServerIP: "203.0.113.10", BaseDomain: "vladinc.ru", Dir: h.dir, Applier: h, Sites: fs, Resolver: dns})
	e.auth = auth.NewService(db, []byte(strings.Repeat("s", 32)), time.Minute, time.Hour)
	e.user = e.newUser()
	return e
}

func (e *env) newUser() auth.User {
	name := fmt.Sprintf("ml%d", time.Now().UnixNano()%1_000_000_000)
	u, err := e.auth.CreateAdmin(bg, name+"@example.com", name, "password123")
	if err != nil {
		e.t.Fatal(err)
	}
	return *u
}

func (e *env) attach(u auth.User, domains ...string) {
	e.sites.mu.Lock()
	defer e.sites.mu.Unlock()
	for _, d := range domains {
		e.sites.domains[u.ID] = append(e.sites.domains[u.ID], sites.Domain{Host: d, Kind: "custom"})
	}
}

func (e *env) file(name string) string {
	raw, err := os.ReadFile(filepath.Join(e.helper.mail, name))
	if err != nil {
		e.t.Fatalf("%s: %v", name, err)
	}
	return string(raw)
}

func (e *env) enable(u auth.User, domain string) *Domain {
	e.t.Helper()
	e.attach(u, strings.ToLower(domain))
	d, err := e.svc.EnableDomain(bg, u, domain)
	if err != nil {
		e.t.Fatalf("EnableDomain %s: %v", domain, err)
	}
	return d
}

func code(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

func TestEnableDomainWritesEverythingTheServerNeeds(t *testing.T) {
	e := newEnv(t)
	d := e.enable(e.user, "Example.COM")
	if d.Domain != "example.com" || !d.Enabled || d.DKIMSelector != "vh1" || d.DKIMPrivate == "" || d.DKIMPublic == "" {
		t.Fatalf("%+v", d)
	}
	if e.file("domains") != "example.com: 1\n" {
		t.Fatalf("domains: %q", e.file("domains"))
	}
	// postmaster обязателен: письма идут владельцу аккаунта
	if got := e.file("aliases"); got != "postmaster@example.com: "+strings.ToLower(e.user.Email)+"\n" {
		t.Fatalf("aliases: %q", got)
	}
	key, err := os.ReadFile(filepath.Join(e.helper.mail, "dkim", "example.com.key"))
	if err != nil || !strings.HasPrefix(string(key), "-----BEGIN PRIVATE KEY-----") {
		t.Fatalf("ключ DKIM: %v", err)
	}
	// закрытый ключ не попадает в JSON ответа
	list, _ := e.svc.List(bg, e.user.ID)
	raw, _ := json.Marshal(list)
	if len(list) != 1 || strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "dkim_private") {
		t.Fatal("список должен работать, а ключа в нём быть не должно")
	}
}

func TestEnableDomainRules(t *testing.T) {
	e := newEnv(t)
	for _, bad := range []string{"", "not a domain", "localhost", "a..b.com", "-x.com", "x.c", "http://x.com", "ex ample.com"} {
		if _, err := e.svc.EnableDomain(bg, e.user, bad); code(err) != "validation.mail_domain" {
			t.Errorf("%q: %v", bad, err)
		}
	}
	// домен не подключён к сайту пользователя
	if _, err := e.svc.EnableDomain(bg, e.user, "unattached.com"); code(err) != "validation.mail_domain_not_attached" {
		t.Fatalf("%v", err)
	}
	// под нашим доменом почту не заводим
	e.attach(e.user, "blog.vlad.vladinc.ru", "vladinc.ru")
	for _, d := range []string{"blog.vlad.vladinc.ru", "vladinc.ru"} {
		if _, err := e.svc.EnableDomain(bg, e.user, d); code(err) != "validation.mail_domain" {
			t.Errorf("%s: %v", d, err)
		}
	}
	// домен другого пользователя
	other := e.newUser()
	e.attach(other, "theirs.com")
	if _, err := e.svc.EnableDomain(bg, e.user, "theirs.com"); code(err) != "validation.mail_domain_not_attached" {
		t.Fatalf("чужой домен: %v", err)
	}
	e.enable(e.user, "mine.com")
	// занят другим пользователем: оба подключили один домен к своим сайтам
	e.attach(other, "mine.com")
	if _, err := e.svc.EnableDomain(bg, other, "mine.com"); code(err) != "mail_domain_taken" {
		t.Fatalf("%v", err)
	}
	// лимит
	for i := 0; i < MaxDomains-1; i++ {
		e.enable(e.user, fmt.Sprintf("d%d.org", i))
	}
	e.attach(e.user, "toomany.org")
	if _, err := e.svc.EnableDomain(bg, e.user, "toomany.org"); code(err) != "mail_domain_limit" {
		t.Fatalf("лимит доменов: %v", err)
	}
}

func TestDomainLimitHoldsUnderRace(t *testing.T) {
	e := newEnv(t)
	var names []string
	for i := 0; i < 9; i++ {
		names = append(names, fmt.Sprintf("race%d.org", i))
	}
	e.attach(e.user, names...)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for _, n := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.svc.EnableDomain(bg, e.user, n); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != MaxDomains {
		t.Fatalf("включено %d доменов, лимит %d", ok, MaxDomains)
	}
}

func TestMailboxLifecycle(t *testing.T) {
	e := newEnv(t)
	d := e.enable(e.user, "example.com")

	box, pw, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "Info", "", 0)
	if err != nil || box.LocalPart != "info" || box.QuotaMB != DefaultQuotaMB || len(pw) != 20 {
		t.Fatalf("%+v %q %v", box, pw, err)
	}
	// хеш проверяется тем же алгоритмом, что у dovecot; пароль в базе не хранится
	if !strings.HasPrefix(box.PasswordHash, "{SHA512-CRYPT}$6$") || strings.Contains(box.PasswordHash, pw) {
		t.Fatalf("%q", box.PasswordHash)
	}
	if err := sha512_crypt.New().Verify(strings.TrimPrefix(box.PasswordHash, "{SHA512-CRYPT}"), []byte(pw)); err != nil {
		t.Fatalf("пароль не подходит к хешу: %v", err)
	}
	if e.file("mailboxes") != "info@example.com: 1\n" {
		t.Fatalf("%q", e.file("mailboxes"))
	}
	users := e.file("dovecot-users")
	if !strings.HasPrefix(users, "info@example.com:{SHA512-CRYPT}$6$") || !strings.Contains(users, "userdb_quota_rule=*:storage=500M") || strings.Contains(users, "nologin") {
		t.Fatalf("%q", users)
	}
	if _, err := os.Stat(filepath.Join(e.helper.vmail, "example.com", "info")); err != nil {
		t.Fatalf("папка ящика: %v", err)
	}

	// свой пароль не возвращается
	box2, pw2, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "second.user+x", "my-own-password-1", 100)
	if err != nil || pw2 != "" || box2.QuotaMB != 100 {
		t.Fatalf("%+v %q %v", box2, pw2, err)
	}
	if err := sha512_crypt.New().Verify(strings.TrimPrefix(box2.PasswordHash, "{SHA512-CRYPT}"), []byte("my-own-password-1")); err != nil {
		t.Fatal(err)
	}

	// выключить ящик: входа нет, письма принимаются; квота меняется
	q, off := 200, false
	up, err := e.svc.UpdateMailbox(bg, e.user.ID, box.ID, &q, &off)
	if err != nil || up.QuotaMB != 200 || up.Enabled {
		t.Fatalf("%+v %v", up, err)
	}
	if !strings.Contains(e.file("mailboxes"), "info@example.com: 1") || !strings.Contains(e.file("dovecot-users"), "storage=200M nologin") {
		t.Fatalf("%q %q", e.file("mailboxes"), e.file("dovecot-users"))
	}

	// смена пароля: старый хеш заменён
	old := box.PasswordHash
	newPw, err := e.svc.SetMailboxPassword(bg, e.user.ID, box.ID, "")
	if err != nil || len(newPw) != 20 {
		t.Fatalf("%q %v", newPw, err)
	}
	var now Mailbox
	e.svc.db.First(&now, box.ID)
	if now.PasswordHash == old || sha512_crypt.New().Verify(strings.TrimPrefix(now.PasswordHash, "{SHA512-CRYPT}"), []byte(pw)) == nil {
		t.Fatal("старый пароль должен перестать подходить")
	}
	if got, err := e.svc.SetMailboxPassword(bg, e.user.ID, box.ID, "another-password-22"); err != nil || got != "" {
		t.Fatalf("%q %v", got, err)
	}

	// удаление: письма убираются с диска, список на удаление очищается
	if err := os.MkdirAll(filepath.Join(e.helper.vmail, "example.com", "info", "Maildir", "new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteMailbox(bg, e.user.ID, box.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.helper.vmail, "example.com", "info")); err == nil {
		t.Fatal("папка удалённого ящика должна исчезнуть")
	}
	if _, err := os.Stat(filepath.Join(e.helper.vmail, "example.com", "second.user+x")); err != nil {
		t.Fatal("папка другого ящика остаётся")
	}
	var n int64
	e.svc.db.Model(&purge{}).Count(&n)
	if n != 0 {
		t.Fatalf("после успешной синхронизации список удаления пуст, а в нём %d", n)
	}
	// то же имя заводится заново без потери свежей почты
	if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "info", "", 0); err != nil {
		t.Fatal(err)
	}
}

func TestMailboxValidationAndLimits(t *testing.T) {
	e := newEnv(t)
	d := e.enable(e.user, "example.com")
	for _, bad := range []string{"", "a b", "a:b", "../x", "a..b", ".a", "a.", "Ж", strings.Repeat("a", 65), "a@b", "*"} {
		if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, bad, "", 0); code(err) != "validation.mail_local" {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "ok", "short", 0); code(err) != "validation.mail_password" {
		t.Fatalf("%v", err)
	}
	if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "ok", "line\nbreak-password", 0); code(err) != "validation.mail_password" {
		t.Fatalf("%v", err)
	}
	for _, q := range []int{MinQuotaMB - 1, MaxQuotaMB + 1, -5} {
		if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "ok", "", q); code(err) != "validation.mail_quota" {
			t.Errorf("квота %d: %v", q, err)
		}
	}
	if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "same", "", 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "same", "", 0); code(err) != "mail_local_taken" {
		t.Fatalf("повтор: %v", err)
	}
	// имя уже занято алиасом (postmaster заведён сам)
	if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "postmaster", "", 0); code(err) != "mail_local_taken" {
		t.Fatalf("алиас: %v", err)
	}
	for i := 1; i < MaxMailboxes; i++ {
		if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, fmt.Sprintf("u%d", i), "", 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "over", "", 0); code(err) != "mail_box_limit" {
		t.Fatalf("лимит ящиков: %v", err)
	}
	// лимит общий на аккаунт: второй домен его не обходит
	d2 := e.enable(e.user, "second.org")
	if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d2.ID, "x", "", 0); code(err) != "mail_box_limit" {
		t.Fatalf("лимит на аккаунт: %v", err)
	}
}

func TestMailboxLimitHoldsUnderRace(t *testing.T) {
	e := newEnv(t)
	d := e.enable(e.user, "example.com")
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, fmt.Sprintf("r%d", i), "", 0); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != MaxMailboxes {
		t.Fatalf("создано %d ящиков, лимит %d", ok, MaxMailboxes)
	}
}

func TestAliases(t *testing.T) {
	e := newEnv(t)
	d := e.enable(e.user, "example.com")
	if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "info", "", 0); err != nil {
		t.Fatal(err)
	}
	a, err := e.svc.SetAlias(bg, e.user.ID, d.ID, "Sales", []string{"Info@Example.com", "boss@gmail.com", "boss@gmail.com"})
	if err != nil || a.LocalPart != "sales" || len(a.To) != 2 {
		t.Fatalf("%+v %v", a, err)
	}
	if !strings.Contains(e.file("aliases"), "sales@example.com: info@example.com, boss@gmail.com") {
		t.Fatalf("%q", e.file("aliases"))
	}
	// замена
	if _, err := e.svc.SetAlias(bg, e.user.ID, d.ID, "sales", []string{"other@gmail.com"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.file("aliases"), "sales@example.com: other@gmail.com\n") {
		t.Fatalf("%q", e.file("aliases"))
	}
	// общий ящик домена
	if _, err := e.svc.SetAlias(bg, e.user.ID, d.ID, "*", []string{"info@example.com"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.file("aliases"), "*@example.com: info@example.com\n") {
		t.Fatalf("%q", e.file("aliases"))
	}
	// отказы
	bad := map[string][]string{
		"validation.mail_dest":      {"not-an-address"},
		"validation.mail_dest2":     {"a@b.com, c@d.com"},
		"validation.mail_dest3":     {"|/bin/sh"},
		"validation.mail_dest4":     {":fail:"},
		"validation.mail_dest5":     {"Name <a@b.com>"},
		"validation.mail_dest6":     {},
		"validation.mail_dest7":     {"a@b.com", "b@c.com", "c@d.com", "d@e.com", "e@f.com", "f@g.com"},
		"validation.mail_dest_loop": {"loop@example.com"},
	}
	for want, to := range bad {
		want = strings.TrimRight(want, "0123456789")
		if _, err := e.svc.SetAlias(bg, e.user.ID, d.ID, "loop", to); code(err) != want {
			t.Errorf("%v: получено %v, ждали %s", to, err, want)
		}
	}
	if _, err := e.svc.SetAlias(bg, e.user.ID, d.ID, "info", []string{"x@y.com"}); code(err) != "mail_local_taken" {
		t.Fatalf("имя ящика занято: %v", err)
	}
	if _, err := e.svc.SetAlias(bg, e.user.ID, d.ID, "a b", []string{"x@y.com"}); code(err) != "validation.mail_local" {
		t.Fatalf("%v", err)
	}
	// удаление
	if err := e.svc.DeleteAlias(bg, e.user.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.file("aliases"), "sales@") {
		t.Fatal("алиас должен исчезнуть")
	}
	// лимит: postmaster и «*» уже есть
	for i := 0; i < MaxAliases-2; i++ {
		if _, err := e.svc.SetAlias(bg, e.user.ID, d.ID, fmt.Sprintf("al%d", i), []string{"x@y.com"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.SetAlias(bg, e.user.ID, d.ID, "onemore", []string{"x@y.com"}); code(err) != "mail_alias_limit" {
		t.Fatalf("лимит алиасов: %v", err)
	}
	// существующий алиас при исчерпанном лимите можно менять
	if _, err := e.svc.SetAlias(bg, e.user.ID, d.ID, "al0", []string{"z@y.com"}); err != nil {
		t.Fatalf("замена при лимите: %v", err)
	}
}

func TestDomainDisableAndDelete(t *testing.T) {
	e := newEnv(t)
	d := e.enable(e.user, "example.com")
	if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "info", "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetDomainEnabled(bg, e.user.ID, d.ID, false); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"domains", "mailboxes", "dovecot-users"} {
		if e.file(f) != "" {
			t.Fatalf("%s: %q", f, e.file(f))
		}
	}
	if _, err := os.Stat(filepath.Join(e.helper.vmail, "example.com", "info")); err != nil {
		t.Fatal("выключенный домен почту не теряет")
	}
	if _, err := e.svc.SetDomainEnabled(bg, e.user.ID, d.ID, true); err != nil || e.file("domains") != "example.com: 1\n" {
		t.Fatalf("%v %q", err, e.file("domains"))
	}
	if err := e.svc.DeleteDomain(bg, e.user.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.helper.vmail, "example.com")); err == nil {
		t.Fatal("папка домена должна быть удалена с диска")
	}
	if _, err := os.Stat(filepath.Join(e.helper.mail, "dkim", "example.com.key")); err == nil {
		t.Fatal("ключ DKIM удалённого домена должен исчезнуть")
	}
	// домен можно завести снова: старые письма не возвращаются, а новая почта не теряется
	if e.enable(e.user, "example.com") == nil {
		t.Fatal("повторное включение")
	}
}

func TestOwnershipIsolation(t *testing.T) {
	e := newEnv(t)
	d := e.enable(e.user, "example.com")
	box, _, _ := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "info", "", 0)
	al, _ := e.svc.SetAlias(bg, e.user.ID, d.ID, "sales", []string{"x@y.com"})
	other := e.newUser()
	q, on := 100, true
	checks := map[string]error{
		"SetDomainEnabled": func() error { _, err := e.svc.SetDomainEnabled(bg, other.ID, d.ID, false); return err }(),
		"DeleteDomain":     e.svc.DeleteDomain(bg, other.ID, d.ID),
		"CreateMailbox":    func() error { _, _, err := e.svc.CreateMailbox(bg, other.ID, d.ID, "x", "", 0); return err }(),
		"UpdateMailbox":    func() error { _, err := e.svc.UpdateMailbox(bg, other.ID, box.ID, &q, &on); return err }(),
		"SetPassword":      func() error { _, err := e.svc.SetMailboxPassword(bg, other.ID, box.ID, ""); return err }(),
		"DeleteMailbox":    e.svc.DeleteMailbox(bg, other.ID, box.ID),
		"SetAlias":         func() error { _, err := e.svc.SetAlias(bg, other.ID, d.ID, "z", []string{"x@y.com"}); return err }(),
		"DeleteAlias":      e.svc.DeleteAlias(bg, other.ID, al.ID),
		"CheckDNS":         func() error { _, err := e.svc.CheckDNS(bg, other.ID, d.ID); return err }(),
	}
	for name, err := range checks {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: чужой объект должен быть «не найден», получено %v", name, err)
		}
	}
	if list, _ := e.svc.List(bg, other.ID); len(list) != 0 {
		t.Fatalf("список чужих доменов: %+v", list)
	}
	if e.file("mailboxes") != "info@example.com: 1\n" {
		t.Fatal("чужие попытки ничего не изменили")
	}
}

func TestSyncFailureKeepsDataAndIsRetried(t *testing.T) {
	e := newEnv(t)
	e.helper.failures = 1
	e.attach(e.user, "example.com")
	d, err := e.svc.EnableDomain(bg, e.user, "example.com")
	if code(err) != "mail_sync_failed" || d == nil {
		t.Fatalf("%v", err)
	}
	if list, _ := e.svc.List(bg, e.user.ID); len(list) != 1 {
		t.Fatal("данные сохранены, несмотря на сбой синхронизации")
	}
	// Watch повторяет синхронизацию, пока она не удастся
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	go e.svc.Watch(ctx, 50*time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := os.ReadFile(filepath.Join(e.helper.mail, "domains"))
		if err == nil && string(raw) == "example.com: 1\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Watch не применил состояние")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// исполнитель не отвечает
	e.helper.down = true
	if _, err := e.svc.SetDomainEnabled(bg, e.user.ID, d.ID, false); code(err) != "mail_helper_down" {
		t.Fatalf("%v", err)
	}
}

func TestDNSRecordsAndStates(t *testing.T) {
	e := newEnv(t)
	d := e.enable(e.user, "example.com")
	rec := func() map[string]Record {
		out := map[string]Record{}
		got, err := e.svc.CheckDNS(bg, e.user.ID, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range got {
			out[r.Kind] = r
		}
		return out
	}
	r := rec()
	if len(r) != 4 || r["mx"].Value != "10 mail.vladinc.ru." || r["mx"].Type != "MX" || r["mx"].Name != "example.com" ||
		r["spf"].Value != "v=spf1 mx ip4:203.0.113.10 ~all" || r["dkim"].Name != "vh1._domainkey.example.com" ||
		!strings.HasPrefix(r["dkim"].Value, "v=DKIM1; k=rsa; p=MII") || r["dmarc"].Name != "_dmarc.example.com" ||
		r["dmarc"].Value != "v=DMARC1; p=none; rua=mailto:postmaster@example.com" {
		t.Fatalf("%+v", r)
	}
	for k, v := range r {
		if v.State != StateMissing {
			t.Errorf("%s: без записей состояние должно быть missing, а не %s", k, v.State)
		}
	}
	// записи созданы правильно
	e.dns.mx["example.com"] = []string{"mail.vladinc.ru"}
	e.dns.txt["example.com"] = []string{"google-site-verification=abc", "v=spf1 mx ip4:203.0.113.10 ~all"}
	e.dns.txt["vh1._domainkey.example.com"] = []string{"v=DKIM1; k=rsa; p=" + d.DKIMPublic}
	e.dns.txt["_dmarc.example.com"] = []string{"v=DMARC1; p=quarantine"}
	for k, v := range rec() {
		if v.State != StateOK {
			t.Errorf("%s: %+v", k, v)
		}
	}
	// записи есть, но чужие
	e.dns.mx["example.com"] = []string{"mx.other.net"}
	e.dns.txt["example.com"] = []string{"v=spf1 include:_spf.google.com ~all"}
	e.dns.txt["vh1._domainkey.example.com"] = []string{"v=DKIM1; k=rsa; p=AAAA"}
	r = rec()
	if r["mx"].State != StateMismatch || r["mx"].Detail != "mx.other.net" || r["spf"].State != StateMismatch || r["dkim"].State != StateMismatch {
		t.Fatalf("%+v", r)
	}
	// ключ DKIM, разбитый на части (так хранят длинные TXT), тоже узнаётся
	half := len(d.DKIMPublic) / 2
	e.dns.txt["vh1._domainkey.example.com"] = []string{"v=DKIM1; k=rsa; p=" + d.DKIMPublic[:half] + " " + d.DKIMPublic[half:]}
	if rec()["dkim"].State != StateOK {
		t.Fatal("ключ с пробелом посередине")
	}
}

func TestEligibleAndEnabled(t *testing.T) {
	e := newEnv(t)
	e.attach(e.user, "one.com", "two.com", "one.com")
	e.sites.mu.Lock()
	e.sites.domains[e.user.ID] = append(e.sites.domains[e.user.ID], sites.Domain{Host: "sub.blog.user.vladinc.ru", Kind: "sub"})
	e.sites.mu.Unlock()
	el, err := e.svc.Eligible(bg, e.user.ID)
	if err != nil || strings.Join(el, ",") != "one.com,two.com" {
		t.Fatalf("%v %v", el, err)
	}
	e.enable(e.user, "one.com")
	el, _ = e.svc.Eligible(bg, e.user.ID)
	if strings.Join(el, ",") != "two.com" {
		t.Fatalf("%v", el)
	}
	if !e.svc.Enabled() {
		t.Fatal("почта настроена")
	}
	var nilSvc *Service
	if nilSvc.Enabled() {
		t.Fatal("nil-служба выключена")
	}
	info, _ := e.svc.Info(bg, e.user.ID)
	if info.Host != "mail.vladinc.ru" || info.IMAPPort != 993 || info.MaxBoxes != MaxMailboxes || len(info.Eligible) != 1 {
		t.Fatalf("%+v", info)
	}
}

func TestUsageIsReported(t *testing.T) {
	e := newEnv(t)
	d := e.enable(e.user, "example.com")
	if _, _, err := e.svc.CreateMailbox(bg, e.user.ID, d.ID, "info", "", 0); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(e.helper.vmail, "example.com", "info", "Maildir", "new")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "1"), []byte(strings.Repeat("x", 4096)), 0o600)
	if err := e.svc.Sync(bg); err != nil {
		t.Fatal(err)
	}
	list, _ := e.svc.List(bg, e.user.ID)
	if list[0].Mailboxes[0].UsedBytes != 4096 {
		t.Fatalf("%+v", list[0].Mailboxes)
	}
}

func TestMailboxRulesBecomeASieveScript(t *testing.T) {
	e := newEnv(t)
	e.enable(e.user, "example.com")
	list, _ := e.svc.List(bg, e.user.ID)
	dom := list[0].ID
	box, _, err := e.svc.CreateMailbox(bg, e.user.ID, dom, "info", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !box.Forward.KeepCopy || len(box.Forward.To) != 0 || box.AutoReply.Enabled || box.AutoReply.Days != 1 {
		t.Fatalf("правила по умолчанию: %+v %+v", box.AutoReply, box.Forward)
	}
	sieve := filepath.Join(e.helper.vmail, "example.com", "info", "sieve-panel", "panel.sieve")
	if _, err := os.Stat(sieve); err == nil {
		t.Fatal("без правил скрипта быть не должно")
	}

	got, err := e.svc.SetMailboxRules(bg, e.user.ID, box.ID,
		AutoReply{Enabled: true, Subject: "  В отпуске ", Body: "Отвечу позже.\r\nСпасибо!\n", From: "2026-10-01", To: "2026-10-15", Days: 2},
		Forward{To: []string{"Boss@Gmail.com", "boss@gmail.com", "b@example.org"}, KeepCopy: false})
	if err != nil {
		t.Fatal(err)
	}
	if got.AutoReply.Subject != "В отпуске" || got.AutoReply.Body != "Отвечу позже.\nСпасибо!" || len(got.Forward.To) != 2 || got.Forward.KeepCopy {
		t.Fatalf("сохранено: %+v %+v", got.AutoReply, got.Forward)
	}
	raw, err := os.ReadFile(sieve)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{`redirect "boss@gmail.com";`, `redirect "b@example.org";`, `vacation :days 2 :subject "В отпуске"`, `"ge" "date" "2026-10-01"`, "Отвечу позже.\nСпасибо!"} {
		if !strings.Contains(text, want) {
			t.Errorf("в скрипте нет %q:\n%s", want, text)
		}
	}
	// правила видны в списке, хеш пароля — нет
	list, _ = e.svc.List(bg, e.user.ID)
	shown, _ := json.Marshal(list)
	if !strings.Contains(string(shown), `"keep_copy":false`) || strings.Contains(string(shown), "SHA512") {
		t.Fatalf("%s", shown)
	}

	// выключение убирает скрипт
	if _, err := e.svc.SetMailboxRules(bg, e.user.ID, box.ID, AutoReply{}, Forward{KeepCopy: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sieve); err == nil {
		t.Fatal("скрипт должен быть удалён")
	}
	list, _ = e.svc.List(bg, e.user.ID)
	if list[0].Mailboxes[0].AutoReply.Enabled || len(list[0].Mailboxes[0].Forward.To) != 0 {
		t.Fatalf("%+v", list[0].Mailboxes[0])
	}
}

func TestMailboxRulesValidation(t *testing.T) {
	e := newEnv(t)
	e.enable(e.user, "example.com")
	list, _ := e.svc.List(bg, e.user.ID)
	box, _, err := e.svc.CreateMailbox(bg, e.user.ID, list[0].ID, "info", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	ok := AutoReply{Enabled: true, Subject: "s", Body: "b"}
	long := strings.Repeat("я", MaxBody+1)
	cases := []struct {
		name string
		ar   AutoReply
		fw   Forward
		want string
	}{
		{"тема пустая", AutoReply{Enabled: true, Subject: " ", Body: "b"}, Forward{}, "validation.mail_autoreply_text"},
		{"тема с переводом строки", AutoReply{Enabled: true, Subject: "a\nb", Body: "b"}, Forward{}, "validation.mail_autoreply_text"},
		{"тема длинная", AutoReply{Enabled: true, Subject: strings.Repeat("x", MaxSubject+1), Body: "b"}, Forward{}, "validation.mail_autoreply_text"},
		{"текст пустой", AutoReply{Enabled: true, Subject: "s", Body: ""}, Forward{}, "validation.mail_autoreply_text"},
		{"текст длинный", AutoReply{Enabled: true, Subject: "s", Body: long}, Forward{}, "validation.mail_autoreply_text"},
		{"управляющий символ", AutoReply{Enabled: true, Subject: "s", Body: "a\x00b"}, Forward{}, "validation.mail_autoreply_text"},
		{"дата не дата", AutoReply{Enabled: true, Subject: "s", Body: "b", From: "2026-02-30"}, Forward{}, "validation.mail_autoreply_dates"},
		{"формат даты", AutoReply{Enabled: true, Subject: "s", Body: "b", To: "1.10.2026"}, Forward{}, "validation.mail_autoreply_dates"},
		{"порядок дат", AutoReply{Enabled: true, Subject: "s", Body: "b", From: "2026-10-15", To: "2026-10-01"}, Forward{}, "validation.mail_autoreply_dates"},
		{"период", AutoReply{Enabled: true, Subject: "s", Body: "b", Days: 31}, Forward{}, "validation.mail_autoreply_days"},
		{"адрес", ok, Forward{To: []string{"x@y.com, z@y.com"}}, "validation.mail_dest"},
		{"конвейер", ok, Forward{To: []string{"|/bin/sh"}}, "validation.mail_dest"},
		{"много адресов", ok, Forward{To: []string{"a@x.com", "b@x.com", "c@x.com", "d@x.com", "e@x.com", "f@x.com"}}, "validation.mail_dest"},
		{"на себя", ok, Forward{To: []string{"info@example.com"}}, "validation.mail_dest_loop"},
	}
	for _, tc := range cases {
		if _, err := e.svc.SetMailboxRules(bg, e.user.ID, box.ID, tc.ar, tc.fw); code(err) != tc.want {
			t.Errorf("%s: %v (код %q, ждали %q)", tc.name, err, code(err), tc.want)
		}
	}
	// чужой ящик недоступен
	other := e.newUser()
	if _, err := e.svc.SetMailboxRules(bg, other.ID, box.ID, ok, Forward{}); code(err) != "mail_not_found" {
		t.Fatalf("чужой ящик: %v", err)
	}
	// после отказов ничего не сохранилось
	list, _ = e.svc.List(bg, e.user.ID)
	if list[0].Mailboxes[0].AutoReply.Enabled {
		t.Fatal("отказ не должен менять правила")
	}
}

const sampleLog = `2026-09-25 14:22:22 1xA6og-000000037kI-0BvG <= app@localhost H=localhost (host.example) [127.0.0.1] P=esmtp S=603 from <app@localhost> for random@example.com
2026-09-25 14:22:22 1xA6og-000000037kI-0BvG => info <random@example.com> R=vladhost_mailbox T=vladhost_lmtp C="250 2.0.0 Saved"
2026-09-25 14:22:25 H=host.example [37.153.70.33] F=<x@gmail.com> rejected RCPT <hacker@evil.example>: relay not permitted
2026-09-25 14:22:26 H=host.example [37.153.70.33] F=<boss@example.com> rejected RCPT <second@example.com>: Sender domain is served by this server: authenticate to send from it
2026-09-25 14:22:27 1xA6ol-000000037ku-33mU <= a@else.org H=mx.else.org [203.0.113.5] P=esmtps S=900 for someone@else.net
2026-09-25 14:22:28 1xA6ol-000000037ku-33mU ** ghost@else.net R=dnslookup T=remote_smtp: secret internal error
`

func TestJournalShowsOnlyTheDomainsMail(t *testing.T) {
	e := newEnv(t)
	d := e.enable(e.user, "example.com")
	other := e.newUser()
	e.attach(other, "else.org")
	od, err := e.svc.EnableDomain(bg, other, "else.org")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.helper.mainlog, []byte(sampleLog), 0o644); err != nil {
		t.Fatal(err)
	}
	j, err := e.svc.Log(bg, e.user.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.helper.logDomain != "example.com" {
		t.Fatalf("исполнителю передан домен %q", e.helper.logDomain)
	}
	kinds := map[string]int{}
	for _, ev := range j.Events {
		kinds[ev.Kind]++
		if strings.Contains(ev.From+ev.To+ev.Detail, "else.") {
			t.Fatalf("в журнале чужое: %+v", ev)
		}
	}
	if kinds["delivered"] != 1 || kinds["received"] != 1 || kinds["rejected"] != 1 || len(j.Events) != 3 || j.Queue == nil {
		t.Fatalf("%v %+v", kinds, j)
	}
	// у другого домена свои события, чужой домен запросить нельзя
	oj, err := e.svc.Log(bg, other.ID, od.ID)
	if err != nil || len(oj.Events) != 2 {
		t.Fatalf("%+v %v", oj, err)
	}
	if _, err := e.svc.Log(bg, e.user.ID, od.ID); code(err) != "mail_not_found" {
		t.Fatalf("чужой домен: %v", err)
	}
	// исполнитель недоступен или ответил ошибкой — понятные коды
	e.helper.down = true
	if _, err := e.svc.Log(bg, e.user.ID, d.ID); code(err) != "mail_helper_down" {
		t.Fatalf("%v", err)
	}
	e.helper.down = false
	e.helper.fail = "io_13"
	if _, err := e.svc.Log(bg, e.user.ID, d.ID); code(err) != "mail_log_failed" {
		t.Fatalf("%v", err)
	}
}

// fakeHosting — «наш DNS» для проверки кнопки автоматической настройки.
type fakeHosting struct {
	mu      sync.Mutex
	status  DNSStatus
	applied []DNSMailRecord
	calls   int
	err     error
}

func (f *fakeHosting) Status(context.Context, int64, string) (DNSStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, f.err
}

func (f *fakeHosting) ApplyMail(_ context.Context, _ int64, _ string, recs []DNSMailRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.applied = recs
	return nil
}

func (f *fakeHosting) Owns(context.Context, int64, string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status.Zone, f.err
}

func TestAutoDNSOnlyForDomainsOnOurNameServers(t *testing.T) {
	e := newEnv(t)
	d := e.enable(e.user, "example.com")

	// наш DNS не подключён: кнопки нет
	res, err := e.svc.DNSInfo(bg, e.user.ID, d.ID)
	if err != nil || res.Auto.Available || res.Auto.Delegated || len(res.Records) != 4 {
		t.Fatalf("%+v %v", res, err)
	}
	if err := e.svc.AutoConfigure(bg, e.user.ID, d.ID); code(err) != "mail_dns_auto_disabled" {
		t.Fatalf("%v", err)
	}

	h := &fakeHosting{status: DNSStatus{Zone: false, State: "none", Expected: []string{"ns.vladinc.ru", "ns2.vladinc.ru"}}}
	e.svc.cfg.DNS = h
	res, _ = e.svc.DNSInfo(bg, e.user.ID, d.ID)
	if !res.Auto.Available || res.Auto.Zone || res.Auto.Delegated || len(res.Auto.Expected) != 2 || res.Auto.Found == nil {
		t.Fatalf("без зоны: %+v", res.Auto)
	}
	if err := e.svc.AutoConfigure(bg, e.user.ID, d.ID); code(err) != "mail_dns_not_delegated" {
		t.Fatalf("без зоны: %v", err)
	}
	h.status = DNSStatus{Zone: true, Delegated: false, State: "none", Found: []string{"ns.majordomo.ru"}, Expected: []string{"ns.vladinc.ru", "ns2.vladinc.ru"}}
	if err := e.svc.AutoConfigure(bg, e.user.ID, d.ID); code(err) != "mail_dns_not_delegated" {
		t.Fatalf("зона есть, домен на чужих серверах: %v", err)
	}
	if h.calls != 0 {
		t.Fatal("в зону ничего писать нельзя")
	}

	// домен на наших серверах: кнопка работает и ставит ровно то, что показывает панель
	h.status = DNSStatus{Zone: true, Delegated: true, State: "ok", Found: []string{"ns.vladinc.ru", "ns2.vladinc.ru"}, Expected: []string{"ns.vladinc.ru", "ns2.vladinc.ru"}}
	res, _ = e.svc.DNSInfo(bg, e.user.ID, d.ID)
	if !res.Auto.Delegated || res.Auto.State != "ok" {
		t.Fatalf("%+v", res.Auto)
	}
	if err := e.svc.AutoConfigure(bg, e.user.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	got := map[string]DNSMailRecord{}
	for _, r := range h.applied {
		got[r.Kind] = r
	}
	if len(got) != 4 || got["mx"].Priority != 10 || got["mx"].Value != "mail.vladinc.ru" || got["mx"].Name != "example.com" || got["mx"].Type != "MX" {
		t.Fatalf("%+v", h.applied)
	}
	if got["spf"].Value != "v=spf1 mx ip4:203.0.113.10 ~all" || got["dkim"].Name != "vh1._domainkey.example.com" || !strings.HasPrefix(got["dkim"].Value, "v=DKIM1; k=rsa; p=") ||
		got["dmarc"].Name != "_dmarc.example.com" || !strings.HasPrefix(got["dmarc"].Value, "v=DMARC1;") {
		t.Fatalf("%+v", h.applied)
	}
	// чужой домен нельзя настроить
	other := e.newUser()
	if err := e.svc.AutoConfigure(bg, other.ID, d.ID); code(err) != "mail_not_found" {
		t.Fatalf("%v", err)
	}
	// ошибка определения состояния DNS не превращается в «можно»
	h.err = errors.New("db down")
	res, _ = e.svc.DNSInfo(bg, e.user.ID, d.ID)
	if res.Auto.Delegated || res.Auto.State != "unknown" {
		t.Fatalf("%+v", res.Auto)
	}
}

package shellaccess

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"vladhost/internal/apperr"
	"vladhost/internal/auth"
	"vladhost/internal/runtimes"
	"vladhost/internal/sites"
	"vladhost/internal/testdb"
)

var bg = context.Background()

type fakeApplier struct {
	mu    sync.Mutex
	calls []runtimes.Request
	fail  string
	down  bool
}

func (f *fakeApplier) Do(_ context.Context, r runtimes.Request, _ time.Duration) (runtimes.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r)
	if f.down {
		return runtimes.Result{}, errors.New("no answer")
	}
	if f.fail != "" {
		return runtimes.Result{Error: f.fail, Output: "boom"}, nil
	}
	return runtimes.Result{OK: true}, nil
}

func (f *fakeApplier) actions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Action)
	}
	return out
}

type env struct {
	t     *testing.T
	svc   *Service
	sites *sites.Service
	fake  *fakeApplier
	auth  *auth.Service
	user  auth.User
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db := testdb.Open(t)
	siteSvc := sites.NewService(db, t.TempDir(), "vladinc.ru", "", sites.Limits{MaxSites: 10, DiskQuotaBytes: 1 << 20})
	fake := &fakeApplier{}
	e := &env{t: t, sites: siteSvc, fake: fake}
	e.svc = New(db, siteSvc, Config{Applier: fake, BaseDomain: "vladinc.ru", SSHHost: "ssh.vladinc.ru", SSHPort: 2222, HostFingerprint: func() string { return "SHA256:abc" }})
	e.auth = auth.NewService(db, []byte(strings.Repeat("s", 32)), time.Minute, time.Hour)
	e.user = e.newUser()
	return e
}

func (e *env) newUser() auth.User {
	name := fmt.Sprintf("sh%d", time.Now().UnixNano()%1_000_000_000)
	u, err := e.auth.CreateAdmin(bg, name+"@example.com", name, "password123")
	if err != nil {
		e.t.Fatal(err)
	}
	return *u
}

func (e *env) site(u auth.User, slug string) *sites.Site {
	e.t.Helper()
	s, err := e.sites.Create(bg, u, slug)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func authorized(t *testing.T, pub any) string {
	t.Helper()
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k))) + " comment"
}

func edKey(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	k, _ := ssh.NewPublicKey(pub)
	return authorized(t, pub), k
}

func TestParseKeyRules(t *testing.T) {
	ed, _ := edKey(t)
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rsa3072, _ := rsa.GenerateKey(rand.Reader, 3072)
	rsa1024, _ := rsa.GenerateKey(rand.Reader, 1024)
	good := []string{ed, authorized(t, &ec.PublicKey), authorized(t, &rsa3072.PublicKey), "  " + ed + "  "}
	for _, g := range good {
		if _, canon, fp, err := ParseKey(g); err != nil || strings.Contains(canon, " comment") || !strings.HasPrefix(fp, "SHA256:") {
			t.Errorf("%.30q: %v %q", g, err, canon)
		}
	}
	base := strings.Fields(ed)[0] + " " + strings.Fields(ed)[1]
	bad := []string{
		"", "not a key", authorized(t, &rsa1024.PublicKey), // слишком короткий RSA
		`command="rm -rf /" ` + ed, // параметры authorized_keys не допускаются
		`from="10.0.0.1" ` + ed, ed + "\n" + ed, base + "\nssh-rsa AAAA", "ssh-dss AAAAB3NzaC1kc3M=", strings.Repeat("a", 9000),
	}
	for _, b := range bad {
		if _, _, _, err := ParseKey(b); err == nil {
			t.Errorf("%.40q должно отклоняться", b)
		}
	}
}

func TestKeysLifecycleLimitAndUniqueness(t *testing.T) {
	e := newEnv(t)
	line, pub := edKey(t)
	k, err := e.svc.AddKey(bg, e.user.ID, "  ноутбук  ", line)
	if err != nil || k.Name != "ноутбук" || k.Algorithm != "ssh-ed25519" || k.Fingerprint != ssh.FingerprintSHA256(pub) || strings.Contains(k.PublicKey, "comment") {
		t.Fatalf("%+v %v", k, err)
	}
	// тот же ключ — ни у этого, ни у другого аккаунта
	if _, err := e.svc.AddKey(bg, e.user.ID, "again", line); !errors.Is(err, ErrKeyTaken) {
		t.Fatalf("%v", err)
	}
	if _, err := e.svc.AddKey(bg, e.newUser().ID, "steal", line); !errors.Is(err, ErrKeyTaken) {
		t.Fatalf("чужой аккаунт не может добавить тот же ключ: %v", err)
	}
	for _, name := range []string{"", strings.Repeat("я", 61)} {
		l, _ := edKey(t)
		if _, err := e.svc.AddKey(bg, e.user.ID, name, l); !errors.Is(err, ErrKeyName) {
			t.Errorf("имя %q: %v", name, err)
		}
	}
	for i := 1; i < MaxKeys; i++ {
		l, _ := edKey(t)
		if _, err := e.svc.AddKey(bg, e.user.ID, fmt.Sprint("k", i), l); err != nil {
			t.Fatal(err)
		}
	}
	l, _ := edKey(t)
	if _, err := e.svc.AddKey(bg, e.user.ID, "extra", l); !errors.Is(err, ErrKeyLimit) {
		t.Fatalf("лимит ключей: %v", err)
	}
	list, _ := e.svc.ListKeys(bg, e.user.ID)
	if len(list) != MaxKeys {
		t.Fatalf("%d", len(list))
	}
	other := e.newUser()
	if err := e.svc.DeleteKey(bg, other.ID, k.ID); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("чужой ключ не удаляется: %v", err)
	}
	if err := e.svc.DeleteKey(bg, e.user.ID, k.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteKey(bg, e.user.ID, k.ID); !errors.Is(err, ErrKeyNotFound) {
		t.Fatal("повторное удаление")
	}
}

func TestKeyLimitHoldsUnderRace(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	var ok, limited int32
	var mu sync.Mutex
	for i := 0; i < 15; i++ {
		l, _ := edKey(t)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.svc.AddKey(bg, e.user.ID, fmt.Sprint("k", i), l)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				ok++
			} else if errors.Is(err, ErrKeyLimit) {
				limited++
			}
		}()
	}
	wg.Wait()
	if ok != MaxKeys || limited != 5 {
		t.Fatalf("создано %d, отказов по лимиту %d", ok, limited)
	}
}

func TestGeneratedKeyPairWorks(t *testing.T) {
	e := newEnv(t)
	g, err := e.svc.GenerateKey(bg, e.user.ID, "сгенерированный")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(g.PrivateKey, "-----BEGIN OPENSSH PRIVATE KEY-----") || !strings.HasSuffix(strings.TrimSpace(g.PrivateKey), "-----END OPENSSH PRIVATE KEY-----") {
		t.Fatalf("формат закрытого ключа: %.60q", g.PrivateKey)
	}
	signer, err := ssh.ParsePrivateKey([]byte(g.PrivateKey))
	if err != nil {
		t.Fatalf("закрытый ключ должен читаться ssh: %v", err)
	}
	if ssh.FingerprintSHA256(signer.PublicKey()) != g.Key.Fingerprint || g.Key.Algorithm != "ssh-ed25519" {
		t.Fatal("закрытый ключ не соответствует сохранённому открытому")
	}
	// Закрытая часть нигде не хранится
	var stored string
	e.svc.db.Raw("SELECT public_key FROM ssh_keys WHERE id = ?", g.Key.ID).Scan(&stored)
	if strings.Contains(stored, "PRIVATE") {
		t.Fatal("в базе не должно быть закрытого ключа")
	}
	list, _ := e.svc.ListKeys(bg, e.user.ID)
	for _, k := range list {
		if strings.Contains(fmt.Sprintf("%+v", k), "PRIVATE") {
			t.Fatal("список ключей не должен содержать закрытую часть")
		}
	}
}

func TestEnableDisableAndStatus(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	st, err := e.svc.Status(bg, e.user.ID, s.ID)
	if err != nil || st.Enabled || st.Login != "blog."+e.user.Username || st.Host != "ssh.vladinc.ru" || st.Port != 2222 || st.Fingerprint != "SHA256:abc" {
		t.Fatalf("%+v %v", st, err)
	}
	st, err = e.svc.Enable(bg, e.user.ID, s.ID)
	if err != nil || !st.Enabled || st.EnabledAt == nil {
		t.Fatalf("%+v %v", st, err)
	}
	if a := e.fake.calls[0]; a.Action != runtimes.ActionShellOn || a.Host != s.Host || a.ID != s.ID {
		t.Fatalf("%+v", a)
	}
	// повторное включение безопасно
	if st, err := e.svc.Enable(bg, e.user.ID, s.ID); err != nil || !st.Enabled {
		t.Fatalf("%+v %v", st, err)
	}
	st, err = e.svc.Disable(bg, e.user.ID, s.ID)
	if err != nil || st.Enabled {
		t.Fatalf("%+v %v", st, err)
	}
	if e.fake.calls[len(e.fake.calls)-1].Action != runtimes.ActionShellOff {
		t.Fatalf("%v", e.fake.actions())
	}
	if _, err := e.svc.WebGrant(bg, e.user.ID, s.ID); !errors.Is(err, ErrDisabled) {
		t.Fatalf("после отключения веб-терминал закрыт: %v", err)
	}
}

func TestHelperFailuresLeaveStateUnchanged(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	e.fake.fail = "user"
	if _, err := e.svc.Enable(bg, e.user.ID, s.ID); err == nil || !strings.Contains(fmt.Sprint(err), "refused") {
		t.Fatalf("%v", err)
	}
	if st, _ := e.svc.Status(bg, e.user.ID, s.ID); st.Enabled {
		t.Fatal("после отказа доступ не включён")
	}
	e.fake.fail, e.fake.down = "", true
	if _, err := e.svc.Enable(bg, e.user.ID, s.ID); !errors.Is(err, ErrHelperDown) {
		t.Fatalf("%v", err)
	}
	e.fake.down = false
	if _, err := e.svc.Enable(bg, e.user.ID, s.ID); err != nil {
		t.Fatal(err)
	}
	e.fake.fail = "busy"
	if _, err := e.svc.Disable(bg, e.user.ID, s.ID); err == nil {
		t.Fatal("отказ исполнителя")
	}
	if st, _ := e.svc.Status(bg, e.user.ID, s.ID); !st.Enabled {
		t.Fatal("если выключить не удалось, доступ остаётся включённым (и видно, что он включён)")
	}
}

func TestOwnershipIsolation(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	other := e.newUser()
	if _, err := e.svc.Status(bg, other.ID, s.ID); !errors.Is(err, sites.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if _, err := e.svc.Enable(bg, other.ID, s.ID); !errors.Is(err, sites.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if _, err := e.svc.Disable(bg, other.ID, s.ID); !errors.Is(err, sites.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if _, err := e.svc.WebGrant(bg, other.ID, s.ID); !errors.Is(err, sites.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if len(e.fake.calls) != 0 {
		t.Fatal("исполнитель не вызывался")
	}
}

func TestAuthenticate(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	line, pub := edKey(t)
	k, err := e.svc.AddKey(bg, e.user.ID, "k", line)
	if err != nil {
		t.Fatal(err)
	}
	login := "blog." + e.user.Username
	// доступ не включён
	if _, err := e.svc.Authenticate(bg, login, pub); !errors.Is(err, ErrAuth) {
		t.Fatalf("без включённого доступа: %v", err)
	}
	if _, err := e.svc.Enable(bg, e.user.ID, s.ID); err != nil {
		t.Fatal(err)
	}
	g, err := e.svc.Authenticate(bg, strings.ToUpper(login), pub) // регистр логина не важен
	if err != nil || g.SiteID != s.ID || g.Host != s.Host || g.UserID != e.user.ID || g.Username != e.user.Username || g.KeyID != k.ID {
		t.Fatalf("%+v %v", g, err)
	}
	// разбор логина: только «сайт.пользователь»
	for _, bad := range []string{"", "blog", "blog." + e.user.Username + ".vladinc.ru", "../etc", "blog.other", "other." + e.user.Username, "blog.vl ad", "root"} {
		if _, err := e.svc.Authenticate(bg, bad, pub); !errors.Is(err, ErrAuth) {
			t.Errorf("логин %q: %v", bad, err)
		}
	}
	// чужой ключ и ключ другого пользователя, чей сайт мы пытаемся открыть
	_, stranger := edKey(t)
	if _, err := e.svc.Authenticate(bg, login, stranger); !errors.Is(err, ErrAuth) {
		t.Fatal("неизвестный ключ")
	}
	other := e.newUser()
	oline, opub := edKey(t)
	if _, err := e.svc.AddKey(bg, other.ID, "o", oline); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(bg, login, opub); !errors.Is(err, ErrAuth) {
		t.Fatal("ключ другого аккаунта не открывает чужой сайт")
	}
	// удалённый ключ больше не работает
	if err := e.svc.DeleteKey(bg, e.user.ID, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(bg, login, pub); !errors.Is(err, ErrAuth) {
		t.Fatal("удалённый ключ")
	}
}

func TestTouchAndPurge(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	line, _ := edKey(t)
	k, _ := e.svc.AddKey(bg, e.user.ID, "k", line)
	if k.LastUsedAt != nil {
		t.Fatal("ключ ещё не использовался")
	}
	e.svc.Touch(bg, k.ID)
	list, _ := e.svc.ListKeys(bg, e.user.ID)
	if list[0].LastUsedAt == nil {
		t.Fatal("вход должен отмечаться")
	}
	// Purge: у сайта без доступа исполнитель не вызывается, с доступом — да
	e.svc.Purge(bg, *s)
	if len(e.fake.calls) != 0 {
		t.Fatalf("%v", e.fake.actions())
	}
	if _, err := e.svc.Enable(bg, e.user.ID, s.ID); err != nil {
		t.Fatal(err)
	}
	e.fake.mu.Lock()
	e.fake.calls = nil
	e.fake.mu.Unlock()
	e.svc.Purge(bg, *s)
	if acts := e.fake.actions(); len(acts) != 1 || acts[0] != runtimes.ActionPurge {
		t.Fatalf("%v", acts)
	}
	var nilSvc *Service
	nilSvc.Purge(bg, *s) // без службы — просто ничего
	if nilSvc.Enabled() {
		t.Fatal("nil-служба выключена")
	}
}

func TestErrorCodesAreApperr(t *testing.T) {
	for _, e := range []error{ErrKeyInvalid, ErrKeyName, ErrKeyTaken, ErrKeyLimit, ErrKeyNotFound, ErrHelperDown, ErrApplyFailed, ErrDisabled} {
		var ae *apperr.Error
		if !errors.As(e, &ae) || ae.Code == "" {
			t.Errorf("%v", e)
		}
	}
}

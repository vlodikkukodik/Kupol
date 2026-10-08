package runtimes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vladhost/internal/apperr"
	"vladhost/internal/auth"
	"vladhost/internal/runtimecfg"
	"vladhost/internal/sites"
	"vladhost/internal/testdb"
)

var bg = context.Background()

// fakeApplier запоминает заявки и отвечает по сценарию.
type fakeApplier struct {
	mu    sync.Mutex
	calls []Request
	fail  map[string]string // действие → код отказа
	down  bool
	state string
	out   string
}

func (f *fakeApplier) Do(_ context.Context, r Request, _ time.Duration) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r)
	if f.down {
		return Result{}, errors.New("no answer")
	}
	if code, ok := f.fail[r.Action]; ok {
		return Result{OK: false, Error: code, Output: "boom"}, nil
	}
	st := f.state
	if st == "" {
		st = "active"
	}
	return Result{OK: true, State: st, Output: f.out}, nil
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

func (f *fakeApplier) last(action string) Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].Action == action {
			return f.calls[i]
		}
	}
	return Request{}
}

type env struct {
	t     *testing.T
	svc   *Service
	sites *sites.Service
	fake  *fakeApplier
	user  auth.User
	auth  *auth.Service
	dir   string
	root  string
}

func newEnv(t *testing.T, caps string) *env {
	t.Helper()
	db := testdb.Open(t)
	root, dir := t.TempDir(), t.TempDir()
	if caps != "" {
		if err := os.WriteFile(filepath.Join(dir, "caps"), []byte(caps), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	siteSvc := sites.NewService(db, root, "vladinc.ru", "", sites.Limits{MaxSites: 10, DiskQuotaBytes: 1 << 20})
	fake := &fakeApplier{fail: map[string]string{}}
	e := &env{t: t, sites: siteSvc, fake: fake, dir: dir, root: root}
	e.svc = New(db, siteSvc, fake, dir)
	siteSvc.SetPermsFixer(e.svc.FixPerms)
	siteSvc.SetDeleteHook(e.svc.Purge)
	e.auth = auth.NewService(db, []byte(strings.Repeat("s", 32)), time.Minute, time.Hour)
	e.user = e.newUser()
	return e
}

func (e *env) newUser() auth.User {
	name := fmt.Sprintf("rt%d", time.Now().UnixNano()%1_000_000_000)
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

const allCaps = "php=8.2,8.3,8.4\nnode=22.1.0\npython=3.12.3\n"

func TestCapsAreParsedAndSanitized(t *testing.T) {
	e := newEnv(t, "php=8.2, 8.3 ,../x,8.03,\nnode=22.1.0\npython=\n")
	c := e.svc.Caps()
	if strings.Join(c.PHP, ",") != "8.2,8.3" || c.Node != "22.1.0" || c.Python != "" || !c.Available() {
		t.Fatalf("%+v", c)
	}
	off := newEnv(t, "")
	if off.svc.Enabled() || off.svc.Caps().Available() {
		t.Fatal("без файла caps сред нет")
	}
}

func TestSetPHPWritesConfigAfterHelperAndReportsState(t *testing.T) {
	e := newEnv(t, allCaps)
	s := e.site(e.user, "blog")
	v, err := e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "php", Version: "8.3"})
	if err != nil || v.Runtime != "php" || v.Version != "8.3" || v.State != "active" || len(v.Caps.PHP) != 3 {
		t.Fatalf("%+v %v", v, err)
	}
	got := runtimecfg.Load(e.sites.SiteDir(s.Host))
	if got.Runtime != "php" || got.ID != s.ID || got.Version != "8.3" {
		t.Fatalf("runtime.json: %+v", got)
	}
	a := e.fake.last(ActionApply)
	if a.Host != s.Host || a.ID != s.ID || a.Runtime != "php" || a.Version != "8.3" || a.Port != 0 {
		t.Fatalf("заявка: %+v", a)
	}
	// Смена версии
	v, _ = e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "php", Version: "8.4"})
	if v.Version != "8.4" || runtimecfg.Load(e.sites.SiteDir(s.Host)).Version != "8.4" {
		t.Fatalf("%+v", v)
	}
}

func TestValidation(t *testing.T) {
	e := newEnv(t, "php=8.3\nnode=22\n")
	s := e.site(e.user, "blog")
	cases := map[string]Input{
		"rt_version":       {Runtime: "php", Version: "7.4"},
		"rt_not_installed": {Runtime: "python", Command: "python app.py"},
		"rt_command":       {Runtime: "node", Command: "node a.js\nrm -rf /"},
		"rt_runtime":       {Runtime: "ruby"},
	}
	for code, in := range cases {
		_, err := e.svc.Set(bg, e.user.ID, s.ID, in)
		var ae *apperr.Error
		if !errors.As(err, &ae) || !strings.HasSuffix(ae.Code, code) {
			t.Fatalf("%s: %v", code, err)
		}
	}
	if len(e.fake.actions()) != 0 {
		t.Fatalf("при отказе проверки исполнитель не вызывается: %v", e.fake.actions())
	}
	if got := runtimecfg.Load(e.sites.SiteDir(s.Host)); got.Runtime != "static" {
		t.Fatal("runtime.json не должен появляться")
	}
	off := newEnv(t, "")
	s2 := off.site(off.user, "xsite")
	if _, err := off.svc.Set(bg, off.user.ID, s2.ID, Input{Runtime: "php", Version: "8.3"}); !errors.Is(err, ErrRuntimeOff) {
		t.Fatalf("PHP не установлен: %v", err)
	}
}

func TestAppPortsAreUniqueAndStable(t *testing.T) {
	e := newEnv(t, allCaps)
	var ports []int
	for i := 0; i < 4; i++ {
		s := e.site(e.user, fmt.Sprintf("app%d", i))
		v, err := e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "node", Command: "node server.js"})
		if err != nil || v.Port < runtimecfg.PortMin || v.Port > runtimecfg.PortMax {
			t.Fatalf("%+v %v", v, err)
		}
		ports = append(ports, v.Port)
		// Повторное применение того же выбора не меняет порт.
		again, _ := e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "node", Command: "node app.js"})
		if again.Port != v.Port || again.Command != "node app.js" {
			t.Fatalf("порт должен сохраняться: %+v", again)
		}
	}
	seen := map[int]bool{}
	for _, p := range ports {
		if seen[p] {
			t.Fatalf("порты повторяются: %v", ports)
		}
		seen[p] = true
	}
}

func TestPortsUniqueUnderRace(t *testing.T) {
	e := newEnv(t, allCaps)
	users := []auth.User{e.user, e.newUser(), e.newUser()}
	var made []*sites.Site
	var owners []auth.User
	for i := 0; i < 9; i++ {
		u := users[i%3]
		made = append(made, e.site(u, fmt.Sprintf("r%d", i)))
		owners = append(owners, u)
	}
	var wg sync.WaitGroup
	ports := make(chan int, len(made))
	for i := range made {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := e.svc.Set(bg, owners[i].ID, made[i].ID, Input{Runtime: "python", Command: "python app.py"})
			if err != nil {
				t.Error(err)
				return
			}
			ports <- v.Port
		}()
	}
	wg.Wait()
	close(ports)
	seen := map[int]bool{}
	for p := range ports {
		if seen[p] {
			t.Fatalf("порт %d выдан дважды", p)
		}
		seen[p] = true
	}
	if len(seen) != len(made) {
		t.Fatalf("получено %d портов из %d", len(seen), len(made))
	}
}

func TestHelperRefusalRollsBackNewRuntime(t *testing.T) {
	e := newEnv(t, allCaps)
	s := e.site(e.user, "blog")
	e.fake.fail[ActionApply] = "pool"
	_, err := e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "php", Version: "8.3"})
	if err == nil || !strings.Contains(fmt.Sprint(err), "runtime helper refused") {
		t.Fatalf("%v", err)
	}
	if runtimecfg.Load(e.sites.SiteDir(s.Host)).Runtime != "static" {
		t.Fatal("шлюз не должен получить runtime.json после отказа")
	}
	if r, _ := e.svc.row(bg, s.ID); r != nil {
		t.Fatal("запись в БД должна откатиться")
	}
	if acts := e.fake.actions(); acts[len(acts)-1] != ActionStop {
		t.Fatalf("после отказа исполнителю велят убрать полуприменённое: %v", acts)
	}
}

func TestFailedSwitchKeepsPreviousRuntime(t *testing.T) {
	e := newEnv(t, allCaps)
	s := e.site(e.user, "blog")
	if _, err := e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "php", Version: "8.2"}); err != nil {
		t.Fatal(err)
	}
	e.fake.fail[ActionApply] = "unit"
	if _, err := e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "node", Command: "node a.js"}); err == nil {
		t.Fatal("ждали отказ")
	}
	if cfg := runtimecfg.Load(e.sites.SiteDir(s.Host)); cfg.Runtime != "php" || cfg.Version != "8.2" {
		t.Fatalf("runtime.json прежний: %+v", cfg)
	}
	if r, _ := e.svc.row(bg, s.ID); r == nil || r.Runtime != "php" || r.Version != "8.2" || r.Port != nil {
		t.Fatalf("запись прежняя: %+v", r)
	}
}

func TestHelperDownGivesServiceError(t *testing.T) {
	e := newEnv(t, allCaps)
	s := e.site(e.user, "blog")
	e.fake.down = true
	if _, err := e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "php", Version: "8.3"}); !errors.Is(err, ErrHelperDown) {
		t.Fatalf("%v", err)
	}
	e.fake.down = false
	if r, _ := e.svc.row(bg, s.ID); r != nil {
		t.Fatal("после сбоя записи быть не должно")
	}
}

func TestBackToStaticRemovesConfigAndRow(t *testing.T) {
	e := newEnv(t, allCaps)
	s := e.site(e.user, "blog")
	if _, err := e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "php", Version: "8.3"}); err != nil {
		t.Fatal(err)
	}
	v, err := e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "static"})
	if err != nil || v.Runtime != "static" || v.State != "" {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err := os.Stat(filepath.Join(e.sites.SiteDir(s.Host), runtimecfg.FileName)); err == nil {
		t.Fatal("runtime.json должен исчезнуть")
	}
	if r, _ := e.svc.row(bg, s.ID); r != nil {
		t.Fatal("запись должна исчезнуть")
	}
	if e.fake.last(ActionStop).ID != s.ID {
		t.Fatal("исполнитель должен убрать пул")
	}
	// Повторный переход к статике ничего не делает.
	before := len(e.fake.actions())
	if _, err := e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "static"}); err != nil || len(e.fake.actions()) != before {
		t.Fatalf("%v", err)
	}
}

func TestStopFailureKeepsRuntime(t *testing.T) {
	e := newEnv(t, allCaps)
	s := e.site(e.user, "blog")
	_, _ = e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "php", Version: "8.3"})
	e.fake.fail[ActionStop] = "busy"
	if _, err := e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "static"}); err == nil {
		t.Fatal("ждали отказ")
	}
	if cfg := runtimecfg.Load(e.sites.SiteDir(s.Host)); cfg.Runtime != "php" {
		t.Fatalf("сайт остаётся на PHP: %+v", cfg)
	}
	if r, _ := e.svc.row(bg, s.ID); r == nil {
		t.Fatal("запись остаётся")
	}
}

func TestOwnershipIsolation(t *testing.T) {
	e := newEnv(t, allCaps)
	s := e.site(e.user, "blog")
	other := e.newUser()
	if _, err := e.svc.Set(bg, other.ID, s.ID, Input{Runtime: "php", Version: "8.3"}); !errors.Is(err, sites.ErrNotFound) {
		t.Fatalf("чужой сайт: %v", err)
	}
	if _, err := e.svc.Get(bg, other.ID, s.ID); !errors.Is(err, sites.ErrNotFound) {
		t.Fatal("чужой сайт не читается")
	}
	if _, err := e.svc.Restart(bg, other.ID, s.ID); !errors.Is(err, sites.ErrNotFound) {
		t.Fatal("чужой сайт не перезапускается")
	}
	if _, err := e.svc.Logs(bg, other.ID, s.ID); !errors.Is(err, sites.ErrNotFound) {
		t.Fatal("чужие журналы не читаются")
	}
	if len(e.fake.actions()) != 0 {
		t.Fatal("исполнитель не вызывался")
	}
}

func TestRestartLogsAndCooldown(t *testing.T) {
	e := newEnv(t, allCaps)
	s := e.site(e.user, "blog")
	if _, err := e.svc.Restart(bg, e.user.ID, s.ID); !errors.Is(err, ErrNotApp) {
		t.Fatalf("статика: %v", err)
	}
	if _, err := e.svc.Logs(bg, e.user.ID, s.ID); !errors.Is(err, ErrNotApp) {
		t.Fatalf("статика: %v", err)
	}
	_, _ = e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "node", Command: "node a.js"})
	e.fake.out = "line1\nline2"
	logs, err := e.svc.Logs(bg, e.user.ID, s.ID)
	if err != nil || logs != "line1\nline2" {
		t.Fatalf("%q %v", logs, err)
	}
	if _, err := e.svc.Restart(bg, e.user.ID, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Restart(bg, e.user.ID, s.ID); !errors.Is(err, ErrRestartTooSoon) {
		t.Fatalf("второй перезапуск подряд: %v", err)
	}
}

func TestStateFromHelper(t *testing.T) {
	e := newEnv(t, allCaps)
	s := e.site(e.user, "blog")
	_, _ = e.svc.Set(bg, e.user.ID, s.ID, Input{Runtime: "python", Command: "python app.py"})
	e.fake.state = "failed"
	if v, _ := e.svc.Get(bg, e.user.ID, s.ID); v.State != "failed" {
		t.Fatalf("%+v", v)
	}
	e.fake.down = true
	if v, _ := e.svc.Get(bg, e.user.ID, s.ID); v.State != "unknown" {
		t.Fatalf("без ответа исполнителя состояние неизвестно: %+v", v)
	}
}

func TestSiteHooksFixPermsAndPurgeOnlyForRuntimeSites(t *testing.T) {
	e := newEnv(t, allCaps)
	static := e.site(e.user, "plain")
	dyn := e.site(e.user, "dyn")
	if _, err := e.svc.Set(bg, e.user.ID, dyn.ID, Input{Runtime: "php", Version: "8.3"}); err != nil {
		t.Fatal(err)
	}
	e.fake.mu.Lock()
	e.fake.calls = nil
	e.fake.mu.Unlock()

	if err := e.sites.Delete(bg, e.user.ID, static.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.fake.actions()) != 0 {
		t.Fatalf("статический сайт не трогает исполнителя: %v", e.fake.actions())
	}
	if err := e.sites.Delete(bg, e.user.ID, dyn.ID); err != nil {
		t.Fatal(err)
	}
	acts := e.fake.actions()
	if len(acts) != 2 || acts[0] != ActionPerms || acts[1] != ActionPurge {
		t.Fatalf("удаление: сначала права, потом очистка: %v", acts)
	}
	if e.fake.last(ActionPurge).ID != dyn.ID {
		t.Fatal("очищается нужный сайт")
	}
	if _, err := os.Stat(e.sites.SiteDir(dyn.Host)); err == nil {
		t.Fatal("каталог сайта удалён")
	}
}

func TestFileApplierProtocol(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "queue"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "results"), 0o755)
	fa := FileApplier{Dir: dir, Poll: 10 * time.Millisecond}
	go func() {
		for i := 0; i < 500; i++ {
			files, _ := filepath.Glob(filepath.Join(dir, "queue", "*.req"))
			if len(files) == 1 {
				raw, _ := os.ReadFile(files[0])
				rid := strings.TrimSuffix(filepath.Base(files[0]), ".req")
				_ = os.WriteFile(filepath.Join(dir, "results", rid+".out"), []byte("журнал: "+strings.SplitN(string(raw), "\n", 2)[0]), 0o644)
				_ = os.WriteFile(filepath.Join(dir, "results", rid+".res"), []byte("ok=1\nstate=active\n"), 0o644)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	res, err := fa.Do(bg, Request{Action: ActionApply, Host: "a.b.vladinc.ru", ID: 9, Runtime: "node", Port: 20001, Command: "node 'a b'.js; echo $HOME"}, 5*time.Second)
	if err != nil || !res.OK || res.State != "active" || res.Output != "журнал: action=apply" {
		t.Fatalf("%+v %v", res, err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "queue", "*"))
	if len(files) != 1 {
		t.Fatalf("осталась заявка: %v", files)
	}
	raw, _ := os.ReadFile(files[0])
	if !strings.Contains(string(raw), "host=a.b.vladinc.ru\nid=9\nruntime=node\nversion=\nport=20001\ncmd=") || strings.Contains(string(raw), "echo") {
		t.Fatalf("команда должна лежать в заявке только в base64: %q", raw)
	}
}

func TestFileApplierTimeoutRemovesRequest(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "queue"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "results"), 0o755)
	fa := FileApplier{Dir: dir, Poll: 10 * time.Millisecond}
	if _, err := fa.Do(bg, Request{Action: ActionStatus, Host: "a.b.vladinc.ru", ID: 1}, 100*time.Millisecond); err == nil {
		t.Fatal("без ответа должна быть ошибка")
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "queue", "*")); len(files) != 0 {
		t.Fatalf("неотвеченная заявка должна убираться: %v", files)
	}
}

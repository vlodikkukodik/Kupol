package cronjobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vladhost/internal/apperr"
	"vladhost/internal/auth"
	"vladhost/internal/sites"
	"vladhost/internal/testdb"
)

var bg = context.Background()

func TestScheduleRules(t *testing.T) {
	good := []string{"*/5 * * * *", "0 * * * *", "0 3 * * *", "0,30 9-17 * * 1-5", "15 2 1 * *", "59 23 * * 0"}
	for _, e := range good {
		if _, err := ParseSchedule(e, 5); err != nil {
			t.Errorf("%q должно проходить: %v", e, err)
		}
	}
	tooOften := []string{"* * * * *", "*/4 * * * *", "0,3 * * * *", "58,59 23 * * *", "1,58 0,23 * * *"}
	for _, e := range tooOften {
		if _, err := ParseSchedule(e, 5); !errors.Is(err, errInterval) {
			t.Errorf("%q слишком часто, получено %v", e, err)
		}
	}
	bad := []string{"", "@every 1m", "@hourly", "0 0 30 2 *", "CRON_TZ=Asia/Tokyo 0 3 * * *", "TZ=UTC 0 3 * * *", "0 3 * *", "60 * * * *", "0 3 * * * *", "a b c d e", "0 3 * * *\n0 4 * * *"}
	for _, e := range bad {
		if _, err := ParseSchedule(e, 5); !errors.Is(err, errSchedule) {
			t.Errorf("%q недопустимо, получено %v", e, err)
		}
	}
}

func TestURLGuard(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1/", "http://10.0.0.5/x", "http://192.168.1.1", "http://169.254.169.254/latest", "http://[::1]/", "http://100.64.0.1/", "ftp://example.com/", "file:///etc/passwd", "http://user:pw@example.com/", "", "http:///x"} {
		if _, err := checkURL(u, false); err == nil {
			t.Errorf("%q должен отклоняться", u)
		}
	}
	if _, err := checkURL("https://example.com/cron?a=1", false); err != nil {
		t.Error(err)
	}
	// Защита стоит на соединении: локальный сервер недоступен, даже если имя указывает на него.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, "hi") }))
	defer srv.Close()
	name := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	ok, _, _, reason := runHTTP(bg, newHTTPClient(5*time.Second, false), name, false)
	if ok || reason != "private_address" {
		t.Fatalf("localhost должен быть закрыт: ok=%v reason=%q", ok, reason)
	}
	ok, code, out, _ := runHTTP(bg, newHTTPClient(5*time.Second, true), srv.URL, true)
	if !ok || code != 200 || !strings.Contains(out, "hi") {
		t.Fatalf("при разрешённой сети запрос должен пройти: %v %d %q", ok, code, out)
	}
}

func TestRedirectToPrivateIsBlocked(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, "secret") }))
	defer target.Close()
	// Внешний адрес, который перенаправляет на внутренний: в тесте «внешний» — тот же loopback, поэтому проверяем сам клиент
	// с включённой защитой и редиректом от разрешённого адреса нельзя, а вот переход на loopback обязан упасть на соединении.
	c := newHTTPClient(5*time.Second, false)
	if ok, _, _, reason := runHTTP(bg, c, target.URL, false); ok || reason != "private_address" {
		t.Fatalf("loopback не должен открываться: %q", reason)
	}
}

type env struct {
	t    *testing.T
	svc  *Service
	db   *sitesEnv
	user auth.User
	now  atomic.Int64
}

type sitesEnv struct{ sites *sites.Service }

type fakeRunner struct {
	mu    sync.Mutex
	calls []string
	res   CmdResult
	err   error
	block chan struct{}
}

func (f *fakeRunner) Run(_ context.Context, id int64, host, cmd string, _ time.Duration) (CmdResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("%d %s %s", id, host, cmd))
	f.mu.Unlock()
	if f.block != nil {
		<-f.block
	}
	return f.res, f.err
}

func newEnv(t *testing.T, mutate func(*Config)) (*env, *fakeRunner) {
	t.Helper()
	db := testdb.Open(t)
	siteSvc := sites.NewService(db, t.TempDir(), "vladinc.ru", "", sites.Limits{MaxSites: 3, DiskQuotaBytes: 1 << 20})
	fr := &fakeRunner{}
	cfg := Config{AllowPrivate: true, Commands: fr, Timeout: 5 * time.Second}
	if mutate != nil {
		mutate(&cfg)
	}
	e := &env{t: t}
	e.svc = New(db, siteSvc, cfg)
	e.svc.now = func() time.Time { return time.Now().Add(time.Duration(e.now.Load()) * time.Second) }
	a := auth.NewService(db, []byte(strings.Repeat("s", 32)), time.Minute, time.Hour)
	name := fmt.Sprintf("cr%d", time.Now().UnixNano()%1_000_000_000)
	u, err := a.CreateAdmin(bg, name+"@example.com", name, "password123")
	if err != nil {
		t.Fatal(err)
	}
	e.user = *u
	e.db = &sitesEnv{sites: siteSvc}
	return e, fr
}

func (e *env) site() *sites.Site {
	e.t.Helper()
	s, err := e.db.sites.Create(bg, e.user, fmt.Sprintf("s%d", time.Now().UnixNano()%100000))
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func httpInput(url string) Input {
	return Input{Name: "ping", Kind: KindHTTP, Schedule: "*/5 * * * *", URL: url, Enabled: true}
}

func TestValidationAndLimits(t *testing.T) {
	e, _ := newEnv(t, nil)
	site := e.site()
	bad := map[string]Input{
		"cron_name":     {Name: " ", Kind: KindHTTP, Schedule: "0 * * * *", URL: "https://example.com"},
		"cron_schedule": {Name: "a", Kind: KindHTTP, Schedule: "nope", URL: "https://example.com"},
		"cron_interval": {Name: "a", Kind: KindHTTP, Schedule: "* * * * *", URL: "https://example.com"},
		"cron_url":      {Name: "a", Kind: KindHTTP, Schedule: "0 * * * *", URL: "ftp://example.com/"},
		"cron_command":  {Name: "a", Kind: KindCommand, Schedule: "0 * * * *", Command: " ", SiteID: site.ID},
		"cron_site":     {Name: "a", Kind: KindCommand, Schedule: "0 * * * *", Command: "true", SiteID: 999999},
		"cron_kind":     {Name: "a", Kind: "x", Schedule: "0 * * * *"},
	}
	for code, in := range bad {
		_, err := e.svc.Create(bg, e.user.ID, in)
		var ae *apperr.Error
		if !errors.As(err, &ae) || !strings.HasSuffix(ae.Code, code) {
			t.Errorf("%s: получено %v", code, err)
		}
	}
	for i := 0; i < 5; i++ {
		if _, err := e.svc.Create(bg, e.user.ID, httpInput("https://example.com/"+fmt.Sprint(i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.Create(bg, e.user.ID, httpInput("https://example.com/6")); !errors.Is(err, ErrLimit) {
		t.Fatalf("шестая задача должна упереться в лимит: %v", err)
	}
}

func TestLimitHoldsUnderRace(t *testing.T) {
	e, _ := newEnv(t, nil)
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.svc.Create(bg, e.user.ID, httpInput(fmt.Sprintf("https://example.com/%d", i))); err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 5 {
		t.Fatalf("создано %d задач, лимит 5", ok.Load())
	}
}

func TestCommandsOffWithoutRunner(t *testing.T) {
	e, _ := newEnv(t, func(c *Config) { c.Commands = nil })
	site := e.site()
	_, err := e.svc.Create(bg, e.user.ID, Input{Name: "a", Kind: KindCommand, Schedule: "0 * * * *", Command: "true", SiteID: site.ID})
	if !errors.Is(err, ErrCommandsOff) {
		t.Fatal(err)
	}
	if e.svc.Info().CommandsEnabled {
		t.Fatal("в сведениях команды должны быть выключены")
	}
}

func TestOwnershipIsolation(t *testing.T) {
	e, _ := newEnv(t, nil)
	j, _ := e.svc.Create(bg, e.user.ID, httpInput("https://example.com/"))
	const stranger = int64(987654)
	if _, err := e.svc.Get(bg, stranger, j.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("чужая задача не должна открываться")
	}
	if err := e.svc.Delete(bg, stranger, j.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("чужую задачу нельзя удалить")
	}
	if _, err := e.svc.RunNow(bg, stranger, j.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("чужую задачу нельзя запустить")
	}
	// Команда не может выполняться в чужом сайте.
	other := &sites.Site{}
	_ = other
	if _, err := e.svc.Create(bg, stranger, Input{Name: "a", Kind: KindCommand, Schedule: "0 * * * *", Command: "true", SiteID: e.site().ID}); err == nil {
		t.Fatal("нельзя привязать команду к чужому сайту")
	}
}

func TestTickRunsDueJobOnceAndLogs(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); _, _ = fmt.Fprint(w, "pong") }))
	defer srv.Close()
	e, _ := newEnv(t, nil)
	j, err := e.svc.Create(bg, e.user.ID, httpInput(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Tick(bg); err != nil || hits.Load() != 0 {
		t.Fatalf("срок не пришёл: %v hits=%d", err, hits.Load())
	}
	e.now.Store(6 * 60)
	// Две копии панели тикают одновременно: запуск должен быть один.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = e.svc.Tick(bg) }()
	}
	wg.Wait()
	e.svc.Wait()
	if hits.Load() != 1 {
		t.Fatalf("задача должна выполниться один раз, а не %d", hits.Load())
	}
	runs, _ := e.svc.Runs(bg, e.user.ID, j.ID)
	if len(runs) != 1 || runs[0].Status != RunOK || runs[0].Code != 200 || !strings.Contains(runs[0].Output, "pong") {
		t.Fatalf("журнал: %+v", runs)
	}
	got, _ := e.svc.Get(bg, e.user.ID, j.ID)
	if got.LastStatus != RunOK || got.NextRunAt == nil || !got.NextRunAt.After(time.Now().Add(4*time.Minute)) {
		t.Fatalf("итог задачи: %+v", got)
	}
}

func TestMissedRunsDoNotPileUp(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	e, _ := newEnv(t, nil)
	_, _ = e.svc.Create(bg, e.user.ID, httpInput(srv.URL))
	e.now.Store(3 * 24 * 3600) // панель три дня не работала
	_ = e.svc.Tick(bg)
	e.svc.Wait()
	_ = e.svc.Tick(bg)
	e.svc.Wait()
	if hits.Load() != 1 {
		t.Fatalf("после простоя задача идёт один раз, а не %d", hits.Load())
	}
}

func TestFailuresBuildStreakAndSuccessResetsIt(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			http.Error(w, "boom", 500)
		}
	}))
	defer srv.Close()
	e, _ := newEnv(t, nil)
	j, _ := e.svc.Create(bg, e.user.ID, httpInput(srv.URL))
	for i := 1; i <= 3; i++ {
		e.now.Store(int64(i) * 600)
		_ = e.svc.Tick(bg)
		e.svc.Wait()
	}
	got, _ := e.svc.Get(bg, e.user.ID, j.ID)
	if got.FailStreak != 3 || got.FailingSince == nil || got.LastStatus != RunFailed {
		t.Fatalf("серия неудач: %+v", got)
	}
	fail.Store(false)
	e.now.Store(4 * 600)
	_ = e.svc.Tick(bg)
	e.svc.Wait()
	got, _ = e.svc.Get(bg, e.user.ID, j.ID)
	if got.FailStreak != 0 || got.FailingSince != nil || got.LastStatus != RunOK {
		t.Fatalf("успех должен сбросить серию: %+v", got)
	}
}

func TestOverlappingRunIsSkipped(t *testing.T) {
	e, fr := newEnv(t, func(c *Config) { c.Timeout = 5 * time.Minute })
	site := e.site()
	fr.block = make(chan struct{})
	j, err := e.svc.Create(bg, e.user.ID, Input{Name: "slow", Kind: KindCommand, Schedule: "*/5 * * * *", Command: "sleep 100", SiteID: site.ID, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	e.now.Store(6 * 60)
	_ = e.svc.Tick(bg)
	time.Sleep(100 * time.Millisecond)
	e.now.Store(12 * 60)
	_ = e.svc.Tick(bg)
	runs, _ := e.svc.Runs(bg, e.user.ID, j.ID)
	skipped := 0
	for _, r := range runs {
		if r.Status == RunSkipped {
			skipped++
		}
	}
	for _, r := range runs {
		if r.Status == RunSkipped && r.Reason != "overlap" {
			t.Fatalf("причина пропуска: %q", r.Reason)
		}
	}
	if skipped != 1 || len(fr.calls) != 1 {
		t.Fatalf("второй запуск должен пропуститься: skipped=%d calls=%d", skipped, len(fr.calls))
	}
	if _, err := e.svc.RunNow(bg, e.user.ID, j.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("ручной запуск во время работы: %v", err)
	}
	close(fr.block)
	e.svc.Wait()
}

func TestCommandResults(t *testing.T) {
	e, fr := newEnv(t, nil)
	site := e.site()
	j, _ := e.svc.Create(bg, e.user.ID, Input{Name: "c", Kind: KindCommand, Schedule: "0 3 * * *", Command: "echo hi", SiteID: site.ID, Enabled: false})
	cases := []struct {
		res    CmdResult
		err    error
		status string
	}{
		{CmdResult{Output: "hi\n"}, nil, RunOK},
		{CmdResult{Exit: 2, Output: "bad"}, nil, RunFailed},
		{CmdResult{Exit: 124, TimedOut: true}, nil, RunTimeout},
		{CmdResult{}, errors.New("исполнитель не ответил"), RunFailed},
	}
	for i, c := range cases {
		fr.res, fr.err = c.res, c.err
		e.now.Store(int64(i+1) * 120) // между ручными запусками проходит пауза
		run, err := e.svc.RunNow(bg, e.user.ID, j.ID)
		if err != nil {
			t.Fatal(err)
		}
		e.svc.Wait()
		var got Run
		e.svc.db.First(&got, run.ID)
		if got.Status != c.status {
			t.Errorf("случай %d: статус %s, ждали %s", i, got.Status, c.status)
		}
	}
	if len(fr.calls) != 4 || !strings.Contains(fr.calls[0], site.Host) || !strings.HasSuffix(fr.calls[0], "echo hi") {
		t.Fatalf("вызовы: %v", fr.calls)
	}
}

func TestRunNowCooldown(t *testing.T) {
	e, _ := newEnv(t, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	j, _ := e.svc.Create(bg, e.user.ID, httpInput(srv.URL))
	if _, err := e.svc.RunNow(bg, e.user.ID, j.ID); err != nil {
		t.Fatal(err)
	}
	e.svc.Wait()
	if _, err := e.svc.RunNow(bg, e.user.ID, j.ID); !errors.Is(err, ErrCooldown) {
		t.Fatalf("повторный запуск в первую минуту: %v", err)
	}
}

func TestRunsAreKeptWithinLimitAndOutputIsCut(t *testing.T) {
	e, fr := newEnv(t, func(c *Config) { c.KeepRuns = 3 })
	site := e.site()
	fr.res = CmdResult{Output: strings.Repeat("я", maxOutput)} // многобайтные символы: обрезка не должна ломать UTF-8
	j, _ := e.svc.Create(bg, e.user.ID, Input{Name: "c", Kind: KindCommand, Schedule: "0 3 * * *", Command: "x", SiteID: site.ID})
	for i := 1; i <= 5; i++ {
		e.now.Store(int64(i) * 120)
		if _, err := e.svc.RunNow(bg, e.user.ID, j.ID); err != nil {
			t.Fatal(err)
		}
		e.svc.Wait()
	}
	runs, _ := e.svc.Runs(bg, e.user.ID, j.ID)
	if len(runs) != 3 {
		t.Fatalf("хранится %d запусков, лимит 3", len(runs))
	}
	if len(runs[0].Output) > maxOutput+8 || strings.ContainsRune(runs[0].Output, '�') {
		t.Fatalf("вывод обрезан плохо: %d байт", len(runs[0].Output))
	}
}

func TestUpdateRecomputesNextRunAndDisable(t *testing.T) {
	e, _ := newEnv(t, nil)
	j, _ := e.svc.Create(bg, e.user.ID, httpInput("https://example.com/"))
	in := httpInput("https://example.com/")
	in.Enabled = false
	got, err := e.svc.Update(bg, e.user.ID, j.ID, in)
	if err != nil || got.Enabled || got.NextRunAt != nil {
		t.Fatalf("выключенная задача не должна иметь следующего запуска: %+v %v", got, err)
	}
	in.Enabled, in.Schedule = true, "0 3 * * *"
	got, _ = e.svc.Update(bg, e.user.ID, j.ID, in)
	if got.NextRunAt == nil || got.NextRunAt.Hour() != 3 || got.NextRunAt.Minute() != 0 {
		t.Fatalf("следующий запуск: %v", got.NextRunAt)
	}
}

func TestCleanupMarksAbandonedRuns(t *testing.T) {
	e, _ := newEnv(t, nil)
	j, _ := e.svc.Create(bg, e.user.ID, httpInput("https://example.com/"))
	old := e.svc.now().Add(-time.Hour)
	run := &Run{JobID: j.ID, StartedAt: old, Status: RunRunning}
	e.svc.db.Create(run)
	if err := e.svc.Cleanup(bg); err != nil {
		t.Fatal(err)
	}
	var got Run
	e.svc.db.First(&got, run.ID)
	if got.Status != RunFailed || got.FinishedAt == nil || got.Reason != "aborted" {
		t.Fatalf("брошенный запуск: %+v", got)
	}
}

func TestFileRunnerProtocol(t *testing.T) {
	q, r := t.TempDir(), t.TempDir()
	fr := FileRunner{Queue: q, Results: r, Poll: 10 * time.Millisecond}
	// Имитация cron-run.sh: ждёт заявку, пишет вывод и итог.
	go func() {
		for i := 0; i < 500; i++ {
			raw, err := os.ReadFile(filepath.Join(q, "42.req"))
			if err == nil {
				_ = os.WriteFile(filepath.Join(r, "42.out"), []byte("ответ "+string(raw[:4])), 0o644)
				_ = os.WriteFile(filepath.Join(r, "42.res"), []byte("exit=3\ntimeout=0\n"), 0o644)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	res, err := fr.Run(bg, 42, "blog.vlad.vladinc.ru", "echo 'a b' $HOME; false", 5*time.Second)
	if err != nil || res.Exit != 3 || res.TimedOut || !strings.HasPrefix(res.Output, "ответ host") {
		t.Fatalf("%+v %v", res, err)
	}
	if left, _ := filepath.Glob(filepath.Join(q, ".*")); len(left) != 0 {
		t.Fatalf("временные файлы остались: %v", left)
	}
	raw, _ := os.ReadFile(filepath.Join(q, "42.req"))
	if !strings.Contains(string(raw), "host=blog.vlad.vladinc.ru\ntimeout=5\ncmd=") || strings.Contains(string(raw), "echo") {
		t.Fatalf("заявка: %q", raw)
	}
}

func TestFileRunnerCancelRemovesRequest(t *testing.T) {
	q, r := t.TempDir(), t.TempDir()
	fr := FileRunner{Queue: q, Results: r, Poll: 10 * time.Millisecond}
	ctx, cancel := context.WithTimeout(bg, 100*time.Millisecond)
	defer cancel()
	if _, err := fr.Run(ctx, 7, "a.b.vladinc.ru", "true", time.Second); err == nil {
		t.Fatal("без ответа должна быть ошибка")
	}
	if _, err := os.Stat(filepath.Join(q, "7.req")); err == nil {
		t.Fatal("отменённая заявка должна удаляться")
	}
}

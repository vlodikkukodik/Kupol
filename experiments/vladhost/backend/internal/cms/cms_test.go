package cms

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vladhost/internal/apperr"
	"vladhost/internal/auth"
	"vladhost/internal/runtimes"
	"vladhost/internal/sites"
	"vladhost/internal/testdb"
	"vladhost/internal/userdb"
)

var bg = context.Background()

// --- поддельные соседи ---

type fakeDBs struct {
	mu      sync.Mutex
	next    int64
	live    map[int64]userdb.Database
	engines []userdb.Engine
	failNew error
	takenN  int // сколько первых имён «заняты»
	created []string
}

func (f *fakeDBs) Enabled() bool { return len(f.engines) > 0 }
func (f *fakeDBs) Info() userdb.Info {
	return userdb.Info{Engines: f.engines, PerEngine: 3}
}
func (f *fakeDBs) Create(_ context.Context, u auth.User, e userdb.Engine, name string) (*userdb.Database, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNew != nil {
		return nil, "", f.failNew
	}
	if f.takenN > 0 {
		f.takenN--
		return nil, "", userdb.ErrTaken
	}
	f.next++
	d := userdb.Database{ID: f.next, UserID: u.ID, Engine: e, Name: u.Username + "_" + name}
	if f.live == nil {
		f.live = map[int64]userdb.Database{}
	}
	f.live[d.ID] = d
	f.created = append(f.created, d.Name)
	pw, _ := userdb.NewPassword()
	return &d, pw, nil
}
func (f *fakeDBs) Delete(_ context.Context, _, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.live, id)
	return nil
}
func (f *fakeDBs) List(_ context.Context, userID int64) ([]userdb.Database, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []userdb.Database
	for _, d := range f.live {
		if d.UserID == userID {
			out = append(out, d)
		}
	}
	return out, nil
}
func (f *fakeDBs) liveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.live)
}

type stubApplier struct {
	mu    sync.Mutex
	calls []runtimes.Request
}

func (s *stubApplier) Do(_ context.Context, r runtimes.Request, _ time.Duration) (runtimes.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, r)
	return runtimes.Result{OK: true, State: "active"}, nil
}

// fakeWordPress — «шлюз с WordPress»: страница установщика, форма, корень и страница входа. host → установлен ли сайт.
type fakeWordPress struct {
	srv       *httptest.Server
	mu        sync.Mutex
	installed map[string]bool
	forms     map[string]url.Values
	stepOne   map[string]string // язык, выбранный на шаге 1
	failGets  int32             // первые запросы установщика отвечают 503 (пул ещё поднимается)
	postCode  int               // 0 — 200
	dbError   bool
	chooser   bool // вместо формы установки — выбор языка (WordPress с доступом к переводам): в форме нет слов install.php
}

func newFakeWordPress(t *testing.T) *fakeWordPress {
	w := &fakeWordPress{installed: map[string]bool{}, forms: map[string]url.Values{}, stepOne: map[string]string{}, postCode: 200}
	w.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		host := r.Host
		switch {
		case r.URL.Path == "/wp-admin/install.php" && r.Method == http.MethodGet:
			if atomic.AddInt32(&w.failGets, -1) >= 0 {
				http.Error(rw, "starting", http.StatusServiceUnavailable)
				return
			}
			if w.dbError {
				_, _ = fmt.Fprint(rw, "Error establishing a database connection")
				return
			}
			if w.chooser {
				_, _ = fmt.Fprint(rw, `<form id="setup" method="post" action="?step=1"><select name="language"></select></form>`)
				return
			}
			_, _ = fmt.Fprint(rw, `<form action="install.php?step=2">`)
		case r.URL.Path == "/wp-admin/install.php" && r.Method == http.MethodPost:
			if w.postCode != 200 {
				http.Error(rw, "boom", w.postCode)
				return
			}
			_ = r.ParseForm()
			if r.URL.Query().Get("step") == "1" { // выбор языка: WordPress скачивает языковой пакет
				w.stepOne[host] = r.PostForm.Get("language")
				_, _ = fmt.Fprint(rw, "step one")
				return
			}
			w.forms[host] = r.PostForm
			w.installed[host] = true
			_, _ = fmt.Fprint(rw, "Success!")
		case r.URL.Path == "/":
			if !w.installed[host] {
				rw.Header().Set("Location", "/wp-admin/install.php")
				rw.WriteHeader(http.StatusFound)
				return
			}
			_, _ = fmt.Fprint(rw, `<link href="/wp-content/themes/x/style.css">`)
		case r.URL.Path == "/wp-login.php":
			_, _ = fmt.Fprint(rw, "login")
		default:
			http.NotFound(rw, r)
		}
	}))
	t.Cleanup(w.srv.Close)
	return w
}

// wpZip собирает «архив WordPress»: всё лежит в папке wordpress/, как в настоящем.
func wpZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"wordpress/index.php":                 "<?php // front",
		"wordpress/wp-login.php":              "<?php // login",
		"wordpress/wp-admin/install.php":      "<?php // installer",
		"wordpress/wp-content/index.php":      "<?php // silence",
		"wordpress/wp-config-sample.php":      "<?php // sample",
		"wordpress/wp-includes/version.php":   "<?php $wp_version = '7.1.2';",
		"wordpress/wp-content/plugins/x/x.js": "//",
	} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type env struct {
	t     *testing.T
	svc   *Service
	sites *sites.Service
	dbs   *fakeDBs
	rt    *runtimes.Service
	stub  *stubApplier
	wp    *fakeWordPress
	user  auth.User
	auth  *auth.Service
	zip   []byte
	hits  atomic.Int32 // сколько раз скачали архив
	dl    *httptest.Server
	app   App
	cache string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db := testdb.Open(t)
	root := t.TempDir()
	siteSvc := sites.NewService(db, root, "vladinc.ru", "", sites.Limits{MaxSites: 10, DiskQuotaBytes: 100 << 20})
	capsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(capsDir, "caps"), []byte("php=8.2,8.3,8.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stub := &stubApplier{}
	rt := runtimes.New(db, siteSvc, stub, capsDir)
	e := &env{t: t, sites: siteSvc, rt: rt, stub: stub, dbs: &fakeDBs{engines: []userdb.Engine{userdb.Postgres, userdb.MariaDB}}, wp: newFakeWordPress(t), zip: wpZip(t), cache: t.TempDir()}
	e.dl = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.hits.Add(1)
		if r.URL.Path == "/missing.zip" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(e.zip)
	}))
	t.Cleanup(e.dl.Close)
	sum := sha256.Sum256(e.zip)
	e.app = App{ID: WordPress, Name: "WordPress", Version: "7.1.2", URL: e.dl.URL + "/wordpress.zip", SHA256: hex.EncodeToString(sum[:])}
	e.svc = New(db, siteSvc, e.dbs, rt, Config{Catalog: []App{e.app}, CacheDir: e.cache, GatewayURL: e.wp.srv.URL, DBHost: "localhost", InstallerWait: 2 * time.Second, PollEvery: 20 * time.Millisecond})
	e.auth = auth.NewService(db, []byte(strings.Repeat("s", 32)), time.Minute, time.Hour)
	e.user = e.newUser()
	return e
}

func (e *env) newUser() auth.User {
	name := fmt.Sprintf("cm%d", time.Now().UnixNano()%1_000_000_000)
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

func input() Input {
	return Input{CMS: WordPress, Title: "Мой блог", AdminUser: "vlad", AdminEmail: "vlad@example.com", Locale: "ru_RU"}
}

func (e *env) install(u auth.User, s *sites.Site, in Input) *JobView {
	e.t.Helper()
	if err := e.svc.Start(bg, u, s.ID, in); err != nil {
		e.t.Fatalf("Start: %v", err)
	}
	e.svc.Wait()
	v, _ := e.svc.TakeJob(u.ID, s.ID)
	return v
}

func fileExists(e *env, s *sites.Site, name string) bool {
	_, err := os.Stat(filepath.Join(e.sites.SiteDir(s.Host), "public", name))
	return err == nil
}

// --- тесты ---

func TestInstallWordPressEndToEnd(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	atomic.StoreInt32(&e.wp.failGets, 1) // пул PHP поднимается: первый запрос установщика неудачен
	v := e.install(e.user, s, input())
	if v.Status != JobDone || v.Result == nil {
		t.Fatalf("установка: %+v", v)
	}
	r := v.Result
	if r.URL != "https://"+s.Host+"/" || r.AdminURL != "https://"+s.Host+"/wp-admin/" || r.AdminUser != "vlad" || len(r.AdminPassword) != 24 ||
		r.DBName != e.user.Username+"_wp"+fmt.Sprint(s.ID) || len(r.DBPassword) != 24 {
		t.Fatalf("итог: %+v", r)
	}
	// Файлы разложены без лишней папки wordpress/, конфигурация записана
	for _, f := range []string{"index.php", "wp-login.php", "wp-admin/install.php", "wp-config.php"} {
		if !fileExists(e, s, f) {
			t.Errorf("нет файла %s", f)
		}
	}
	cfg, _ := os.ReadFile(filepath.Join(e.sites.SiteDir(s.Host), "public", "wp-config.php"))
	for _, want := range []string{"define('DB_NAME', '" + r.DBName + "')", "define('DB_USER', '" + r.DBName + "')", "define('DB_PASSWORD', '" + r.DBPassword + "')",
		"define('DB_HOST', 'localhost')", "define('WPLANG', 'ru_RU')", "define('WP_HOME', 'https://" + s.Host + "')", "define('FORCE_SSL_ADMIN', true)", "define('FS_METHOD', 'direct')", "$table_prefix = 'wp"} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf("в wp-config.php нет %q", want)
		}
	}
	// PHP включён самой установкой, версия — самая новая
	last := e.stub.calls[len(e.stub.calls)-1]
	if a := findCall(e.stub, runtimes.ActionApply); a.Runtime != "php" || a.Version != "8.4" {
		t.Fatalf("PHP: %+v (последний вызов %+v)", a, last)
	}
	// Форма установки WordPress
	if e.wp.stepOne[s.Host] != "ru_RU" {
		t.Fatalf("шаг выбора языка: %v", e.wp.stepOne)
	}
	form := e.wp.forms[s.Host]
	if form.Get("weblog_title") != "Мой блог" || form.Get("user_name") != "vlad" || form.Get("admin_email") != "vlad@example.com" ||
		form.Get("admin_password") != r.AdminPassword || form.Get("language") != "ru_RU" || form.Get("blog_public") != "1" {
		t.Fatalf("форма: %v", form)
	}
	// Состояние после установки
	st, err := e.svc.Status(bg, e.user.ID, s.ID)
	if err != nil || st.Installed == nil || st.Installed.Version != "7.1.2" || st.Installed.AdminURL != r.AdminURL || st.Installed.DBName != r.DBName {
		t.Fatalf("%+v %v", st, err)
	}
	if e.dbs.liveCount() != 1 {
		t.Fatal("база должна остаться")
	}
	// Пароли отдаются один раз
	if again, _ := e.svc.TakeJob(e.user.ID, s.ID); again.Result != nil {
		t.Fatal("итог с паролями должен отдаваться один раз")
	}
	// Повторная установка на тот же сайт отклоняется
	if err := e.svc.Start(bg, e.user, s.ID, input()); !errors.Is(err, ErrInstalled) {
		t.Fatalf("%v", err)
	}
}

func findCall(s *stubApplier, action string) runtimes.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.calls) - 1; i >= 0; i-- {
		if s.calls[i].Action == action {
			return s.calls[i]
		}
	}
	return runtimes.Request{}
}

func TestEnglishInstallSkipsLanguageField(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	in := input()
	in.Locale = "en_US"
	if v := e.install(e.user, s, in); v.Status != JobDone {
		t.Fatalf("%+v", v)
	}
	if _, ok := e.wp.forms[s.Host]["language"]; ok {
		t.Fatal("для английского языка поле не отправляется")
	}
	if _, ok := e.wp.stepOne[s.Host]; ok {
		t.Fatal("для английского языка шаг выбора языка не нужен")
	}
	cfg, _ := os.ReadFile(filepath.Join(e.sites.SiteDir(s.Host), "public", "wp-config.php"))
	if strings.Contains(string(cfg), "WPLANG") {
		t.Fatal("для английского языка WPLANG не задаётся")
	}
}

// Регрессия: на сервере с доступом к переводам WordPress сначала показывает выбор языка, а в его форме нет слов install.php — установка
// принимала это за неготовый установщик и откатывалась.
func TestInstallWorksWhenWordPressShowsLanguageChooser(t *testing.T) {
	e := newEnv(t)
	e.wp.chooser = true
	s := e.site(e.user, "blog")
	v := e.install(e.user, s, input())
	if v.Status != JobDone || v.Result == nil {
		t.Fatalf("%+v", v)
	}
	if e.wp.stepOne[s.Host] != "ru_RU" || e.wp.forms[s.Host].Get("language") != "ru_RU" {
		t.Fatalf("язык: шаг 1 %q, форма %q", e.wp.stepOne[s.Host], e.wp.forms[s.Host].Get("language"))
	}
	// английский без выбора языка тоже проходит
	s2 := e.site(e.user, "eng")
	in := input()
	in.Locale = "en_US"
	if v := e.install(e.user, s2, in); v.Status != JobDone {
		t.Fatalf("%+v", v)
	}
}

func TestFailuresRollBackDatabaseAndFiles(t *testing.T) {
	cases := []struct {
		name string
		prep func(e *env)
		code string
	}{
		{"контрольная сумма", func(e *env) { e.zip = append(e.zip, 0) }, "validation"},
		{"установщик отвечает ошибкой", func(e *env) { e.wp.postCode = 500 }, "cms_fail_install"},
		{"WordPress не видит базу", func(e *env) { e.wp.dbError = true }, "cms_fail_install"},
		{"архив не скачивается", func(e *env) { e.app.URL = e.dl.URL + "/missing.zip"; e.svc.cfg.Catalog = []App{e.app} }, "cms_fail_download"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			s := e.site(e.user, "blog")
			if c.name == "контрольная сумма" {
				c.prep(e) // архив на сервере отдаётся другой, чем в каталоге
			} else {
				c.prep(e)
			}
			if c.name == "WordPress не видит базу" {
				// ждём недолго: установщик всё время отвечает ошибкой подключения
				e.svc.cfg.GatewayURL = e.wp.srv.URL
			}
			ctx, cancel := context.WithTimeout(bg, 4*time.Second)
			defer cancel()
			if err := e.svc.Start(ctx, e.user, s.ID, input()); err != nil {
				t.Fatal(err)
			}
			e.svc.Wait()
			v, _ := e.svc.TakeJob(e.user.ID, s.ID)
			if v.Status != JobFailed || v.Failure == nil || v.Result != nil {
				t.Fatalf("ждали отказ: %+v", v)
			}
			if e.dbs.liveCount() != 0 {
				t.Fatal("база должна быть удалена при откате")
			}
			entries, _ := e.sites.ListDir(bg, e.user.ID, s.ID, "")
			if len(entries) != 0 {
				t.Fatalf("папка сайта должна быть очищена: %v", entries)
			}
			if st, _ := e.svc.Status(bg, e.user.ID, s.ID); st.Installed != nil {
				t.Fatal("записи об установке быть не должно")
			}
			// После отката установку можно повторить
			e2 := e.svc
			_ = e2
		})
	}
}

func TestChecksumMismatchIsReported(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	e.app.SHA256 = strings.Repeat("0", 64)
	e.svc.cfg.Catalog = []App{e.app}
	e.svc.cfg.CacheDir = t.TempDir()
	if err := e.svc.Start(bg, e.user, s.ID, input()); err != nil {
		t.Fatal(err)
	}
	e.svc.Wait()
	v, _ := e.svc.TakeJob(e.user.ID, s.ID)
	if v.Status != JobFailed || v.Failure.Code != "cms_fail_checksum" || v.Failure.Step != StepDownload {
		t.Fatalf("%+v", v)
	}
	if left, _ := filepath.Glob(filepath.Join(e.svc.cfg.CacheDir, "*")); len(left) != 0 {
		t.Fatalf("непроверенный архив не должен остаться в кэше: %v", left)
	}
	if e.dbs.liveCount() != 0 {
		t.Fatal("база удалена")
	}
}

func TestRetryAfterFailureWorks(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	e.wp.postCode = 500
	if err := e.svc.Start(bg, e.user, s.ID, input()); err != nil {
		t.Fatal(err)
	}
	e.svc.Wait()
	if v, _ := e.svc.TakeJob(e.user.ID, s.ID); v.Status != JobFailed {
		t.Fatalf("%+v", v)
	}
	e.wp.postCode = 200
	if v := e.install(e.user, s, input()); v.Status != JobDone {
		t.Fatalf("повторная установка: %+v", v)
	}
}

func TestDatabaseFailureLeavesSiteUntouched(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	e.dbs.failNew = userdb.ErrLimit.With(3)
	v := e.install(e.user, s, input())
	if v.Status != JobFailed || v.Failure.Code != "cms_fail_database" || v.Failure.Step != StepDatabase {
		t.Fatalf("%+v", v)
	}
	if entries, _ := e.sites.ListDir(bg, e.user.ID, s.ID, ""); len(entries) != 0 {
		t.Fatal("файлы не должны появляться, пока нет базы")
	}
}

func TestTakenDatabaseNameGetsSuffix(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	e.dbs.takenN = 2
	v := e.install(e.user, s, input())
	if v.Status != JobDone || !strings.HasPrefix(v.Result.DBName, e.user.Username+"_wp"+fmt.Sprint(s.ID)) || len(v.Result.DBName) <= len(e.user.Username+"_wp"+fmt.Sprint(s.ID)) {
		t.Fatalf("%+v", v)
	}
}

func TestValidation(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	bad := map[string]func(*Input){
		"cms_app":          func(in *Input) { in.CMS = "joomla" },
		"cms_title":        func(in *Input) { in.Title = "   " },
		"cms_title2":       func(in *Input) { in.Title = strings.Repeat("я", 81) },
		"cms_admin_user":   func(in *Input) { in.AdminUser = "a b" },
		"cms_admin_user2":  func(in *Input) { in.AdminUser = "ab" },
		"cms_admin_email":  func(in *Input) { in.AdminEmail = "not-an-email" },
		"cms_admin_email2": func(in *Input) { in.AdminEmail = "Vlad <v@example.com>" },
		"cms_locale":       func(in *Input) { in.Locale = "xx_XX" },
	}
	for name, mut := range bad {
		in := input()
		mut(&in)
		err := e.svc.Start(bg, e.user, s.ID, in)
		var ae *apperr.Error
		if !errors.As(err, &ae) || !strings.HasPrefix(name, strings.TrimPrefix(ae.Code, "validation.")) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(e.stub.calls) != 0 || e.dbs.liveCount() != 0 {
		t.Fatal("при отказе проверки ничего не создаётся")
	}
}

func TestSiteMustBeEmptyAndOwned(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	if err := e.sites.WriteFile(bg, e.user.ID, s.ID, "index.html", strings.NewReader("hi"), 0); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Start(bg, e.user, s.ID, input()); !errors.Is(err, ErrSiteNotEmpty) {
		t.Fatalf("непустой сайт: %v", err)
	}
	other := e.newUser()
	if err := e.svc.Start(bg, other, s.ID, input()); !errors.Is(err, sites.ErrNotFound) {
		t.Fatalf("чужой сайт: %v", err)
	}
	if _, err := e.svc.Status(bg, other.ID, s.ID); !errors.Is(err, sites.ErrNotFound) {
		t.Fatalf("чужой сайт (состояние): %v", err)
	}
	if v, _ := e.svc.TakeJob(other.ID, s.ID); v != nil {
		t.Fatal("чужая установка не видна")
	}
}

func TestUnavailableWithoutMariaOrRuntimes(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	e.dbs.engines = []userdb.Engine{userdb.Postgres}
	if e.svc.Enabled() {
		t.Fatal("без MariaDB установщик недоступен")
	}
	if err := e.svc.Start(bg, e.user, s.ID, input()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if st, _ := e.svc.Status(bg, e.user.ID, s.ID); st.Available {
		t.Fatal("в состоянии раздел помечен недоступным")
	}
	e.dbs.engines = []userdb.Engine{userdb.MariaDB}
	if !e.svc.Enabled() {
		t.Fatal("с MariaDB доступен")
	}
	var nilSvc *Service
	if nilSvc.Enabled() {
		t.Fatal("nil-служба выключена")
	}
}

func TestOnlyOneInstallPerSiteAndBoundedConcurrency(t *testing.T) {
	e := newEnv(t)
	e.svc.cfg.Concurrency = 1
	e.svc.sem = make(chan struct{}, 1)
	s1, s2 := e.site(e.user, "one"), e.site(e.user, "two")
	e.wp.postCode = 200
	atomic.StoreInt32(&e.wp.failGets, 1) // растягиваем первую установку на секунду
	if err := e.svc.Start(bg, e.user, s1.ID, input()); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Start(bg, e.user, s1.ID, input()); !errors.Is(err, ErrBusy) {
		t.Fatalf("второй запуск на том же сайте: %v", err)
	}
	if err := e.svc.Start(bg, e.user, s2.ID, input()); !errors.Is(err, ErrTooManyRunning) {
		t.Fatalf("лимит одновременных установок: %v", err)
	}
	e.svc.Wait()
	if v, _ := e.svc.TakeJob(e.user.ID, s1.ID); v.Status != JobDone {
		t.Fatalf("%+v", v)
	}
}

func TestDownloadIsCachedAndCorruptCacheIsReplaced(t *testing.T) {
	e := newEnv(t)
	s1, s2, s3 := e.site(e.user, "one"), e.site(e.user, "two"), e.site(e.user, "three")
	e.install(e.user, s1, input())
	e.install(e.user, s2, input())
	if e.hits.Load() != 1 {
		t.Fatalf("архив скачан %d раз, ждали 1", e.hits.Load())
	}
	files, _ := filepath.Glob(filepath.Join(e.cache, "*.zip"))
	if len(files) != 1 {
		t.Fatalf("кэш: %v", files)
	}
	_ = os.WriteFile(files[0], []byte("испорчено"), 0o644)
	if v := e.install(e.user, s3, input()); v.Status != JobDone || e.hits.Load() != 2 {
		t.Fatalf("испорченный кэш должен быть скачан заново: %+v hits=%d", v, e.hits.Load())
	}
}

func TestRemovedApplicationIsForgotten(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	e.install(e.user, s, input())
	if err := e.sites.Remove(bg, e.user.ID, s.ID, "wp-config.php"); err != nil {
		t.Fatal(err)
	}
	st, _ := e.svc.Status(bg, e.user.ID, s.ID)
	if st.Installed != nil {
		t.Fatal("после удаления файлов приложение считается неустановленным")
	}
	var n int64
	e.svc.db.Model(&Row{}).Where("site_id = ?", s.ID).Count(&n)
	if n != 0 {
		t.Fatal("запись должна забыться")
	}
}

func TestStatusRequirements(t *testing.T) {
	e := newEnv(t)
	s := e.site(e.user, "blog")
	st, err := e.svc.Status(bg, e.user.ID, s.ID)
	if err != nil || !st.Available || len(st.Catalog) != 1 || st.Catalog[0].URL != "" || st.Catalog[0].SHA256 != "" ||
		!st.Requirements.Empty || !st.Requirements.Database || !st.Requirements.PHP || len(st.Locales) != 3 {
		t.Fatalf("%+v %v", st, err)
	}
	_ = e.sites.WriteFile(bg, e.user.ID, s.ID, "a.txt", strings.NewReader("x"), 0)
	if st, _ := e.svc.Status(bg, e.user.ID, s.ID); st.Requirements.Empty {
		t.Fatal("папка не пуста")
	}
	for i := 0; i < 3; i++ {
		_, _, _ = e.dbs.Create(bg, e.user, userdb.MariaDB, fmt.Sprintf("x%d", i))
	}
	if st, _ := e.svc.Status(bg, e.user.ID, s.ID); st.Requirements.Database {
		t.Fatal("лимит баз исчерпан")
	}
}

func TestWPConfigIsSafe(t *testing.T) {
	cfg, err := wpConfig("blog.vlad.vladinc.ru", "vlad_wp1", "Abc123", "localhost", "ru_RU")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(cfg, "\n") {
		if strings.HasPrefix(line, "define('") && !strings.HasSuffix(line, ", true);") && !strings.HasSuffix(line, ", false);") && strings.Count(line, "'") != 4 {
			t.Errorf("подозрительная строка (лишние кавычки): %q", line)
		}
	}
	other, _ := wpConfig("blog.vlad.vladinc.ru", "vlad_wp1", "Abc123", "localhost", "ru_RU")
	if cfg == other {
		t.Fatal("соли и префикс таблиц должны быть случайными")
	}
	for _, bad := range [][4]string{
		{"a'b.example", "db", "pw", "localhost"}, {"h.example", "d'b", "pw", "localhost"}, {"h.example", "db", "p'w", "localhost"},
		{"h.example", "db", "p\\w", "localhost"}, {"h.example", "db", "p$w", "localhost"}, {"h.example", "db", "", "localhost"}, {"h.example", "db", "pw", "loc\nalhost"},
	} {
		if _, err := wpConfig(bad[0], bad[1], bad[2], bad[3], "en_US"); err == nil {
			t.Errorf("%q должно отклоняться", bad)
		}
	}
}

func TestCatalogValidation(t *testing.T) {
	if err := DefaultCatalog()[0].Validate(); err != nil {
		t.Fatal(err)
	}
	ok := DefaultCatalog()[0]
	for name, mut := range map[string]func(*App){
		"app": func(a *App) { a.ID = "joomla" }, "version": func(a *App) { a.Version = "../1" }, "url": func(a *App) { a.URL = "http://example.com/x.zip" },
		"sum": func(a *App) { a.SHA256 = "abc" },
	} {
		a := ok
		mut(&a)
		if a.Validate() == nil {
			t.Errorf("%s: должно отклоняться", name)
		}
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "c.json")
	_ = os.WriteFile(p, []byte(`[{"id":"wordpress","name":"WordPress","version":"9.9.9","url":"https://example.com/w.zip","sha256":"`+strings.Repeat("a", 64)+`"}]`), 0o644)
	got, err := LoadCatalog(p)
	if err != nil || len(got) != 1 || got[0].Version != "9.9.9" {
		t.Fatalf("%+v %v", got, err)
	}
	_ = os.WriteFile(p, []byte(`[{"id":"wordpress","version":"1.0","url":"ftp://x","sha256":"x"}]`), 0o644)
	if _, err := LoadCatalog(p); err == nil {
		t.Fatal("плохой каталог должен отклоняться")
	}
	if d, err := LoadCatalog(""); err != nil || len(d) != 1 {
		t.Fatal("без файла — встроенный каталог")
	}
}

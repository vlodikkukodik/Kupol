package httpapi_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vladhost/internal/auth"
	"vladhost/internal/cms"
	"vladhost/internal/config"
	"vladhost/internal/httpapi"
	"vladhost/internal/runtimes"
	"vladhost/internal/userdb"
)

// cmsDBs — база данных пользователя «на бумаге»: достаточно, чтобы установщик получил имя и пароль.
type cmsDBs struct {
	mu   sync.Mutex
	next int64
	live map[int64]userdb.Database
}

func (f *cmsDBs) Enabled() bool { return true }
func (f *cmsDBs) Info() userdb.Info {
	return userdb.Info{Engines: []userdb.Engine{userdb.MariaDB}, PerEngine: 3}
}
func (f *cmsDBs) Create(_ context.Context, u auth.User, e userdb.Engine, name string) (*userdb.Database, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	d := userdb.Database{ID: f.next, UserID: u.ID, Engine: e, Name: u.Username + "_" + name}
	if f.live == nil {
		f.live = map[int64]userdb.Database{}
	}
	f.live[d.ID] = d
	pw, _ := userdb.NewPassword()
	return &d, pw, nil
}
func (f *cmsDBs) Delete(_ context.Context, _, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.live, id)
	return nil
}
func (f *cmsDBs) List(_ context.Context, uid int64) ([]userdb.Database, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []userdb.Database
	for _, d := range f.live {
		if d.UserID == uid {
			out = append(out, d)
		}
	}
	return out, nil
}

type cmsJobView struct {
	Status  string   `json:"status"`
	Step    string   `json:"step"`
	Steps   []string `json:"steps"`
	Failure *struct {
		Code, Message, Step string
	} `json:"failure"`
	Result *struct {
		URL           string `json:"url"`
		AdminURL      string `json:"admin_url"`
		AdminUser     string `json:"admin_user"`
		AdminPassword string `json:"admin_password"`
		DBName        string `json:"db_name"`
		DBPassword    string `json:"db_password"`
	} `json:"result"`
}

type cmsStatus struct {
	Available bool `json:"available"`
	Catalog   []struct {
		ID, Name, Version string
	} `json:"catalog"`
	Locales   []string `json:"locales"`
	Installed *struct {
		CMS      string `json:"cms"`
		Version  string `json:"version"`
		AdminURL string `json:"admin_url"`
	} `json:"installed"`
	Requirements struct{ PHP, Database, Empty bool } `json:"requirements"`
}

// withCMS включает установщик: среды выполнения с PHP, «MariaDB», скачивание с локального сервера и «шлюз с WordPress».
func (e *env) withCMS(badSum ...bool) *cms.Service {
	e.t.Helper()
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	for _, n := range []string{"wordpress/index.php", "wordpress/wp-login.php", "wordpress/wp-admin/install.php"} {
		w, _ := zw.Create(n)
		_, _ = w.Write([]byte("<?php"))
	}
	_ = zw.Close()
	sum := sha256.Sum256(zbuf.Bytes())
	if len(badSum) > 0 && badSum[0] {
		sum[0] ^= 0xff // каталог ждёт другой архив, чем отдаёт сервер
	}
	dl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(zbuf.Bytes()) }))
	e.t.Cleanup(dl.Close)

	var mu sync.Mutex
	installed := map[string]bool{}
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/wp-admin/install.php" && r.Method == http.MethodGet:
			_, _ = fmt.Fprint(w, "install.php")
		case r.URL.Path == "/wp-admin/install.php":
			installed[r.Host] = true
			_, _ = fmt.Fprint(w, "Success!")
		case r.URL.Path == "/" && installed[r.Host]:
			_, _ = fmt.Fprint(w, "wp-content")
		case r.URL.Path == "/":
			w.Header().Set("Location", "/wp-admin/install.php")
			w.WriteHeader(http.StatusFound)
		default:
			_, _ = fmt.Fprint(w, "ok")
		}
	}))
	e.t.Cleanup(gw.Close)

	dir := e.t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "caps"), []byte("php=8.3\n"), 0o644); err != nil {
		e.t.Fatal(err)
	}
	rt := runtimes.New(e.db, e.sites, &stubApplier{}, dir)
	svc := cms.New(e.db, e.sites, &cmsDBs{}, rt, cms.Config{
		Catalog:       []cms.App{{ID: cms.WordPress, Name: "WordPress", Version: "7.1.2", URL: dl.URL + "/wp.zip", SHA256: hex.EncodeToString(sum[:])}},
		GatewayURL:    gw.URL,
		InstallerWait: 2 * time.Second,
		PollEvery:     20 * time.Millisecond,
	})
	e.r = httpapi.New(e.svc, e.sites, config.Config{JWTSecret: []byte(strings.Repeat("s", 32)), AuthPerMinute: 10000, AuthBurst: 10000},
		httpapi.WithRuntimes(rt), httpapi.WithCMS(svc))
	return svc
}

func TestCMSHiddenWhenNotConfigured(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	site, _ := e.createSite(tok, "blog")
	for _, tc := range [][2]string{{"GET", ""}, {"POST", ""}, {"GET", "/job"}} {
		if w := e.do(tc[0], fmt.Sprintf("/api/sites/%d/cms%s", site, tc[1]), map[string]string{}, tok); w.Code != 404 {
			t.Errorf("%s %s: %d", tc[0], tc[1], w.Code)
		}
	}
	if list := decode[struct {
		CMSAvailable bool `json:"cms_available"`
	}](t, e.do("GET", "/api/sites", nil, tok)); list.CMSAvailable {
		t.Fatal("установщик не должен быть помечен доступным")
	}
}

func TestCMSInstallThroughAPI(t *testing.T) {
	e := newEnv(t)
	svc := e.withCMS()
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	site, host := e.createSite(john, "blog")
	base := fmt.Sprintf("/api/sites/%d/cms", site)

	if w := e.do("GET", base, nil, ""); w.Code != 401 {
		t.Fatalf("без входа: %d", w.Code)
	}
	if list := decode[struct {
		CMSAvailable bool `json:"cms_available"`
	}](t, e.do("GET", "/api/sites", nil, john)); !list.CMSAvailable {
		t.Fatal("установщик должен быть доступен")
	}
	st := decode[struct{ CMS cmsStatus }](t, e.do("GET", base, nil, john)).CMS
	if !st.Available || len(st.Catalog) != 1 || st.Catalog[0].Version != "7.1.2" || st.Installed != nil || !st.Requirements.Empty || len(st.Locales) != 3 {
		t.Fatalf("%+v", st)
	}
	if w := e.do("GET", base+"/job", nil, john); !strings.Contains(w.Body.String(), `"job":null`) {
		t.Fatalf("до установки задания нет: %s", w.Body)
	}

	// Ошибки проверки — на двух языках, с привязкой к полю
	bad := map[string]any{"cms": "wordpress", "title": "Блог", "admin_user": "a b", "admin_email": "v@example.com", "locale": "ru_RU"}
	w := e.doLang("ru", "POST", base, bad, john)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "Логин администратора") || !strings.Contains(w.Body.String(), `"field":"admin_user"`) {
		t.Fatalf("ru: %d %s", w.Code, w.Body)
	}
	if w := e.doLang("it", "POST", base, bad, john); w.Code != 422 || !strings.Contains(w.Body.String(), "Login dell'amministratore") {
		t.Fatalf("it: %d %s", w.Code, w.Body)
	}

	// Чужой сайт
	good := map[string]any{"cms": "wordpress", "title": "Мой блог", "admin_user": "vlad", "admin_email": "v@example.com", "locale": "ru_RU"}
	for _, tc := range [][2]string{{"GET", ""}, {"POST", ""}, {"GET", "/job"}} {
		if w := e.do(tc[0], base+tc[1], good, mary); w.Code != 404 {
			t.Errorf("чужой %s %s: %d", tc[0], tc[1], w.Code)
		}
	}

	// Установка: 202 → опрос до завершения
	if w := e.do("POST", base, good, john); w.Code != 202 {
		t.Fatalf("запуск: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", base, good, john); w.Code != 409 || !strings.Contains(w.Body.String(), "cms_busy") {
		// установка могла уже закончиться: тогда отказ — «уже установлено»
		if w.Code != 409 || !strings.Contains(w.Body.String(), "cms_installed") {
			t.Fatalf("повторный запуск: %d %s", w.Code, w.Body)
		}
	}
	svc.Wait()
	first := decode[struct{ Job cmsJobView }](t, e.do("GET", base+"/job", nil, john)).Job
	if first.Status != "done" || first.Result == nil || len(first.Steps) != 7 {
		t.Fatalf("итог: %+v", first)
	}
	r := first.Result
	if r.URL != "https://"+host+"/" || r.AdminUser != "vlad" || len(r.AdminPassword) != 24 || len(r.DBPassword) != 24 || !strings.HasPrefix(r.DBName, "john_wp") {
		t.Fatalf("результат: %+v", r)
	}
	// Пароли — один раз
	if second := decode[struct{ Job cmsJobView }](t, e.do("GET", base+"/job", nil, john)).Job; second.Result != nil || second.Status != "done" {
		t.Fatalf("повторное чтение: %+v", second)
	}
	st = decode[struct{ CMS cmsStatus }](t, e.do("GET", base, nil, john)).CMS
	if st.Installed == nil || st.Installed.CMS != "wordpress" || st.Installed.AdminURL != "https://"+host+"/wp-admin/" {
		t.Fatalf("после установки: %+v", st)
	}
	if w := e.do("GET", base, nil, mary); w.Code != 404 {
		t.Fatalf("чужое состояние: %d", w.Code)
	}
}

func TestCMSFailureMessageIsLocalized(t *testing.T) {
	e := newEnv(t)
	svc := e.withCMS(true)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	site, _ := e.createSite(john, "blog")
	base := fmt.Sprintf("/api/sites/%d/cms", site)
	good := map[string]any{"cms": "wordpress", "title": "Blog", "admin_user": "vlad", "admin_email": "v@example.com", "locale": "it_IT"}
	if w := e.do("POST", base, good, john); w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	svc.Wait()
	for lang, want := range map[string]string{"it": "Il checksum dell'archivio scaricato non coincide", "ru": "Контрольная сумма скачанного архива не совпала"} {
		w := e.doLang(lang, "GET", base+"/job", nil, john)
		job := decode[struct{ Job cmsJobView }](t, w).Job
		if job.Status != "failed" || job.Failure == nil || job.Failure.Code != "cms_fail_checksum" || job.Failure.Step != "download" || !strings.Contains(job.Failure.Message, want) || job.Result != nil {
			t.Fatalf("%s: %+v", lang, job)
		}
	}
	// После отказа установка возможна снова: сайт пуст, записи нет
	st := decode[struct{ CMS cmsStatus }](t, e.do("GET", base, nil, john)).CMS
	if st.Installed != nil || !st.Requirements.Empty {
		t.Fatalf("%+v", st)
	}
}

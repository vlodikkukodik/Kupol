package cms

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vladhost/internal/apperr"
	"vladhost/internal/auth"
	"vladhost/internal/runtimes"
	"vladhost/internal/userdb"
)

// Отказы по шагам. Каждый — отдельная запись с литеральным кодом: сторож i18n сверяет коды с каталогами сообщений.
var (
	ErrFailRuntime  = apperr.New(http.StatusBadGateway, "cms_fail_runtime", "could not enable PHP")
	ErrFailDatabase = apperr.New(http.StatusBadGateway, "cms_fail_database", "could not create the database")
	ErrFailDownload = apperr.New(http.StatusBadGateway, "cms_fail_download", "could not download the distribution")
	ErrFailChecksum = apperr.New(http.StatusBadGateway, "cms_fail_checksum", "distribution checksum mismatch")
	ErrFailFiles    = apperr.New(http.StatusBadGateway, "cms_fail_files", "could not unpack files")
	ErrFailConfig   = apperr.New(http.StatusBadGateway, "cms_fail_config", "could not write the configuration")
	ErrFailInstall  = apperr.New(http.StatusBadGateway, "cms_fail_install", "the application installer failed")
	ErrFailVerify   = apperr.New(http.StatusBadGateway, "cms_fail_verify", "the installed site does not respond")
)

// Start проверяет условия и запускает установку в фоне. Возвращает сразу; ход и итог — через TakeJob / Status.
func (s *Service) Start(ctx context.Context, u auth.User, siteID int64, in Input) error {
	if !s.Enabled() {
		return ErrNotFound
	}
	site, err := s.sites.Get(ctx, u.ID, siteID)
	if err != nil {
		return err
	}
	app, ok := s.app(in.CMS)
	if !ok {
		return ErrUnknownApp
	}
	if err := validate(&in); err != nil {
		return err
	}
	if row, err := s.row(ctx, site); err != nil {
		return err
	} else if row != nil {
		return ErrInstalled
	}
	empty, err := s.publicEmpty(ctx, u.ID, siteID)
	if err != nil {
		return err
	}
	if !empty {
		return ErrSiteNotEmpty
	}
	if !s.hasMaria() {
		return ErrNoDatabase
	}
	if len(s.rt.Caps().PHP) == 0 {
		return ErrNoPHP
	}

	s.mu.Lock()
	if j := s.jobs[siteID]; j != nil && j.Status == JobRunning {
		s.mu.Unlock()
		return ErrBusy
	}
	running := 0
	for _, j := range s.jobs {
		if j.Status == JobRunning {
			running++
		}
	}
	if running >= s.cfg.Concurrency {
		s.mu.Unlock()
		return ErrTooManyRunning
	}
	job := &Job{SiteID: siteID, UserID: u.ID, Status: JobRunning, Step: StepRuntime, Started: s.now()}
	s.jobs[siteID] = job
	s.mu.Unlock()

	s.wg.Go(func() {
		s.sem <- struct{}{}
		defer func() { <-s.sem }()
		s.run(context.WithoutCancel(ctx), u, site.Host, app, in, job)
	})
	return nil
}

// undo — действия отката, которые выполняются в обратном порядке, если установка сорвалась.
type undo struct{ fns []func() }

func (u *undo) add(f func()) { u.fns = append(u.fns, f) }
func (u *undo) run() {
	for i := len(u.fns) - 1; i >= 0; i-- {
		u.fns[i]()
	}
}

func (s *Service) fail(j *Job, e *apperr.Error, detail string) {
	s.mu.Lock()
	j.Status, j.Err, j.Detail, j.Ended = JobFailed, e, detail, s.now()
	s.mu.Unlock()
}

// run выполняет шаги. Любой отказ откатывает уже сделанное (база, файлы), чтобы установку можно было повторить с чистого листа.
func (s *Service) run(ctx context.Context, u auth.User, host string, app App, in Input, job *Job) {
	var rollback undo
	failed := true
	defer func() {
		if failed {
			rollback.run()
		}
	}()
	step := func(name string) { s.setStep(job, name) }
	bail := func(e *apperr.Error, err error) {
		log.Printf("cms: установка %s на %s: шаг %s: %v", app.ID, host, job.Step, err)
		if e.Code == ErrFailChecksum.Code { // у этого сообщения нет подробностей: текст ошибки и так известен
			s.fail(job, e, "")
			return
		}
		s.fail(job, e.With(errText(err)), errText(err))
	}
	siteID := job.SiteID

	// 1. PHP
	step(StepRuntime)
	if v, err := s.rt.Get(ctx, u.ID, siteID); err != nil {
		bail(ErrFailRuntime, err)
		return
	} else if v.Runtime != "php" {
		php := s.rt.Caps().PHP
		if _, err := s.rt.Set(ctx, u.ID, siteID, runtimes.Input{Runtime: "php", Version: php[len(php)-1]}); err != nil {
			bail(ErrFailRuntime, err)
			return
		}
	}

	// 2. база данных
	step(StepDatabase)
	db, dbPass, err := s.createDatabase(ctx, u, siteID)
	if err != nil {
		bail(ErrFailDatabase, err)
		return
	}
	rollback.add(func() {
		if err := s.dbs.Delete(context.WithoutCancel(ctx), u.ID, db.ID); err != nil {
			log.Printf("cms: откат: удаление базы %s: %v", db.Name, err)
		}
	})

	// 3. дистрибутив
	step(StepDownload)
	zipPath, err := s.fetch(ctx, app)
	if err != nil {
		e := ErrFailDownload
		if errors.Is(err, errChecksum) {
			e = ErrFailChecksum
		}
		bail(e, err)
		return
	}
	defer func() {
		if s.cfg.CacheDir == "" {
			_ = os.Remove(zipPath)
		}
	}()

	// 4. файлы
	step(StepFiles)
	f, err := os.Open(zipPath)
	if err != nil {
		bail(ErrFailFiles, err)
		return
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		bail(ErrFailFiles, err)
		return
	}
	rollback.add(func() { s.wipe(context.WithoutCancel(ctx), u.ID, siteID) })
	if _, err := s.sites.Deploy(ctx, u.ID, siteID, f, fi.Size()); err != nil {
		bail(ErrFailFiles, err)
		return
	}

	// 5. wp-config.php
	step(StepConfig)
	cfg, err := wpConfig(host, db.Name, dbPass, s.cfg.DBHost, in.Locale)
	if err != nil {
		bail(ErrFailConfig, err)
		return
	}
	if err := s.sites.WriteFile(ctx, u.ID, siteID, "wp-config.php", strings.NewReader(cfg), 0); err != nil {
		bail(ErrFailConfig, err)
		return
	}

	// 6. установка WordPress
	step(StepInstall)
	adminPass, err := userdb.NewPassword()
	if err != nil {
		bail(ErrFailInstall, err)
		return
	}
	if err := s.installWordPress(ctx, host, in, adminPass); err != nil {
		bail(ErrFailInstall, err)
		return
	}

	// 7. проверка
	step(StepVerify)
	if err := s.verify(ctx, host); err != nil {
		bail(ErrFailVerify, err)
		return
	}

	row := Row{SiteID: siteID, CMS: app.ID, Version: app.Version, DBName: db.Name, InstalledAt: s.now()}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		bail(ErrFailVerify, err)
		return
	}
	failed = false
	s.mu.Lock()
	job.Status, job.Ended = JobDone, s.now()
	job.Result = &Result{URL: "https://" + host + "/", AdminURL: "https://" + host + "/wp-admin/", AdminUser: in.AdminUser, AdminPassword: adminPass,
		DBName: db.Name, DBPassword: dbPass}
	s.mu.Unlock()
}

func errText(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	s := err.Error()
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// createDatabase заводит базу MariaDB с именем wp{номер сайта}; если имя занято — с коротким случайным хвостом.
func (s *Service) createDatabase(ctx context.Context, u auth.User, siteID int64) (*userdb.Database, string, error) {
	base := "wp" + itoa(siteID)
	name := base
	for attempt := 0; attempt < 4; attempt++ {
		d, pw, err := s.dbs.Create(ctx, u, userdb.MariaDB, name)
		if err == nil {
			return d, pw, nil
		}
		if !errors.Is(err, userdb.ErrTaken) {
			return nil, "", err
		}
		suffix, rerr := randomString(3, "abcdefghijklmnopqrstuvwxyz0123456789")
		if rerr != nil {
			return nil, "", rerr
		}
		name = base + suffix
	}
	return nil, "", userdb.ErrTaken
}

var errChecksum = errors.New("checksum mismatch")

// fetch возвращает путь к проверенному архиву: из кэша или скачанному заново. Сумма сверяется всегда, в том числе с кэшем.
func (s *Service) fetch(ctx context.Context, app App) (string, error) {
	dir := s.cfg.CacheDir
	if dir == "" {
		d, err := os.MkdirTemp("", "vhcms")
		if err != nil {
			return "", err
		}
		dir = d
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	final := filepath.Join(dir, fmt.Sprintf("%s-%s-%s.zip", app.ID, app.Version, app.SHA256[:12]))
	if sum, err := fileSum(final); err == nil && sum == app.SHA256 {
		return final, nil
	}
	tmp, err := os.CreateTemp(dir, ".dl-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.URL, nil)
	if err != nil {
		_ = tmp.Close()
		return "", err
	}
	req.Header.Set("User-Agent", "Vladhost-Installer/1.0")
	resp, err := s.cfg.HTTP.Do(req)
	if err != nil {
		_ = tmp.Close()
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_ = tmp.Close()
		return "", fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, 512<<20)); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if hex.EncodeToString(h.Sum(nil)) != app.SHA256 {
		return "", errChecksum
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		return "", err
	}
	return final, nil
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// wipe очищает папку сайта после неудачной установки: сайт возвращается в пустое состояние.
func (s *Service) wipe(ctx context.Context, userID, siteID int64) {
	entries, err := s.sites.ListDir(ctx, userID, siteID, "")
	if err != nil {
		log.Printf("cms: откат: чтение папки сайта: %v", err)
		return
	}
	for _, e := range entries {
		if err := s.sites.Remove(ctx, userID, siteID, e.Name); err != nil {
			log.Printf("cms: откат: удаление %s: %v", e.Name, err)
		}
	}
}

// --- установка самого WordPress через шлюз ---

func (s *Service) gatewayClient() *http.Client {
	return &http.Client{
		Timeout:       3 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (s *Service) gatewayRequest(ctx context.Context, method, host, path string, form url.Values) (*http.Response, []byte, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.cfg.GatewayURL, "/")+path, body)
	if err != nil {
		return nil, nil, err
	}
	req.Host = host // шлюз находит сайт по имени
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := s.gatewayClient().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp, data, nil
}

// waitPHP ждёт, пока пул PHP отзовётся (только что включённый пул может подниматься несколько секунд), и возвращает страницу установщика.
func (s *Service) waitInstaller(ctx context.Context, host string) error {
	var last error
	deadline := time.Now().Add(s.cfg.InstallerWait)
	for {
		resp, body, err := s.gatewayRequest(ctx, http.MethodGet, host, "/wp-admin/install.php", nil)
		switch {
		case err != nil:
			last = err
		// Страница установщика бывает двух видов: форма установки (действие install.php?step=2) и выбор языка, когда серверу доступны переводы
		// WordPress (форма id="setup" с действием ?step=1, слова install.php в ней нет).
		case resp.StatusCode == http.StatusOK && (bytes.Contains(body, []byte("install.php")) || bytes.Contains(body, []byte(`id="setup"`))):
			return nil
		case resp.StatusCode == http.StatusOK && bytes.Contains(body, []byte("already installed")):
			return errors.New("wordpress reports it is already installed")
		default:
			last = fmt.Errorf("installer page: HTTP %d", resp.StatusCode)
			if bytes.Contains(body, []byte("database connection")) {
				last = errors.New("wordpress cannot connect to the database")
			}
		}
		if time.Now().After(deadline) {
			return last
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.cfg.PollEvery):
		}
	}
}

// installWordPress отправляет форму установки WordPress (второй шаг install.php). Язык, название, администратор — из выбора пользователя.
func (s *Service) installWordPress(ctx context.Context, host string, in Input, adminPass string) error {
	if err := s.waitInstaller(ctx, host); err != nil {
		return err
	}
	// Языковой пакет WordPress скачивает на шаге 1 (при выборе языка); без него шаг 2 остался бы на английском. Сбой скачивания не
	// останавливает установку: сайт получится на английском, язык можно сменить в админке.
	if in.Locale != "en_US" {
		if _, _, err := s.gatewayRequest(ctx, http.MethodPost, host, "/wp-admin/install.php?step=1", url.Values{"language": {in.Locale}}); err != nil {
			return err
		}
	}
	form := url.Values{
		"weblog_title":    {in.Title},
		"user_name":       {in.AdminUser},
		"admin_password":  {adminPass},
		"admin_password2": {adminPass},
		"admin_email":     {in.AdminEmail},
		"blog_public":     {"1"},
		"Submit":          {"Install WordPress"},
		"language":        {in.Locale},
	}
	if in.Locale == "en_US" {
		form.Del("language")
	}
	resp, body, err := s.gatewayRequest(ctx, http.MethodPost, host, "/wp-admin/install.php?step=2", form)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("install step: HTTP %d", resp.StatusCode)
	}
	if bytes.Contains(body, []byte("database connection")) || bytes.Contains(body, []byte("id=\"error-page\"")) {
		return errors.New("wordpress installer reported an error")
	}
	return nil
}

// verify: после установки корень сайта открывается (а не перенаправляет на установщик), а страница входа отвечает.
func (s *Service) verify(ctx context.Context, host string) error {
	resp, body, err := s.gatewayRequest(ctx, http.MethodGet, host, "/", nil)
	if err != nil {
		return err
	}
	if loc := resp.Header.Get("Location"); resp.StatusCode >= 300 && resp.StatusCode < 400 && strings.Contains(loc, "install.php") {
		return errors.New("site still redirects to the installer")
	}
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("wp-content")) {
		return fmt.Errorf("home page: HTTP %d", resp.StatusCode)
	}
	resp, _, err = s.gatewayRequest(ctx, http.MethodGet, host, "/wp-login.php", nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login page: HTTP %d", resp.StatusCode)
	}
	return nil
}

// --- wp-config.php ---

const saltAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_=+*/[]{}()!@#$%^&|~"

func randomString(n int, alphabet string) (string, error) {
	out := make([]byte, n)
	for i := range out {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		out[i] = alphabet[v.Int64()]
	}
	return string(out), nil
}

// wpConfig собирает wp-config.php. Все значения, попадающие в PHP-строки, — из проверенных алфавитов (имя базы и хоста, пароль,
// соли без кавычек и обратной косой черты), поэтому подставлять их в одинарные кавычки безопасно.
func wpConfig(host, dbName, dbPass, dbHost, locale string) (string, error) {
	for _, v := range []string{host, dbName, dbHost, locale} {
		if strings.ContainsAny(v, "'\\\"$\n\r\x00") || v == "" {
			return "", fmt.Errorf("unsafe value %q", v)
		}
	}
	if strings.ContainsAny(dbPass, "'\\\"$\n\r\x00") || dbPass == "" {
		return "", errors.New("unsafe database password")
	}
	var b strings.Builder
	b.WriteString("<?php\n// Created by the Vladhost panel when WordPress was installed.\n")
	fmt.Fprintf(&b, "define('DB_NAME', '%s');\ndefine('DB_USER', '%s');\ndefine('DB_PASSWORD', '%s');\ndefine('DB_HOST', '%s');\n", dbName, dbName, dbPass, dbHost)
	b.WriteString("define('DB_CHARSET', 'utf8mb4');\ndefine('DB_COLLATE', '');\n\n")
	// Salt-константы не должны содержать одинарной кавычки и обратной косой черты: алфавит их не включает.
	safe := strings.NewReplacer("'", "", "\\", "").Replace(saltAlphabet)
	for _, k := range []string{"AUTH_KEY", "SECURE_AUTH_KEY", "LOGGED_IN_KEY", "NONCE_KEY", "AUTH_SALT", "SECURE_AUTH_SALT", "LOGGED_IN_SALT", "NONCE_SALT"} {
		v, err := randomString(64, safe)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "define('%s', '%s');\n", k, v)
	}
	prefix, err := randomString(5, "abcdefghijklmnopqrstuvwxyz0123456789")
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "\n$table_prefix = 'wp%s_';\n\n", prefix)
	fmt.Fprintf(&b, "define('WP_HOME', 'https://%s');\ndefine('WP_SITEURL', 'https://%s');\n", host, host)
	b.WriteString("define('FORCE_SSL_ADMIN', true);\ndefine('WP_DEBUG', false);\n")
	// Обновления и установка плагинов пишут файлы напрямую: у пула PHP этого сайта есть права на его папку.
	b.WriteString("define('FS_METHOD', 'direct');\ndefine('WP_AUTO_UPDATE_CORE', 'minor');\n")
	// Язык сайта. Установщик WordPress не всегда записывает его в настройки сам, а константа работает, пока администратор не выберет язык в админке.
	if locale != "en_US" {
		fmt.Fprintf(&b, "define('WPLANG', '%s');\n", locale)
	}
	b.WriteString("\n")
	b.WriteString("if (!defined('ABSPATH')) {\n    define('ABSPATH', __DIR__ . '/');\n}\nrequire_once ABSPATH . 'wp-settings.php';\n")
	return b.String(), nil
}

package notify_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"vladhost/internal/auth"
	"vladhost/internal/mailer"
	"vladhost/internal/notify"
	"vladhost/internal/sites"
	"vladhost/internal/testdb"
)

var bg = context.Background()

// fakeSender запоминает письма; поведение задаётся функцией fail.
type fakeSender struct {
	mu    sync.Mutex
	sent  []mailer.Message
	calls atomic.Int32
	fail  func(call int) error
	delay time.Duration
}

func (f *fakeSender) Send(_ context.Context, m mailer.Message) error {
	n := int(f.calls.Add(1))
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.fail != nil {
		if err := f.fail(n); err != nil {
			return err
		}
	}
	f.mu.Lock()
	f.sent = append(f.sent, m)
	f.mu.Unlock()
	return nil
}

func (f *fakeSender) messages() []mailer.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]mailer.Message(nil), f.sent...)
}

type env struct {
	t      *testing.T
	db     *gorm.DB
	svc    *notify.Service
	sender *fakeSender
	sites  *sites.Service
	auth   *auth.Service
	certs  string
	root   string
}

func newEnv(t *testing.T, quota int64) *env {
	t.Helper()
	db := testdb.Open(t)
	sender := &fakeSender{}
	e := &env{t: t, db: db, sender: sender, certs: t.TempDir(), root: t.TempDir()}
	e.svc = notify.New(db, sender, "https://app.example.test/")
	e.sites = sites.NewService(db, e.root, "vladinc.ru", e.certs, sites.Limits{MaxSites: 3, DiskQuotaBytes: quota})
	e.auth = auth.NewService(db, []byte(strings.Repeat("s", 32)), time.Minute, time.Hour)
	return e
}

// user заводит пользователя; verified/notify задают, получит ли он уведомления.
func (e *env) user(name, lang string, verified, notify bool) auth.User {
	e.t.Helper()
	u, err := e.auth.CreateAdmin(bg, name+"@example.com", name, "password123")
	if err != nil {
		e.t.Fatal(err)
	}
	upd := map[string]any{"lang": lang, "notify_email": notify, "role": "user"}
	if !verified {
		upd["email_verified_at"] = nil
	}
	if err := e.db.Model(&auth.User{}).Where("id = ?", u.ID).Updates(upd).Error; err != nil {
		e.t.Fatal(err)
	}
	var fresh auth.User
	if err := e.db.First(&fresh, u.ID).Error; err != nil {
		e.t.Fatal(err)
	}
	return fresh
}

func (e *env) site(u auth.User, slug string) *sites.Site {
	e.t.Helper()
	s, err := e.sites.Create(bg, u, slug)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *env) info(host string, notAfter time.Time) {
	e.t.Helper()
	dir := filepath.Join(e.certs, "info")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	body := fmt.Sprintf("not_before=%s\nnot_after=%s\nissuer=Test\nnames=%s\n", notAfter.Add(-90*24*time.Hour).UTC().Format(time.RFC3339), notAfter.UTC().Format(time.RFC3339), host)
	if err := os.WriteFile(filepath.Join(dir, host), []byte(body), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) scan() {
	e.t.Helper()
	if err := e.svc.Scan(bg, e.sites); err != nil {
		e.t.Fatal(err)
	}
	e.svc.Drain(bg)
}

// ---- шаблоны ----

func TestEveryKindRendersInBothLanguages(t *testing.T) {
	d := notify.Data{Name: "john", Link: "https://app.example.test/x?token=abc", Host: "blog.john.vladinc.ru", Reason: "rate limited", Days: 5, Percent: 93, Used: "465 MB", Total: "500 MB"}
	for _, kind := range notify.Kinds() {
		var subjects []string
		for _, lang := range []string{"ru", "it"} {
			r, err := notify.Render(kind, lang, d)
			if err != nil {
				t.Fatalf("%s/%s: %v", kind, lang, err)
			}
			if r.Subject == "" || r.Text == "" || r.HTML == "" || strings.Contains(r.Subject+r.Text, "{{") || strings.Contains(r.Subject+r.Text, "<no value>") {
				t.Errorf("%s/%s: %+v", kind, lang, r)
			}
			if !strings.Contains(r.Text, "john") && kind != notify.KindCertFailed {
				// имя есть в приветствии всех писем
				t.Errorf("%s/%s: нет имени", kind, lang)
			}
			subjects = append(subjects, r.Subject)
		}
		if subjects[0] == subjects[1] {
			t.Errorf("%s: темы на разных языках совпали: %q", kind, subjects[0])
		}
	}
	// Языки действительно разные, неизвестный — русский.
	it, _ := notify.Render(notify.KindVerifyEmail, "it", d)
	ru, _ := notify.Render(notify.KindVerifyEmail, "ru", d)
	xx, _ := notify.Render(notify.KindVerifyEmail, "fr", d)
	if !strings.Contains(it.Text, "Conferma") || !strings.Contains(ru.Text, "Подтвердите") || xx.Text != ru.Text {
		t.Fatalf("языки: %q %q %q", it.Text, ru.Text, xx.Text)
	}
	if _, err := notify.Render("no_such_kind", "ru", d); err == nil {
		t.Fatal("неизвестный вид письма принят")
	}
}

func TestLinksAndEscaping(t *testing.T) {
	r, err := notify.Render(notify.KindCertFailed, "ru", notify.Data{
		Name: "john", Host: "blog.john.vladinc.ru", Link: "https://app.example.test/sites/1/ssl?a=1&b=2",
		Reason: "<script>alert(1)</script>\r\nBcc: evil@example.com \"quoted\"",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.HTML, "<script>") || !strings.Contains(r.HTML, "&lt;script&gt;") {
		t.Fatalf("HTML не экранирован: %s", r.HTML)
	}
	if !strings.Contains(r.HTML, `<a href="https://app.example.test/sites/1/ssl?a=1&amp;b=2"`) {
		t.Fatalf("ссылка в HTML: %s", r.HTML)
	}
	if strings.ContainsAny(r.Subject, "\r\n") {
		t.Fatalf("перевод строки в теме: %q", r.Subject)
	}
	if strings.Contains(r.Text, "\r\nBcc:") || strings.Contains(r.Text, "\nBcc:") {
		t.Fatalf("причина не свёрнута в одну строку: %q", r.Text)
	}
	long := strings.Repeat("я", 1000)
	r, _ = notify.Render(notify.KindCertFailed, "ru", notify.Data{Name: "j", Host: "h", Reason: long, Link: "https://x"})
	if len(r.Text) > 1500 {
		t.Fatalf("длинная причина не обрезана: %d", len(r.Text))
	}
	// Опасное имя не попадает в тему многострочно.
	r, _ = notify.Render(notify.KindCertFailed, "ru", notify.Data{Name: "j", Host: "a.com\r\nBcc: x@y.z", Link: "https://x", Reason: "r"})
	if strings.ContainsAny(r.Subject, "\r\n") {
		t.Fatalf("тема: %q", r.Subject)
	}
}

func TestExpiryTextsDifferForExpiredCertificate(t *testing.T) {
	soon, _ := notify.Render(notify.KindCertExpiring, "ru", notify.Data{Name: "j", Host: "h.example", Days: 5, Link: "https://x"})
	gone, _ := notify.Render(notify.KindCertExpiring, "ru", notify.Data{Name: "j", Host: "h.example", Days: -2, Link: "https://x"})
	if !strings.Contains(soon.Subject, "через 5") || !strings.Contains(gone.Subject, "истёк") || strings.Contains(gone.Subject, "через") {
		t.Fatalf("%q / %q", soon.Subject, gone.Subject)
	}
}

// ---- очередь ----

func TestOutboxDeliversAndWipesBodies(t *testing.T) {
	e := newEnv(t, 500<<20)
	u := e.user("john", "ru", true, true)
	if err := e.svc.SendVerification(bg, u, "TOKEN123"); err != nil {
		t.Fatal(err)
	}
	if q, _, _, _ := e.svc.Status(bg); q != 1 {
		t.Fatalf("в очереди %d", q)
	}
	if n := e.svc.Drain(bg); n != 1 {
		t.Fatalf("обработано %d", n)
	}
	msgs := e.sender.messages()
	if len(msgs) != 1 || msgs[0].To != "john@example.com" || !strings.Contains(msgs[0].Text, "https://app.example.test/verify-email?token=TOKEN123") {
		t.Fatalf("%+v", msgs)
	}
	q, s, f, _ := e.svc.Status(bg)
	if q != 0 || s != 1 || f != 0 {
		t.Fatalf("очередь %d/%d/%d", q, s, f)
	}
	var row notify.Outbox
	if err := e.db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.TextBody != "" || row.HTMLBody != "" || row.SentAt == nil || row.Attempts != 1 {
		t.Fatalf("после отправки тела (с ссылкой) должны быть стёрты: %+v", row)
	}
	if e.svc.Drain(bg) != 0 {
		t.Fatal("отправленное письмо взято повторно")
	}
}

func TestOutboxRetriesTemporaryFailures(t *testing.T) {
	e := newEnv(t, 500<<20)
	u := e.user("john", "ru", true, true)
	e.sender.fail = func(call int) error {
		if call <= 2 {
			return &mailer.Error{Temporary: true, Err: errors.New("451 try later")}
		}
		return nil
	}
	_ = e.svc.SendPasswordChanged(bg, u)

	e.svc.Drain(bg)
	var row notify.Outbox
	_ = e.db.First(&row).Error
	if row.Status != "queued" || row.Attempts != 1 || !strings.Contains(row.LastError, "451") || !row.NextAttemptAt.After(time.Now().Add(30*time.Second)) {
		t.Fatalf("после первой неудачи письмо ждёт повтора: %+v", row)
	}
	if row.TextBody == "" {
		t.Fatal("пока письмо не отправлено, его тело нужно")
	}
	if e.svc.Drain(bg) != 0 {
		t.Fatal("письмо взято раньше срока повтора")
	}
	// Сдвигаем срок: второй сбой, потом успех.
	for range 2 {
		e.db.Model(&notify.Outbox{}).Where("id = ?", row.ID).Update("next_attempt_at", time.Now().Add(-time.Second))
		e.svc.Drain(bg)
	}
	_ = e.db.First(&row, row.ID).Error
	if row.Status != "sent" || row.Attempts != 3 || row.LastError != "" || len(e.sender.messages()) != 1 {
		t.Fatalf("после успеха: %+v", row)
	}
}

func TestOutboxGivesUpOnPermanentFailureAndAfterMaxAttempts(t *testing.T) {
	e := newEnv(t, 500<<20)
	u := e.user("john", "ru", true, true)
	e.sender.fail = func(int) error { return &mailer.Error{Err: errors.New("550 no such user")} }
	_ = e.svc.SendPasswordChanged(bg, u)
	e.svc.Drain(bg)
	var row notify.Outbox
	_ = e.db.First(&row).Error
	if row.Status != "failed" || row.Attempts != 1 || !strings.Contains(row.LastError, "550") || row.TextBody != "" {
		t.Fatalf("окончательный отказ: %+v", row)
	}
	if e.svc.Drain(bg) != 0 {
		t.Fatal("проваленное письмо взято повторно")
	}

	// Временные сбои без конца: после предела попыток письмо тоже проваливается.
	e2 := newEnv(t, 500<<20)
	u2 := e2.user("mary", "ru", true, true)
	e2.sender.fail = func(int) error { return &mailer.Error{Temporary: true, Err: errors.New("timeout")} }
	_ = e2.svc.SendPasswordChanged(bg, u2)
	for i := 0; i < 10; i++ {
		e2.db.Model(&notify.Outbox{}).Where("status = 'queued'").Update("next_attempt_at", time.Now().Add(-time.Second))
		e2.svc.Drain(bg)
	}
	var r2 notify.Outbox
	_ = e2.db.First(&r2).Error
	if r2.Status != "failed" || r2.Attempts != 6 || r2.TextBody != "" {
		t.Fatalf("после предела попыток: %+v", r2)
	}
}

func TestOutboxIsSafeAgainstParallelWorkers(t *testing.T) {
	e := newEnv(t, 500<<20)
	u := e.user("john", "ru", true, true)
	e.sender.delay = 20 * time.Millisecond
	for range 12 {
		_ = e.svc.SendPasswordChanged(bg, u)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { e.svc.Drain(bg) })
	}
	wg.Wait()
	if got := e.sender.calls.Load(); got != 12 {
		t.Fatalf("отправок %d, ожидали 12: каждое письмо ровно один раз", got)
	}
	if _, sent, _, _ := e.svc.Status(bg); sent != 12 {
		t.Fatalf("отправлено %d", sent)
	}
}

func TestOutboxRecoversAfterCrashViaLease(t *testing.T) {
	e := newEnv(t, 500<<20)
	u := e.user("john", "ru", true, true)
	_ = e.svc.SendPasswordChanged(bg, u)
	// Воркер взял письмо и «упал» до отправки: письмо занято арендой (срок в будущем) и другим воркером не берётся...
	e.db.Model(&notify.Outbox{}).Where("status = 'queued'").Updates(map[string]any{"attempts": 1, "next_attempt_at": time.Now().Add(5 * time.Minute)})
	if e.svc.Drain(bg) != 0 || len(e.sender.messages()) != 0 {
		t.Fatal("письмо под арендой отправлено параллельно")
	}
	// ...а когда аренда истекла, письмо возвращается в очередь и уходит.
	e.db.Model(&notify.Outbox{}).Where("status = 'queued'").Update("next_attempt_at", time.Now().Add(-time.Second))
	if e.svc.Drain(bg) != 1 || len(e.sender.messages()) != 1 {
		t.Fatal("письмо не вернулось в очередь после аренды")
	}
	var row notify.Outbox
	_ = e.db.First(&row).Error
	if row.Attempts != 2 || row.Status != "sent" {
		t.Fatalf("%+v", row)
	}
}

func TestDisabledServiceRefusesToQueue(t *testing.T) {
	e := newEnv(t, 500<<20)
	off := notify.New(e.db, nil, "https://x")
	u := e.user("john", "ru", true, true)
	if off.Enabled() {
		t.Fatal("без отправителя почта выключена")
	}
	if err := off.SendVerification(bg, u, "t"); !errors.Is(err, notify.ErrDisabled) {
		t.Fatalf("%v", err)
	}
	var nilSvc *notify.Service
	if nilSvc.Enabled() {
		t.Fatal("nil-служба должна быть выключенной, а не паниковать")
	}
	if n := off.Drain(bg); n != 0 {
		t.Fatal(n)
	}
	off.Run(bg, time.Millisecond) // вернётся сразу, а не зависнет
}

func TestQueuedMailUsesUsersLanguage(t *testing.T) {
	e := newEnv(t, 500<<20)
	u := e.user("carla", "it", true, true)
	_ = e.svc.SendVerification(bg, u, "T")
	e.svc.Drain(bg)
	m := e.sender.messages()[0]
	if !strings.Contains(m.Subject, "Conferma") || !strings.Contains(m.Text, "Ciao carla") {
		t.Fatalf("%q", m.Subject)
	}
}

// ---- уведомления ----

func TestCertFailureIsMailedOncePerEvent(t *testing.T) {
	e := newEnv(t, 500<<20)
	u := e.user("john", "ru", true, true)
	s := e.site(u, "blog")
	fail := func(at time.Time, reason string) {
		e.db.Exec("UPDATE sites SET cert_status = 'failed', cert_error = ?, cert_requested_at = ? WHERE id = ?", reason, at, s.ID)
	}
	fail(time.Now().Add(-time.Hour), "DNS problem")
	e.scan()
	e.scan()
	e.scan()
	msgs := e.sender.messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Subject, s.Host) || !strings.Contains(msgs[0].Text, "DNS problem") ||
		!strings.Contains(msgs[0].Text, fmt.Sprintf("https://app.example.test/sites/%d/ssl", s.ID)) {
		t.Fatalf("одно письмо на одну неудачу: %+v", msgs)
	}
	// Пользователь повторил выпуск, и он снова не удался — это новое событие.
	fail(time.Now(), "rate limited")
	e.scan()
	if got := len(e.sender.messages()); got != 2 {
		t.Fatalf("новая неудача → второе письмо, писем %d", got)
	}
	// Успех письма не шлёт.
	e.db.Exec("UPDATE sites SET cert_status = 'active', cert_error = '' WHERE id = ?", s.ID)
	e.scan()
	if got := len(e.sender.messages()); got != 2 {
		t.Fatalf("после успеха писем %d", got)
	}
}

func TestDomainCertFailureIsMailed(t *testing.T) {
	e := newEnv(t, 500<<20)
	u := e.user("john", "ru", true, true)
	s := e.site(u, "blog")
	e.db.Exec("INSERT INTO domains (site_id, host, kind, status, error, cert_requested_at) VALUES (?, 'shop.example.com', 'custom', 'failed', 'CAA forbids', now())", s.ID)
	e.scan()
	msgs := e.sender.messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Subject, "shop.example.com") || !strings.Contains(msgs[0].Text, "CAA forbids") {
		t.Fatalf("%+v", msgs)
	}
}

func TestExpiringCertificateGetsWarningThenUrgentNotice(t *testing.T) {
	e := newEnv(t, 500<<20)
	u := e.user("john", "ru", true, true)
	s := e.site(u, "blog")
	e.db.Exec("UPDATE sites SET cert_status = 'active' WHERE id = ?", s.ID)

	e.info(s.Host, time.Now().Add(60*24*time.Hour))
	e.scan()
	if len(e.sender.messages()) != 0 {
		t.Fatal("сертификат в порядке — писем нет")
	}
	e.info(s.Host, time.Now().Add(10*24*time.Hour+time.Hour))
	e.scan()
	e.scan()
	msgs := e.sender.messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Subject, "через 10") {
		t.Fatalf("первое предупреждение: %+v", msgs)
	}
	e.info(s.Host, time.Now().Add(2*24*time.Hour+time.Hour))
	e.scan()
	e.scan()
	if got := len(e.sender.messages()); got != 2 {
		t.Fatalf("срочное второе письмо, писем %d", got)
	}
	// Продлили: новый срок — новая история, старые письма не мешают, но и лишних нет.
	e.info(s.Host, time.Now().Add(80*24*time.Hour))
	e.scan()
	if got := len(e.sender.messages()); got != 2 {
		t.Fatalf("после продления писем %d", got)
	}
	e.info(s.Host, time.Now().Add(9*24*time.Hour))
	e.scan()
	if got := len(e.sender.messages()); got != 3 {
		t.Fatalf("новый сертификат подошёл к концу — новое письмо, писем %d", got)
	}
}

func TestExpiredCertificateIsMailedAsExpired(t *testing.T) {
	e := newEnv(t, 500<<20)
	u := e.user("john", "ru", true, true)
	s := e.site(u, "blog")
	e.db.Exec("UPDATE sites SET cert_status = 'active' WHERE id = ?", s.ID)
	e.info(s.Host, time.Now().Add(-5*time.Hour))
	e.scan()
	msgs := e.sender.messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Subject, "истёк") {
		t.Fatalf("%+v", msgs)
	}
}

func TestDiskWarningAndRearm(t *testing.T) {
	e := newEnv(t, 100<<20) // квота 100 МБ
	u := e.user("john", "it", true, true)
	s := e.site(u, "blog")
	set := func(mb int64) { e.db.Exec("UPDATE sites SET disk_bytes = ? WHERE id = ?", mb<<20, s.ID) }

	set(50)
	e.scan()
	if len(e.sender.messages()) != 0 {
		t.Fatal("50% — ещё не повод")
	}
	set(93)
	e.scan()
	e.scan()
	msgs := e.sender.messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Subject, "93%") || !strings.Contains(msgs[0].Subject, "Lo spazio") || !strings.Contains(msgs[0].Text, "93 MB su 100 MB") {
		t.Fatalf("%+v", msgs)
	}
	set(85) // между порогами: письмо не повторяется и не сбрасывается
	e.scan()
	set(95)
	e.scan()
	if got := len(e.sender.messages()); got != 1 {
		t.Fatalf("между порогами писем %d", got)
	}
	set(40) // освободили место
	e.scan()
	set(92)
	e.scan()
	if got := len(e.sender.messages()); got != 2 {
		t.Fatalf("после освобождения места новое заполнение заслуживает письма: %d", got)
	}
}

func TestNoNotificationsForUnverifiedOrOptedOutUsers(t *testing.T) {
	e := newEnv(t, 100<<20)
	unverified := e.user("uma", "ru", false, true)
	optedOut := e.user("otto", "ru", true, false)
	for _, u := range []auth.User{unverified, optedOut} {
		s := e.site(u, "blog")
		e.db.Exec("UPDATE sites SET cert_status = 'failed', cert_error = 'x', disk_bytes = ? WHERE id = ?", int64(99<<20), s.ID)
	}
	e.scan()
	if got := len(e.sender.messages()); got != 0 {
		t.Fatalf("письма ушли тем, кто их не ждёт: %+v", e.sender.messages())
	}
	// Подтвердил адрес и включил уведомления — письма пошли.
	e.db.Exec("UPDATE users SET email_verified_at = now() WHERE id = ?", unverified.ID)
	e.scan()
	if got := len(e.sender.messages()); got != 2 { // ошибка сертификата и диск
		t.Fatalf("после подтверждения писем %d", got)
	}
	for _, m := range e.sender.messages() {
		if m.To != "uma@example.com" {
			t.Fatalf("письмо не тому: %s", m.To)
		}
	}
}

func TestNotificationsAreSentToTheirOwnersOnly(t *testing.T) {
	e := newEnv(t, 100<<20)
	john := e.user("john", "ru", true, true)
	mary := e.user("mary", "ru", true, true)
	sj := e.site(john, "blog")
	e.site(mary, "shop")
	e.db.Exec("UPDATE sites SET cert_status = 'failed', cert_error = 'boom' WHERE id = ?", sj.ID)
	e.scan()
	msgs := e.sender.messages()
	if len(msgs) != 1 || msgs[0].To != "john@example.com" {
		t.Fatalf("%+v", msgs)
	}
}

func TestFrozenDatabaseIsMailedOncePerFreeze(t *testing.T) {
	e := newEnv(t, 500<<20)
	e.svc.SetDatabaseLimit(100 << 20)
	u := e.user("dbu", "it", true, true)
	frozen := func(at string) {
		e.db.Exec("UPDATE user_databases SET status = 'frozen', frozen_at = ?, size_bytes = ? WHERE name = 'dbu_blog'", at, int64(120<<20))
	}
	if err := e.db.Exec("INSERT INTO user_databases (user_id, engine, name, status) VALUES (?, 'postgres', 'dbu_blog', 'active')", u.ID).Error; err != nil {
		t.Fatal(err)
	}
	e.scan()
	if len(e.sender.messages()) != 0 {
		t.Fatal("активная база — письма нет")
	}
	frozen("2026-09-20 10:00:00+00")
	e.scan()
	e.scan()
	msgs := e.sender.messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Subject, "dbu_blog") || !strings.Contains(msgs[0].Subject, "sola lettura") ||
		!strings.Contains(msgs[0].Text, "120 MB") || !strings.Contains(msgs[0].Text, "100 MB") || !strings.Contains(msgs[0].Text, "https://app.example.test/databases") {
		t.Fatalf("%+v", msgs)
	}
	// Разморозили и заморозили снова — новое событие, новое письмо.
	e.db.Exec("UPDATE user_databases SET status = 'active', frozen_at = NULL")
	e.scan()
	frozen("2026-09-22 10:00:00+00")
	e.scan()
	if got := len(e.sender.messages()); got != 2 {
		t.Fatalf("новое замораживание: писем %d", got)
	}
	// Неподтверждённому адресу и отказавшимся письма не идут.
	e.db.Exec("UPDATE users SET notify_email = false WHERE id = ?", u.ID)
	e.db.Exec("UPDATE user_databases SET frozen_at = '2026-09-23 10:00:00+00'")
	e.scan()
	if got := len(e.sender.messages()); got != 2 {
		t.Fatalf("после отказа от уведомлений писем %d", got)
	}
}

func TestFailingCronJobIsMailedOncePerStreak(t *testing.T) {
	e := newEnv(t, 500<<20)
	u := e.user("crj", "it", true, true)
	if err := e.db.Exec(`INSERT INTO cron_jobs (id, user_id, name, kind, schedule, url, enabled, fail_streak, failing_since)
		VALUES (1, ?, 'ping', 'http', '*/10 * * * *', 'https://example.com', true, 2, '2026-09-20 10:00:00+00')`, u.ID).Error; err != nil {
		t.Fatal(err)
	}
	e.db.Exec("INSERT INTO cron_runs (job_id, status, code, output) VALUES (1, 'failed', 500, 'HTTP 500 Internal Server Error')")
	e.scan()
	if len(e.sender.messages()) != 0 {
		t.Fatal("две неудачи — ещё не поломка")
	}
	e.db.Exec("UPDATE cron_jobs SET fail_streak = 3")
	e.scan()
	e.scan()
	msgs := e.sender.messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Subject, "ping") || !strings.Contains(msgs[0].Text, "3 volte") ||
		!strings.Contains(msgs[0].Text, "HTTP 500") || !strings.Contains(msgs[0].Text, "https://app.example.test/cron") {
		t.Fatalf("%+v", msgs)
	}
	// Серия продолжается — письма нет; после успеха и новой серии — есть.
	e.db.Exec("UPDATE cron_jobs SET fail_streak = 5")
	e.scan()
	e.db.Exec("UPDATE cron_jobs SET fail_streak = 3, failing_since = '2026-09-22 10:00:00+00'")
	e.scan()
	if got := len(e.sender.messages()); got != 2 {
		t.Fatalf("новая серия неудач: писем %d", got)
	}
}

func TestFullMailboxIsMailedOncePerFillAndRearms(t *testing.T) {
	e := newEnv(t, 100<<20)
	john := e.user("john", "ru", true, true)
	mary := e.user("mary", "it", true, true)
	optedOut := e.user("otto", "ru", true, false)
	used := map[string]int64{"info@example.com": 50, "sales@example.com": 95, "mary@example.org": 92, "otto@example.net": 99}
	rows := func(ctx context.Context) ([]notify.MailboxFill, error) {
		return []notify.MailboxFill{
			{UserID: john.ID, Address: "info@example.com", Used: used["info@example.com"] << 20, Quota: 100 << 20},
			{UserID: john.ID, Address: "sales@example.com", Used: used["sales@example.com"] << 20, Quota: 100 << 20},
			{UserID: mary.ID, Address: "mary@example.org", Used: used["mary@example.org"] << 20, Quota: 100 << 20},
			{UserID: optedOut.ID, Address: "otto@example.net", Used: used["otto@example.net"] << 20, Quota: 100 << 20},
			{UserID: john.ID, Address: "nolimit@example.com", Used: 1 << 30, Quota: 0}, // без квоты — не считается
		}, nil
	}
	e.svc.SetMailboxFill(rows)
	e.scan()
	e.scan()
	msgs := e.sender.messages()
	if len(msgs) != 2 {
		t.Fatalf("ждали 2 письма (sales и mary), получили %d: %+v", len(msgs), msgs)
	}
	bySubject := map[string]string{}
	for _, m := range msgs {
		bySubject[m.To] = m.Subject + "|" + m.Text
	}
	if s := bySubject["john@example.com"]; !strings.Contains(s, "sales@example.com") || !strings.Contains(s, "95%") || !strings.Contains(s, "95 MB из 100 MB") || !strings.Contains(s, "/mail") {
		t.Fatalf("письмо владельцу: %q", s)
	}
	if s := bySubject["mary@example.com"]; !strings.Contains(s, "mary@example.org") || !strings.Contains(s, "quasi piena") {
		t.Fatalf("письмо на итальянском: %q", s)
	}
	// между порогами письмо не повторяется, а после освобождения места новое заполнение снова заслуживает письма
	used["sales@example.com"] = 85
	e.scan()
	used["sales@example.com"] = 96
	e.scan()
	if got := len(e.sender.messages()); got != 2 {
		t.Fatalf("между порогами писем %d", got)
	}
	used["sales@example.com"] = 10
	e.scan()
	used["sales@example.com"] = 91
	e.scan()
	if got := len(e.sender.messages()); got != 3 {
		t.Fatalf("после освобождения места: %d", got)
	}
}

func TestTicketMailsGoToTheRightPeopleInTheirLanguage(t *testing.T) {
	e := newEnv(t, 100<<20)
	staff := e.user("boss", "ru", true, true)
	quiet := e.user("quiet", "ru", true, false) // отказался от писем
	unverified := e.user("newbie", "ru", false, true)
	author := e.user("john", "it", true, true)

	// новое обращение: письма получают администраторы, которым их можно слать; автору не отправляется, даже если он в списке
	e.svc.TicketNew(bg, []auth.User{staff, quiet, unverified, author}, author, 7, "Не открывается сайт", "Сайт\nотдаёт   502", false)
	e.svc.Drain(bg)
	msgs := e.sender.messages()
	if len(msgs) != 1 || msgs[0].To != "boss@example.com" {
		t.Fatalf("%+v", msgs)
	}
	m := msgs[0]
	if !strings.Contains(m.Subject, "№7") || !strings.Contains(m.Subject, "Не открывается сайт") || !strings.Contains(m.Text, "john") || !strings.Contains(m.Text, "/support/7") {
		t.Fatalf("%+v", m)
	}

	// дополнение в тикете — отдельный вид письма
	e.svc.TicketNew(bg, []auth.User{staff}, author, 7, "Не открывается сайт", "ещё вопрос", true)
	e.svc.Drain(bg)
	if msgs = e.sender.messages(); len(msgs) != 2 || !strings.Contains(msgs[1].Subject, "Ответ в обращении") {
		t.Fatalf("%+v", msgs)
	}

	// ответ поддержки: владелец получает письмо на своём языке; тем, кто не ждёт писем, не отправляется
	e.svc.TicketReply(bg, author, 7, "Не открывается сайт", "Посмотрите журнал ошибок")
	e.svc.TicketReply(bg, quiet, 8, "Другое", "текст")
	e.svc.TicketReply(bg, unverified, 9, "Другое", "текст")
	e.svc.Drain(bg)
	msgs = e.sender.messages()
	if len(msgs) != 3 || msgs[2].To != "john@example.com" || !strings.Contains(msgs[2].Subject, "Il supporto ha risposto") {
		t.Fatalf("%+v", msgs)
	}
	if !strings.Contains(msgs[2].Text, "Посмотрите журнал ошибок") || !strings.Contains(msgs[2].Text, "/support/7") {
		t.Fatalf("%q", msgs[2].Text)
	}
}

package httpapi_test

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"vladhost/internal/config"
	"vladhost/internal/httpapi"
	"vladhost/internal/mailer"
	"vladhost/internal/notify"
)

// mailbox — «почтовый ящик» тестов: запоминает письма, которые ушли бы по SMTP.
type mailbox struct {
	mu   sync.Mutex
	msgs []mailer.Message
}

func (m *mailbox) Send(_ context.Context, msg mailer.Message) error {
	m.mu.Lock()
	m.msgs = append(m.msgs, msg)
	m.mu.Unlock()
	return nil
}

func (m *mailbox) all() []mailer.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mailer.Message(nil), m.msgs...)
}

// withMail включает почту в сервере окружения; письма доставляются сразу, когда тест вызывает flush.
func (e *env) withMail() (*mailbox, func()) {
	e.t.Helper()
	box := &mailbox{}
	svc := notify.New(e.db, box, "https://app.example.test")
	e.r = httpapi.New(e.svc, e.sites, config.Config{
		JWTSecret: []byte(strings.Repeat("s", 32)), AuthPerMinute: 10000, AuthBurst: 10000,
	}, httpapi.WithMail(svc))
	return box, func() { svc.Drain(context.Background()) }
}

var tokenRe = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

func tokenFrom(t *testing.T, m mailer.Message) string {
	t.Helper()
	g := tokenRe.FindStringSubmatch(m.Text)
	if g == nil {
		t.Fatalf("в письме нет ссылки: %q", m.Text)
	}
	return g[1]
}

func (e *env) register(lang, invite, name string) (string, int) {
	e.t.Helper()
	w := e.doLang(lang, "POST", "/api/auth/register", map[string]string{
		"invite": invite, "email": name + "@example.com", "username": name, "password": "password123",
	}, "")
	if w.Code != 200 {
		e.t.Fatalf("регистрация %s: %d %s", name, w.Code, w.Body)
	}
	s := decode[sessionBody](e.t, w)
	return s.AccessToken, int(s.User.ID)
}

// ageTokens делает ссылки «старыми»: иначе пауза между письмами не даст запросить следующее.
func (e *env) ageTokens(kind string) {
	e.t.Helper()
	if err := e.db.Exec("UPDATE mail_tokens SET created_at = created_at - interval '2 minutes' WHERE kind = ?", kind).Error; err != nil {
		e.t.Fatal(err)
	}
}

func TestMailDisabledWithoutConfig(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	for _, tc := range []struct {
		method, path string
		body         any
		token        string
	}{
		{"POST", "/api/auth/forgot", map[string]string{"email": "john@example.com"}, ""},
		{"POST", "/api/me/email/verify", nil, tok},
	} {
		w := e.do(tc.method, tc.path, tc.body, tc.token)
		if w.Code != 409 || decode[errBody](t, w).Error.Code != "mail_unavailable" {
			t.Errorf("%s: %d %s", tc.path, w.Code, w.Body)
		}
	}
	if body := e.do("GET", "/api/me", nil, tok).Body.String(); !strings.Contains(body, `"mail_enabled":false`) {
		t.Fatalf("/me: %s", body)
	}
}

func TestRegistrationSendsVerificationInTheUsersLanguage(t *testing.T) {
	e := newEnv(t)
	box, flush := e.withMail()
	adm, _ := e.admin()
	tok, _ := e.register("it", e.invite(adm), "carla")
	flush()
	msgs := box.all()
	if len(msgs) != 1 || msgs[0].To != "carla@example.com" || !strings.Contains(msgs[0].Subject, "Conferma") ||
		!strings.Contains(msgs[0].Text, "https://app.example.test/verify-email?token=") {
		t.Fatalf("%+v", msgs)
	}
	me := decode[struct {
		User struct {
			Lang            string  `json:"lang"`
			NotifyEmail     bool    `json:"notify_email"`
			EmailVerifiedAt *string `json:"email_verified_at"`
		} `json:"user"`
		MailEnabled bool `json:"mail_enabled"`
	}](t, e.do("GET", "/api/me", nil, tok))
	if me.User.Lang != "it" || !me.User.NotifyEmail || me.User.EmailVerifiedAt != nil || !me.MailEnabled {
		t.Fatalf("%+v", me)
	}

	// Ссылка подтверждает адрес один раз.
	token := tokenFrom(t, msgs[0])
	if w := e.do("POST", "/api/auth/verify-email", map[string]string{"token": token}, ""); w.Code != 204 {
		t.Fatalf("подтверждение: %d %s", w.Code, w.Body)
	}
	if got := decode[struct {
		User struct {
			EmailVerifiedAt *string `json:"email_verified_at"`
		} `json:"user"`
	}](t, e.do("GET", "/api/me", nil, tok)); got.User.EmailVerifiedAt == nil {
		t.Fatal("адрес не подтверждён")
	}
	if w := e.do("POST", "/api/auth/verify-email", map[string]string{"token": token}, ""); w.Code != 400 || decode[errBody](t, w).Error.Code != "invalid_mail_token" {
		t.Fatalf("повторное использование: %d %s", w.Code, w.Body)
	}
	// Ссылка хранится хешем: сырого токена в БД нет.
	var n int64
	e.db.Raw("SELECT count(*) FROM mail_tokens WHERE token_hash = ?", token).Scan(&n)
	if n != 0 {
		t.Fatal("сырой токен лежит в БД")
	}
	// Подтверждённому письмо не нужно.
	if w := e.do("POST", "/api/me/email/verify", nil, tok); w.Code != 409 || decode[errBody](t, w).Error.Code != "email_verified" {
		t.Fatalf("уже подтверждён: %d", w.Code)
	}
}

func TestVerificationLinkValidation(t *testing.T) {
	e := newEnv(t)
	e.withMail()
	for _, token := range []string{"", "x", "nope-not-a-token", strings.Repeat("a", 200)} {
		w := e.do("POST", "/api/auth/verify-email", map[string]string{"token": token}, "")
		if token == "" && w.Code != 400 {
			t.Errorf("пустой: %d", w.Code)
		}
		if token != "" && (w.Code != 400 || decode[errBody](t, w).Error.Code != "invalid_mail_token") {
			t.Errorf("%q: %d %s", token, w.Code, w.Body)
		}
	}
	if w := e.do("POST", "/api/auth/verify-email", "не объект", ""); w.Code != 400 {
		t.Errorf("не объект: %d", w.Code)
	}
}

func TestResendVerificationAndOnlyLatestLinkWorks(t *testing.T) {
	e := newEnv(t)
	box, flush := e.withMail()
	adm, _ := e.admin()
	tok, _ := e.register("ru", e.invite(adm), "john")
	flush()
	first := tokenFrom(t, box.all()[0])

	// Сразу повторно — рано; после паузы — можно, и старая ссылка перестаёт действовать.
	if w := e.do("POST", "/api/me/email/verify", nil, tok); w.Code != 429 || decode[errBody](t, w).Error.Code != "mail_cooldown" {
		t.Fatalf("пауза: %d %s", w.Code, w.Body)
	}
	e.ageTokens("verify")
	if w := e.do("POST", "/api/me/email/verify", nil, tok); w.Code != 202 {
		t.Fatalf("повтор: %d %s", w.Code, w.Body)
	}
	flush()
	if len(box.all()) != 2 {
		t.Fatalf("писем %d", len(box.all()))
	}
	second := tokenFrom(t, box.all()[1])
	if w := e.do("POST", "/api/auth/verify-email", map[string]string{"token": first}, ""); w.Code != 400 {
		t.Fatalf("старая ссылка должна не работать: %d", w.Code)
	}
	if w := e.do("POST", "/api/auth/verify-email", map[string]string{"token": second}, ""); w.Code != 204 {
		t.Fatalf("новая ссылка: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/me/email/verify", nil, ""); w.Code != 401 {
		t.Fatalf("без входа: %d", w.Code)
	}
}

func TestVerificationLinkExpires(t *testing.T) {
	e := newEnv(t)
	box, flush := e.withMail()
	adm, _ := e.admin()
	e.register("ru", e.invite(adm), "john")
	flush()
	token := tokenFrom(t, box.all()[0])
	e.db.Exec("UPDATE mail_tokens SET expires_at = now() - interval '1 second'")
	if w := e.do("POST", "/api/auth/verify-email", map[string]string{"token": token}, ""); w.Code != 400 {
		t.Fatalf("просроченная ссылка: %d", w.Code)
	}
}

func TestForgotPasswordDoesNotRevealWhetherTheAddressExists(t *testing.T) {
	e := newEnv(t)
	box, flush := e.withMail()
	adm, _ := e.admin()
	e.user(adm, "john")
	flush()
	before := len(box.all())

	known := e.do("POST", "/api/auth/forgot", map[string]string{"email": "john@example.com"}, "")
	unknown := e.do("POST", "/api/auth/forgot", map[string]string{"email": "nobody@example.com"}, "")
	garbage := e.do("POST", "/api/auth/forgot", map[string]string{"email": "not an email"}, "")
	if known.Code != 202 || unknown.Code != 202 || garbage.Code != 202 || known.Body.String() != unknown.Body.String() || known.Body.String() != garbage.Body.String() {
		t.Fatalf("ответы различаются: %d/%d/%d %q %q", known.Code, unknown.Code, garbage.Code, known.Body, unknown.Body)
	}
	flush()
	msgs := box.all()[before:]
	if len(msgs) != 1 || msgs[0].To != "john@example.com" || !strings.Contains(msgs[0].Text, "/reset-password?token=") {
		t.Fatalf("письмо только существующему адресу: %+v", msgs)
	}
	if w := e.do("POST", "/api/auth/forgot", "не объект", ""); w.Code != 400 {
		t.Fatalf("не объект: %d", w.Code)
	}
	// Адрес без учёта регистра.
	e.ageTokens("reset")
	if w := e.do("POST", "/api/auth/forgot", map[string]string{"email": "JOHN@Example.com"}, ""); w.Code != 202 {
		t.Fatal(w.Code)
	}
	flush()
	if got := len(box.all()) - before; got != 2 {
		t.Fatalf("регистр адреса не должен мешать: писем %d", got)
	}
}

func TestForgotPasswordIsRateLimitedPerAccount(t *testing.T) {
	e := newEnv(t)
	box, flush := e.withMail()
	adm, _ := e.admin()
	e.user(adm, "john")
	flush()
	base := len(box.all())
	ask := func() {
		if w := e.do("POST", "/api/auth/forgot", map[string]string{"email": "john@example.com"}, ""); w.Code != 202 {
			t.Fatalf("ответ всегда 202: %d", w.Code)
		}
		flush()
	}
	ask()
	ask() // в ту же минуту: письма нет, но ответ такой же
	if got := len(box.all()) - base; got != 1 {
		t.Fatalf("пауза между письмами: писем %d", got)
	}
	// Не больше пяти в час, даже если паузу выдерживать.
	for i := 0; i < 8; i++ {
		e.ageTokens("reset")
		ask()
	}
	if got := len(box.all()) - base; got != 5 {
		t.Fatalf("за час должно уйти не больше 5 писем, ушло %d", got)
	}
}

func TestForgotPasswordIPLimit(t *testing.T) {
	e := newEnvLimit(t, 20, 3)
	e.withMail()
	e.r = httpapi.New(e.svc, e.sites, config.Config{JWTSecret: []byte(strings.Repeat("s", 32)), AuthPerMinute: 20, AuthBurst: 3},
		httpapi.WithMail(notify.New(e.db, &mailbox{}, "https://x")))
	got429 := false
	for i := 0; i < 10; i++ {
		if e.do("POST", "/api/auth/forgot", map[string]string{"email": "x@example.com"}, "").Code == http.StatusTooManyRequests {
			got429 = true
		}
	}
	if !got429 {
		t.Fatal("перебор запросов с одного IP должен упираться в лимит")
	}
}

func TestResetPasswordFlow(t *testing.T) {
	e := newEnv(t)
	box, flush := e.withMail()
	adm, _ := e.admin()
	e.user(adm, "john")
	w := e.do("POST", "/api/auth/login", map[string]string{"login": "john", "password": "password123"}, "")
	cookie := refreshCookie(t, w)
	flush()
	base := len(box.all())

	e.doLang("it", "POST", "/api/auth/forgot", map[string]string{"email": "john@example.com"}, "")
	flush()
	m := box.all()[base]
	if !strings.Contains(m.Subject, "Reimpostazione") {
		t.Fatalf("письмо на языке запроса: %q", m.Subject)
	}
	token := tokenFrom(t, m)

	// Плохие входные данные не тратят ссылку.
	for _, tc := range []struct {
		body map[string]string
		code int
	}{
		{map[string]string{"token": token, "password": "short"}, 422},
		{map[string]string{"token": "wrong", "password": "brand-new-pass"}, 400},
		{map[string]string{"token": "", "password": "brand-new-pass"}, 400},
	} {
		if w := e.do("POST", "/api/auth/reset", tc.body, ""); w.Code != tc.code {
			t.Errorf("%v → %d %s", tc.body, w.Code, w.Body)
		}
	}
	if w := e.do("POST", "/api/auth/reset", map[string]string{"token": token, "password": "short"}, ""); decode[errBody](t, w).Error.Field != "password" {
		t.Errorf("ошибка пароля привязана к полю password: %s", w.Body)
	}

	if w := e.do("POST", "/api/auth/reset", map[string]string{"token": token, "password": "brand-new-pass"}, ""); w.Code != 204 {
		t.Fatalf("сброс: %d %s", w.Code, w.Body)
	}
	// Старый пароль не подходит, новый подходит; прежние сессии закрыты; ссылка одноразовая.
	if w := e.do("POST", "/api/auth/login", map[string]string{"login": "john", "password": "password123"}, ""); w.Code != 401 {
		t.Fatalf("старый пароль: %d", w.Code)
	}
	if w := e.do("POST", "/api/auth/login", map[string]string{"login": "john", "password": "brand-new-pass"}, ""); w.Code != 200 {
		t.Fatalf("новый пароль: %d", w.Code)
	}
	if w := e.do("POST", "/api/auth/refresh", nil, "", cookie); w.Code != 401 {
		t.Fatalf("прежняя сессия должна быть закрыта: %d", w.Code)
	}
	if w := e.do("POST", "/api/auth/reset", map[string]string{"token": token, "password": "another-pass-1"}, ""); w.Code != 400 {
		t.Fatalf("повторное использование ссылки: %d", w.Code)
	}
	// Владельцу уходит уведомление о смене пароля (на языке запроса).
	flush()
	last := box.all()[len(box.all())-1]
	if last.To != "john@example.com" || !strings.Contains(last.Subject, "Пароль от аккаунта изменён") {
		t.Fatalf("уведомление о смене пароля: %+v", last)
	}
}

func TestResetLinkExpiresAndNewRequestKillsTheOldOne(t *testing.T) {
	e := newEnv(t)
	box, flush := e.withMail()
	adm, _ := e.admin()
	e.user(adm, "john")
	flush()
	base := len(box.all())
	e.do("POST", "/api/auth/forgot", map[string]string{"email": "john@example.com"}, "")
	e.ageTokens("reset")
	e.do("POST", "/api/auth/forgot", map[string]string{"email": "john@example.com"}, "")
	flush()
	first, second := tokenFrom(t, box.all()[base]), tokenFrom(t, box.all()[base+1])
	if w := e.do("POST", "/api/auth/reset", map[string]string{"token": first, "password": "brand-new-pass"}, ""); w.Code != 400 {
		t.Fatalf("прежняя ссылка после нового запроса: %d", w.Code)
	}
	e.db.Exec("UPDATE mail_tokens SET expires_at = now() - interval '1 second' WHERE kind = 'reset'")
	if w := e.do("POST", "/api/auth/reset", map[string]string{"token": second, "password": "brand-new-pass"}, ""); w.Code != 400 {
		t.Fatalf("просроченная ссылка: %d", w.Code)
	}
}

func TestResetConfirmsTheAddress(t *testing.T) {
	e := newEnv(t)
	box, flush := e.withMail()
	adm, _ := e.admin()
	e.register("ru", e.invite(adm), "john")
	flush()
	base := len(box.all())
	e.do("POST", "/api/auth/forgot", map[string]string{"email": "john@example.com"}, "")
	flush()
	if w := e.do("POST", "/api/auth/reset", map[string]string{"token": tokenFrom(t, box.all()[base]), "password": "brand-new-pass"}, ""); w.Code != 204 {
		t.Fatal(w.Code)
	}
	var verified bool
	e.db.Raw("SELECT email_verified_at IS NOT NULL FROM users WHERE username = 'john'").Scan(&verified)
	if !verified {
		t.Fatal("письмо со ссылкой дошло до владельца: адрес подтверждён")
	}
}

func TestPasswordChangeSendsNotice(t *testing.T) {
	e := newEnv(t)
	box, flush := e.withMail()
	adm, _ := e.admin()
	tok, _ := e.register("ru", e.invite(adm), "john")
	flush()
	base := len(box.all())
	w := e.do("POST", "/api/me/password", map[string]string{"current_password": "password123", "new_password": "brand-new-pass"}, tok)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	flush()
	if got := box.all()[base:]; len(got) != 1 || !strings.Contains(got[0].Subject, "Пароль") {
		t.Fatalf("%+v", got)
	}
}

func TestUpdatePreferences(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	type me struct {
		User struct {
			Lang        string `json:"lang"`
			NotifyEmail bool   `json:"notify_email"`
		} `json:"user"`
	}
	got := decode[me](t, e.do("PATCH", "/api/me", map[string]any{"lang": "it", "notify_email": false}, tok))
	if got.User.Lang != "it" || got.User.NotifyEmail {
		t.Fatalf("%+v", got)
	}
	if got := decode[me](t, e.do("GET", "/api/me", nil, tok)); got.User.Lang != "it" || got.User.NotifyEmail {
		t.Fatalf("настройки не сохранились: %+v", got)
	}
	// По одному полю.
	if got := decode[me](t, e.do("PATCH", "/api/me", map[string]any{"notify_email": true}, tok)); got.User.Lang != "it" || !got.User.NotifyEmail {
		t.Fatalf("%+v", got)
	}
	for _, body := range []any{map[string]any{"lang": "fr"}, map[string]any{"lang": ""}, "не объект"} {
		if w := e.do("PATCH", "/api/me", body, tok); w.Code != 400 {
			t.Errorf("%v → %d", body, w.Code)
		}
	}
	if w := e.do("PATCH", "/api/me", map[string]any{"lang": "ru"}, ""); w.Code != 401 {
		t.Errorf("без входа: %d", w.Code)
	}
}

func TestAdminsCreatedByOperatorAreVerified(t *testing.T) {
	e := newEnv(t)
	e.withMail()
	adm, _ := e.admin()
	me := decode[struct {
		User struct {
			EmailVerifiedAt *string `json:"email_verified_at"`
			NotifyEmail     bool    `json:"notify_email"`
			Lang            string  `json:"lang"`
		} `json:"user"`
	}](t, e.do("GET", "/api/me", nil, adm))
	if me.User.EmailVerifiedAt == nil || !me.User.NotifyEmail || me.User.Lang != "ru" {
		t.Fatalf("%+v", me)
	}
}

func TestMailTokensAreDeletedWithTheUser(t *testing.T) {
	e := newEnv(t)
	_, flush := e.withMail()
	adm, _ := e.admin()
	e.register("ru", e.invite(adm), "john")
	flush()
	var n int64
	e.db.Raw("SELECT count(*) FROM mail_tokens").Scan(&n)
	if n != 1 {
		t.Fatalf("токенов %d", n)
	}
	e.db.Exec("DELETE FROM users WHERE username = 'john'")
	e.db.Raw("SELECT count(*) FROM mail_tokens").Scan(&n)
	if n != 0 {
		t.Fatal("ссылки удалённого пользователя остались")
	}
}

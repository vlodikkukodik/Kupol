package accounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"kupol/internal/config"
	"kupol/internal/passwords"
	"kupol/internal/ratelimit"
	"kupol/internal/testutil"
)

// Все тесты работают с настоящим PostgreSQL и настоящим argon2id (с облегчёнными параметрами,
// чтобы тесты шли быстро), а время подменяется управляемыми часами.

var fastParams = passwords.Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type env struct {
	t       *testing.T
	svc     *Service
	db      *gorm.DB
	clock   *testClock
	limiter *ratelimit.Limiter
	ipSeq   int
}

func newEnv(t *testing.T) *env { return newEnvWith(t, config.DefaultLimits()) }

func newEnvWith(t *testing.T, limits config.Limits) *env {
	t.Helper()
	clock := &testClock{t: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)}
	db := testutil.NewMigratedDB(t)
	hasher, err := passwords.NewHasher(fastParams, 4)
	if err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.New(clock.Now)
	svc, err := NewService(Options{DB: db, Hasher: hasher, Limiter: limiter, Limits: limits, Log: testutil.Logger(), Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, svc: svc, db: db, clock: clock, limiter: limiter}
}

// freshIP выдаёт уникальный IP, чтобы регистрации в одном тесте не упирались в лимит по IP.
func (e *env) freshIP() ClientInfo {
	e.ipSeq++
	return ClientInfo{IP: fmt.Sprintf("203.0.113.%d", e.ipSeq), UserAgent: "test-agent"}
}

func answerFor(t *testing.T, c *Captcha) string {
	t.Helper()
	for _, q := range questions {
		if q.Text == c.Question {
			return q.Answers[0]
		}
	}
	t.Fatalf("вопрос %q не найден в банке", c.Question)
	return ""
}

func (e *env) register(login, password string) *RegisterResult {
	e.t.Helper()
	res, err := e.registerWith(login, password, e.freshIP())
	if err != nil {
		e.t.Fatalf("регистрация %q: %v", login, err)
	}
	return res
}

func (e *env) registerWith(login, password string, ci ClientInfo) (*RegisterResult, error) {
	e.t.Helper()
	c, err := e.svc.NewCaptcha(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return e.svc.Register(context.Background(), RegisterInput{
		Login: login, Password: password, CaptchaID: c.ID, CaptchaAnswer: answerFor(e.t, c),
	}, ci)
}

func (e *env) count(query string, args ...any) int64 {
	e.t.Helper()
	var n int64
	if err := e.db.Raw(query, args...).Scan(&n).Error; err != nil {
		e.t.Fatal(err)
	}
	return n
}

func mustRateLimited(t *testing.T, err error) *RateLimitedError {
	t.Helper()
	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("ожидалась RateLimitedError, получено %v", err)
	}
	return rl
}

func mustValidation(t *testing.T, err error) map[string]string {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("ожидалась ValidationError, получено %v", err)
	}
	return ve.Fields
}

var ctx = context.Background()

// ---------------------------------------------------------------- регистрация

func TestRegisterCreatesAccountAndSession(t *testing.T) {
	e := newEnv(t)
	ci := ClientInfo{IP: "203.0.113.7", UserAgent: "Mozilla/5.0 test"}
	c, _ := e.svc.NewCaptcha(ctx)
	res, err := e.svc.Register(ctx, RegisterInput{Login: "Куратор7", Password: "секретный пароль", CaptchaID: c.ID, CaptchaAnswer: answerFor(t, c)}, ci)
	if err != nil {
		t.Fatal(err)
	}

	if res.User.Login != "Куратор7" || res.User.Level != LevelVisitor || res.User.Directorate {
		t.Fatalf("пользователь: %+v", res.User)
	}
	if res.User.LevelName() != "Посетитель" {
		t.Errorf("звание: %q", res.User.LevelName())
	}
	if !backupRe.MatchString(res.BackupCode) {
		t.Errorf("резервный код: %q", res.BackupCode)
	}
	if want := e.clock.Now().Add(SessionSlidingTTL); !res.ExpiresAt.Equal(want) {
		t.Errorf("срок сессии %s, ожидался %s", res.ExpiresAt, want)
	}

	// в БД нет открытых секретов
	var u User
	if err := e.db.Where("login = ?", "куратор7").Take(&u).Error; err != nil { // без учёта регистра
		t.Fatal(err)
	}
	for _, secret := range []string{"секретный пароль", res.BackupCode, res.Token} {
		if strings.Contains(u.PasswordHash, secret) || strings.Contains(u.BackupCodeHash, secret) {
			t.Errorf("секрет %q попал в хеш открытым текстом", secret)
		}
	}
	if !strings.HasPrefix(u.PasswordHash, "$argon2id$") || !strings.HasPrefix(u.BackupCodeHash, "$argon2id$") {
		t.Errorf("хеши не argon2id: %q %q", u.PasswordHash, u.BackupCodeHash)
	}
	if u.LastLoginAt == nil || !u.PasswordChangedAt.Equal(e.clock.Now()) {
		t.Errorf("отметки времени: %+v", u)
	}

	// токен в БД хранится только хешем; по токену находится пользователь
	if n := e.count("SELECT count(*) FROM sessions WHERE token_hash = ?", []byte(res.Token)); n != 0 {
		t.Error("токен лежит в БД открытым текстом")
	}
	a, err := e.svc.Authenticate(ctx, res.Token)
	if err != nil || a.User.ID != u.ID {
		t.Fatalf("Authenticate: %+v, %v", a, err)
	}
	if a.Session.UserAgent != "Mozilla/5.0 test" || a.Session.IP == nil || *a.Session.IP != "203.0.113.7" {
		t.Errorf("сведения о клиенте: %+v", a.Session)
	}
}

func TestRegisterValidationCollectsAllFieldsAndKeepsCaptcha(t *testing.T) {
	e := newEnv(t)
	c, _ := e.svc.NewCaptcha(ctx)
	_, err := e.svc.Register(ctx, RegisterInput{Login: "a", Password: "short", CaptchaID: c.ID, CaptchaAnswer: ""}, e.freshIP())
	f := mustValidation(t, err)
	for _, field := range []string{"login", "password", "captcha_answer"} {
		if f[field] == "" {
			t.Errorf("нет ошибки для поля %s: %v", field, f)
		}
	}
	if n := e.count("SELECT count(*) FROM users"); n != 0 {
		t.Errorf("создано пользователей: %d", n)
	}
	// ошибка формы не сжигает вопрос анкеты: пользователь исправит поля и ответит на тот же вопрос
	if n := e.count("SELECT count(*) FROM captcha_challenges WHERE id = ?", c.ID); n != 1 {
		t.Error("валидация не должна расходовать вопрос анкеты")
	}
	// пароль не должен совпадать с логином
	_, err = e.svc.Register(ctx, RegisterInput{Login: "Vladislav", Password: "vladislav", CaptchaID: c.ID, CaptchaAnswer: answerFor(t, c)}, e.freshIP())
	if f := mustValidation(t, err); !strings.Contains(f["password"], "совпадать") {
		t.Errorf("пароль = логин: %v", f)
	}
}

func TestCaptchaIsSingleUse(t *testing.T) {
	e := newEnv(t)
	c, _ := e.svc.NewCaptcha(ctx)
	ans := answerFor(t, c)

	// неверный ответ гасит вопрос: перебирать ответы на одном вопросе нельзя
	_, err := e.svc.Register(ctx, RegisterInput{Login: "first", Password: "password-1", CaptchaID: c.ID, CaptchaAnswer: "не знаю"}, e.freshIP())
	if !errors.Is(err, ErrCaptcha) {
		t.Fatalf("неверный ответ: %v", err)
	}
	_, err = e.svc.Register(ctx, RegisterInput{Login: "first", Password: "password-1", CaptchaID: c.ID, CaptchaAnswer: ans}, e.freshIP())
	if !errors.Is(err, ErrCaptcha) {
		t.Fatalf("после неверного ответа тот же вопрос с верным ответом должен быть отвергнут: %v", err)
	}
	if n := e.count("SELECT count(*) FROM users"); n != 0 {
		t.Fatal("аккаунт создан по погашенному вопросу")
	}

	// успешное использование тоже гасит
	c2, _ := e.svc.NewCaptcha(ctx)
	in := RegisterInput{Login: "second", Password: "password-2", CaptchaID: c2.ID, CaptchaAnswer: answerFor(t, c2)}
	if _, err := e.svc.Register(ctx, in, e.freshIP()); err != nil {
		t.Fatal(err)
	}
	in.Login = "third"
	if _, err := e.svc.Register(ctx, in, e.freshIP()); !errors.Is(err, ErrCaptcha) {
		t.Fatalf("повторное использование вопроса: %v", err)
	}
}

func TestCaptchaExpiresAndRejectsGarbageIDs(t *testing.T) {
	e := newEnv(t)
	c, _ := e.svc.NewCaptcha(ctx)
	e.clock.Advance(captchaTTL + time.Second)
	_, err := e.svc.Register(ctx, RegisterInput{Login: "late", Password: "password-1", CaptchaID: c.ID, CaptchaAnswer: answerFor(t, c)}, e.freshIP())
	if !errors.Is(err, ErrCaptcha) {
		t.Fatalf("просроченный вопрос: %v", err)
	}
	for _, id := range []string{"", "не-uuid", "00000000-0000-0000-0000-000000000000", "' OR 1=1 --", strings.Repeat("a", 1000)} {
		_, err := e.svc.Register(ctx, RegisterInput{Login: "ghost", Password: "password-1", CaptchaID: id, CaptchaAnswer: "1974"}, e.freshIP())
		if !errors.Is(err, ErrCaptcha) {
			t.Errorf("id %q: %v", id, err)
		}
	}
}

func TestRegisterLoginTakenIsCaseInsensitive(t *testing.T) {
	e := newEnv(t)
	e.register("Vladislav", "password-1")
	for _, dup := range []string{"Vladislav", "vladislav", "VLADISLAV"} {
		if _, err := e.registerWith(dup, "password-2", e.freshIP()); !errors.Is(err, ErrLoginTaken) {
			t.Errorf("%q: %v", dup, err)
		}
	}
	e.register("Стажёр7", "password-1")
	if _, err := e.registerWith("стажёр7", "password-2", e.freshIP()); !errors.Is(err, ErrLoginTaken) {
		t.Errorf("кириллица без учёта регистра: %v", err)
	}
	if n := e.count("SELECT count(*) FROM users"); n != 2 {
		t.Errorf("пользователей %d, ожидалось 2", n)
	}
	// неудачная регистрация не оставляет «висячих» сессий
	if n := e.count("SELECT count(*) FROM sessions"); n != 2 {
		t.Errorf("сессий %d, ожидалось 2", n)
	}
}

func TestRegisterRateLimitPerIP(t *testing.T) {
	e := newEnv(t) // по умолчанию 3 в час
	ci := ClientInfo{IP: "198.51.100.10"}
	for i := 0; i < 3; i++ {
		if _, err := e.registerWith(fmt.Sprintf("user%d", i), "password-1", ci); err != nil {
			t.Fatalf("регистрация %d: %v", i, err)
		}
	}
	_, err := e.registerWith("user3", "password-1", ci)
	rl := mustRateLimited(t, err)
	if rl.RetryAfter < 59*time.Minute || rl.RetryAfter > time.Hour {
		t.Errorf("RetryAfter = %s", rl.RetryAfter)
	}
	if n := e.count("SELECT count(*) FROM users"); n != 3 {
		t.Errorf("пользователей %d", n)
	}

	// другой IP не затронут
	if _, err := e.registerWith("otherip", "password-1", ClientInfo{IP: "198.51.100.11"}); err != nil {
		t.Errorf("другой IP: %v", err)
	}
	// через час лимит снят
	e.clock.Advance(time.Hour + time.Second)
	if _, err := e.registerWith("later", "password-1", ci); err != nil {
		t.Errorf("после окна: %v", err)
	}
}

func TestRegisterRateLimitNotConsumedByMistakes(t *testing.T) {
	e := newEnv(t)
	ci := ClientInfo{IP: "198.51.100.20"}
	for i := 0; i < 10; i++ {
		// опечатки в форме и неверные ответы анкеты не должны расходовать лимит регистраций
		c, _ := e.svc.NewCaptcha(ctx)
		_, _ = e.svc.Register(ctx, RegisterInput{Login: "x", Password: "y", CaptchaID: c.ID, CaptchaAnswer: "z"}, ci)
		_, _ = e.svc.Register(ctx, RegisterInput{Login: "validlogin", Password: "password-1", CaptchaID: c.ID, CaptchaAnswer: "неверно"}, ci)
	}
	for i := 0; i < 3; i++ {
		if _, err := e.registerWith(fmt.Sprintf("okuser%d", i), "password-1", ci); err != nil {
			t.Fatalf("настоящая регистрация %d после ошибок: %v", i, err)
		}
	}
}

func TestConcurrentRegistrationOfSameLogin(t *testing.T) {
	e := newEnv(t)
	const n = 8
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		ci := e.freshIP()
		go func() {
			defer wg.Done()
			_, err := e.registerWith("Гонка", "password-1", ci)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	var ok, taken int
	for err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrLoginTaken):
			taken++
		default:
			t.Errorf("неожиданная ошибка: %v", err)
		}
	}
	if ok != 1 || taken != n-1 {
		t.Fatalf("успешных %d, «логин занят» %d; ожидалось 1 и %d", ok, taken, n-1)
	}
	if c := e.count("SELECT count(*) FROM users"); c != 1 {
		t.Fatalf("пользователей %d", c)
	}
}

func TestSessionRecordsIPv6AndTruncatesUserAgent(t *testing.T) {
	e := newEnv(t)
	res, err := e.registerWith("ipsix", "password-1", ClientInfo{IP: "2001:db8::42", UserAgent: strings.Repeat("я", 400)})
	if err != nil {
		t.Fatal(err)
	}
	a, err := e.svc.Authenticate(ctx, res.Token)
	if err != nil {
		t.Fatal(err)
	}
	if a.Session.IP == nil || *a.Session.IP != "2001:db8::42" {
		t.Errorf("ip: %v", a.Session.IP)
	}
	if got := len([]rune(a.Session.UserAgent)); got != maxUserAgentLen {
		t.Errorf("длина User-Agent %d, ожидалось %d", got, maxUserAgentLen)
	}

	res, err = e.registerWith("noip", "password-1", ClientInfo{IP: "не-ip"})
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := e.svc.Authenticate(ctx, res.Token); a == nil || a.Session.IP != nil {
		t.Errorf("некорректный IP должен сохраняться как NULL: %+v", a)
	}
}

// ---------------------------------------------------------------- вход

func TestLoginSuccessAndCaseInsensitivity(t *testing.T) {
	e := newEnv(t)
	reg := e.register("Vladislav", "верный пароль")
	e.clock.Advance(time.Hour)

	for _, login := range []string{"Vladislav", "vladislav", "VLADISLAV", "  vladislav  "} {
		res, err := e.svc.Login(ctx, login, "верный пароль", e.freshIP())
		if err != nil {
			t.Fatalf("вход %q: %v", login, err)
		}
		if res.User.ID != reg.User.ID || res.Token == reg.Token {
			t.Errorf("вход %q: другой пользователь или повторно выдан тот же токен", login)
		}
		if !res.User.LastLoginAt.Equal(e.clock.Now()) {
			t.Errorf("last_login_at не обновлён: %v", res.User.LastLoginAt)
		}
	}
	// у одного пользователя может быть несколько сессий (разные устройства)
	if n := e.count("SELECT count(*) FROM sessions WHERE user_id = ?", reg.User.ID); n != 5 {
		t.Errorf("сессий %d, ожидалось 5", n)
	}
}

func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	e := newEnv(t)
	e.register("existing", "правильный пароль")
	ci := e.freshIP()

	_, errWrongPass := e.svc.Login(ctx, "existing", "неправильный", ci)
	_, errNoUser := e.svc.Login(ctx, "no-such-user", "неправильный", ci)
	if !errors.Is(errWrongPass, ErrInvalidCredentials) || !errors.Is(errNoUser, ErrInvalidCredentials) {
		t.Fatalf("ошибки: %v / %v", errWrongPass, errNoUser)
	}
	if errWrongPass.Error() != errNoUser.Error() {
		t.Error("тексты ошибок различаются — по ним можно перебирать логины")
	}
}

func TestLoginRequiresBothFields(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.Login(ctx, "  ", "", e.freshIP())
	f := mustValidation(t, err)
	if f["login"] == "" || f["password"] == "" {
		t.Errorf("поля: %v", f)
	}
}

func TestLoginLockoutPerIPAndLogin(t *testing.T) {
	e := newEnv(t) // 5 неудач за 15 минут на пару IP+логин
	e.register("target", "правильный пароль")
	ci := ClientInfo{IP: "198.51.100.50"}

	for i := 0; i < 5; i++ {
		if _, err := e.svc.Login(ctx, "target", "неверно", ci); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("попытка %d: %v", i+1, err)
		}
	}
	// 6-я попытка блокируется, даже с верным паролем
	_, err := e.svc.Login(ctx, "target", "правильный пароль", ci)
	rl := mustRateLimited(t, err)
	if rl.RetryAfter <= 14*time.Minute || rl.RetryAfter > 15*time.Minute {
		t.Errorf("RetryAfter = %s", rl.RetryAfter)
	}
	// заблокированные попытки не продлевают блокировку
	e.clock.Advance(10 * time.Minute)
	if _, err := e.svc.Login(ctx, "target", "правильный пароль", ci); err == nil {
		t.Fatal("через 10 минут блокировка ещё действует")
	}
	e.clock.Advance(5*time.Minute + time.Second)
	if _, err := e.svc.Login(ctx, "target", "правильный пароль", ci); err != nil {
		t.Fatalf("после окна вход должен работать: %v", err)
	}
}

func TestLoginLockoutDoesNotAffectOtherIPsOrLogins(t *testing.T) {
	e := newEnv(t)
	e.register("target", "правильный пароль")
	e.register("bystander", "другой пароль")
	attacker := ClientInfo{IP: "198.51.100.60"}
	for i := 0; i < 5; i++ {
		_, _ = e.svc.Login(ctx, "target", "неверно", attacker)
	}
	if _, err := e.svc.Login(ctx, "target", "правильный пароль", attacker); mustRateLimited(t, err) == nil {
		t.Fatal("атакующий должен быть заблокирован")
	}
	// владелец с другого IP входит свободно
	if _, err := e.svc.Login(ctx, "target", "правильный пароль", ClientInfo{IP: "198.51.100.61"}); err != nil {
		t.Errorf("другой IP: %v", err)
	}
	// тот же IP, другой логин
	if _, err := e.svc.Login(ctx, "bystander", "другой пароль", attacker); err != nil {
		t.Errorf("другой логин с того же IP: %v", err)
	}
}

func TestLoginLockoutPerLoginAcrossIPs(t *testing.T) {
	e := newEnv(t) // 15 неудач на логин с любых IP
	e.register("victim", "правильный пароль")
	for i := 0; i < 15; i++ {
		ci := ClientInfo{IP: fmt.Sprintf("192.0.2.%d", i+1)} // каждый раз новый IP: лимит по IP+логин не срабатывает
		if _, err := e.svc.Login(ctx, "victim", "неверно", ci); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("попытка %d: %v", i+1, err)
		}
	}
	// теперь логин заблокирован для всех IP, в том числе для нового
	_, err := e.svc.Login(ctx, "victim", "правильный пароль", ClientInfo{IP: "192.0.2.200"})
	mustRateLimited(t, err)
	e.clock.Advance(15*time.Minute + time.Second)
	if _, err := e.svc.Login(ctx, "victim", "правильный пароль", ClientInfo{IP: "192.0.2.200"}); err != nil {
		t.Errorf("после окна: %v", err)
	}
}

func TestSuccessfulLoginResetsOnlyIPCounter(t *testing.T) {
	e := newEnv(t)
	e.register("owner", "правильный пароль")
	ci := ClientInfo{IP: "198.51.100.70"}
	for i := 0; i < 4; i++ {
		_, _ = e.svc.Login(ctx, "owner", "неверно", ci)
	}
	if _, err := e.svc.Login(ctx, "owner", "правильный пароль", ci); err != nil {
		t.Fatal(err)
	}
	// счётчик пары IP+логин обнулён: ещё 5 неудач допустимы
	for i := 0; i < 5; i++ {
		if _, err := e.svc.Login(ctx, "owner", "неверно", ci); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("после сброса попытка %d: %v", i+1, err)
		}
	}
	// а счётчик по самому логину не сбросился: там накоплено 4+5 = 9 неудач
	if blocked, _ := e.limiter.Blocked(loginLockKey("owner"), 15, 15*time.Minute); blocked {
		t.Fatal("9 неудач ещё не блокировка по логину")
	}
	if n := countEvents(e, loginLockKey("owner")); n != 9 {
		t.Errorf("по логину накоплено %d неудач, ожидалось 9: успешный вход не должен их обнулять", n)
	}
}

// countEvents считает события по ключу через проверку порогов (ограничитель наружу события не отдаёт).
func countEvents(e *env, key string) int {
	for n := 1; n <= 100; n++ {
		if blocked, _ := e.limiter.Blocked(key, n, time.Hour); !blocked {
			return n - 1
		}
	}
	return 100
}

func TestUnknownLoginFailuresAlsoCount(t *testing.T) {
	e := newEnv(t)
	ci := ClientInfo{IP: "198.51.100.80"}
	for i := 0; i < 5; i++ {
		_, _ = e.svc.Login(ctx, "нет-такого", "неверно", ci)
	}
	// по блокировке нельзя отличить существующий логин от несуществующего
	_, err := e.svc.Login(ctx, "нет-такого", "неверно", ci)
	mustRateLimited(t, err)
}

func TestLoginRehashesWhenParamsAreWeaker(t *testing.T) {
	e := newEnv(t)
	e.register("upgrader", "правильный пароль")

	stronger, err := passwords.NewHasher(passwords.Params{MemoryKiB: 128, Iterations: 2, Parallelism: 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	svc2, err := NewService(Options{DB: e.db, Hasher: stronger, Limiter: ratelimit.New(nil), Limits: config.DefaultLimits(), Log: testutil.Logger(), Now: e.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc2.Login(ctx, "upgrader", "правильный пароль", e.freshIP()); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := e.db.Raw("SELECT password_hash FROM users WHERE login = 'upgrader'").Scan(&hash).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hash, "m=128,t=2,p=1") {
		t.Fatalf("хеш не обновлён до новых параметров: %s", hash)
	}
	// и после обновления вход по-прежнему работает
	if _, err := svc2.Login(ctx, "upgrader", "правильный пароль", e.freshIP()); err != nil {
		t.Fatalf("вход после пересчёта хеша: %v", err)
	}
}

func TestPasswordIsNormalizedBetweenRegistrationAndLogin(t *testing.T) {
	e := newEnv(t)
	e.register("unicodeuser", "и"+combiningBreve+"-пароль-1") // «й» как «и» + сочетаемая краткая
	if _, err := e.svc.Login(ctx, "unicodeuser", "й-пароль-1", e.freshIP()); err != nil {
		t.Fatalf("тот же пароль в другой форме Unicode должен подходить: %v", err)
	}
}

// ---------------------------------------------------------------- сессии

func TestAuthenticateRejectsBadTokens(t *testing.T) {
	e := newEnv(t)
	reg := e.register("sessuser", "password-1")
	for _, tok := range []string{"", "короткий", strings.Repeat("A", 43), strings.Repeat("A", 44), reg.Token[:42], reg.Token + "x", strings.ToLower(reg.Token)} {
		if tok == reg.Token {
			continue
		}
		if _, err := e.svc.Authenticate(ctx, tok); !errors.Is(err, ErrNoSession) {
			t.Errorf("токен %q: %v", tok, err)
		}
	}
	if _, err := e.svc.Authenticate(ctx, reg.Token); err != nil {
		t.Errorf("настоящий токен: %v", err)
	}
}

func TestSessionSlidingRenewalAndExpiry(t *testing.T) {
	e := newEnv(t)
	reg := e.register("slider", "password-1")
	created := e.clock.Now()

	// в пределах 5 минут срок не трогаем (не пишем в БД на каждый запрос)
	e.clock.Advance(4 * time.Minute)
	a, err := e.svc.Authenticate(ctx, reg.Token)
	if err != nil || a.Renewed {
		t.Fatalf("до 5 минут: renewed=%v err=%v", a != nil && a.Renewed, err)
	}

	// позже — продлевается на полный срок от текущего момента
	e.clock.Advance(2 * time.Minute)
	a, err = e.svc.Authenticate(ctx, reg.Token)
	if err != nil || !a.Renewed {
		t.Fatalf("после 5 минут: err=%v", err)
	}
	if want := e.clock.Now().Add(SessionSlidingTTL); !a.Session.ExpiresAt.Equal(want) {
		t.Errorf("срок %s, ожидался %s", a.Session.ExpiresAt, want)
	}
	if !a.Session.AbsoluteExpiresAt.Equal(created.Add(SessionAbsoluteTTL)) {
		t.Error("абсолютный срок не должен меняться")
	}
	// продление записано в БД, а не только в ответе
	var stored time.Time
	if err := e.db.Raw("SELECT expires_at FROM sessions WHERE user_id = ?", reg.User.ID).Scan(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.Equal(a.Session.ExpiresAt) {
		t.Errorf("в БД %s, в ответе %s", stored, a.Session.ExpiresAt)
	}
}

func TestSessionExpiresWithoutActivity(t *testing.T) {
	e := newEnv(t)
	reg := e.register("idle", "password-1")
	e.clock.Advance(SessionSlidingTTL - time.Minute)
	if _, err := e.svc.Authenticate(ctx, reg.Token); err != nil {
		t.Fatalf("за минуту до истечения: %v", err)
	}
	// активность продлила сессию; без неё она истекает через 30 дней
	e.clock.Advance(SessionSlidingTTL + time.Minute)
	if _, err := e.svc.Authenticate(ctx, reg.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("после 30 дней простоя: %v", err)
	}
}

func TestSessionAbsoluteLifetimeCannotBeExtended(t *testing.T) {
	e := newEnv(t)
	reg := e.register("forever", "password-1")
	absolute := e.clock.Now().Add(SessionAbsoluteTTL)

	// заходим каждые 10 дней: скользящий срок постоянно продлевается, но упирается в абсолютный
	for day := 10; day <= 80; day += 10 {
		e.clock.Advance(10 * 24 * time.Hour)
		if _, err := e.svc.Authenticate(ctx, reg.Token); err != nil {
			t.Fatalf("день %d: %v", day, err)
		}
	}
	e.clock.Advance(5 * 24 * time.Hour) // день 85
	a, err := e.svc.Authenticate(ctx, reg.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Session.ExpiresAt.Equal(absolute) {
		t.Errorf("срок %s должен быть ограничен абсолютным %s", a.Session.ExpiresAt, absolute)
	}
	e.clock.Advance(6 * 24 * time.Hour) // день 91
	if _, err := e.svc.Authenticate(ctx, reg.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("после 90 дней сессия должна закончиться: %v", err)
	}
}

func TestLogoutAndRevokeSessions(t *testing.T) {
	e := newEnv(t)
	reg := e.register("multi", "password-1")
	s2, err := e.svc.Login(ctx, "multi", "password-1", e.freshIP())
	if err != nil {
		t.Fatal(err)
	}
	s3, _ := e.svc.Login(ctx, "multi", "password-1", e.freshIP())

	if err := e.svc.Logout(ctx, s2.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(ctx, s2.Token); !errors.Is(err, ErrNoSession) {
		t.Error("сессия после выхода жива")
	}
	if _, err := e.svc.Authenticate(ctx, reg.Token); err != nil {
		t.Error("выход из одной сессии не должен трогать остальные")
	}
	if err := e.svc.Logout(ctx, s2.Token); err != nil {
		t.Errorf("повторный выход не должен быть ошибкой: %v", err)
	}
	if err := e.svc.Logout(ctx, "мусор"); err != nil {
		t.Errorf("выход с мусорным токеном: %v", err)
	}

	a, _ := e.svc.Authenticate(ctx, reg.Token)
	if err := e.svc.RevokeSessions(ctx, reg.User.ID, a.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(ctx, reg.Token); err != nil {
		t.Error("исключённая сессия должна остаться")
	}
	if _, err := e.svc.Authenticate(ctx, s3.Token); !errors.Is(err, ErrNoSession) {
		t.Error("остальные сессии должны быть завершены")
	}
	if err := e.svc.RevokeSessions(ctx, reg.User.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(ctx, reg.Token); !errors.Is(err, ErrNoSession) {
		t.Error("RevokeSessions(…, 0) должен завершить все сессии")
	}
}

func TestDeletedUserSessionStopsWorking(t *testing.T) {
	e := newEnv(t)
	reg := e.register("doomed", "password-1")
	if err := e.db.Exec("DELETE FROM users WHERE id = ?", reg.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(ctx, reg.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("сессия удалённого пользователя: %v", err)
	}
}

// ---------------------------------------------------------------- смена пароля

func TestChangePassword(t *testing.T) {
	e := newEnv(t)
	reg := e.register("changer", "старый пароль")
	other, _ := e.svc.Login(ctx, "changer", "старый пароль", e.freshIP())
	cur, _ := e.svc.Authenticate(ctx, reg.Token)
	e.clock.Advance(time.Hour)

	if err := e.svc.ChangePassword(ctx, reg.User.ID, cur.Session.ID, "старый пароль", "новый пароль 2", e.freshIP()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Login(ctx, "changer", "старый пароль", e.freshIP()); !errors.Is(err, ErrInvalidCredentials) {
		t.Error("старый пароль должен перестать работать")
	}
	if _, err := e.svc.Login(ctx, "changer", "новый пароль 2", e.freshIP()); err != nil {
		t.Errorf("новый пароль: %v", err)
	}
	if _, err := e.svc.Authenticate(ctx, reg.Token); err != nil {
		t.Error("текущая сессия должна остаться")
	}
	if _, err := e.svc.Authenticate(ctx, other.Token); !errors.Is(err, ErrNoSession) {
		t.Error("остальные сессии должны быть завершены")
	}
	var changed time.Time
	_ = e.db.Raw("SELECT password_changed_at FROM users WHERE id = ?", reg.User.ID).Scan(&changed).Error
	if !changed.Equal(e.clock.Now()) {
		t.Errorf("password_changed_at = %s", changed)
	}
}

func TestChangePasswordRejections(t *testing.T) {
	e := newEnv(t)
	reg := e.register("changer2", "старый пароль")
	cur, _ := e.svc.Authenticate(ctx, reg.Token)
	ci := e.freshIP()

	if err := e.svc.ChangePassword(ctx, reg.User.ID, cur.Session.ID, "не тот", "новый пароль 2", ci); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("неверный текущий: %v", err)
	}
	err := e.svc.ChangePassword(ctx, reg.User.ID, cur.Session.ID, "старый пароль", "коротко", ci)
	if f := mustValidation(t, err); f["new_password"] == "" {
		t.Errorf("слабый новый пароль: %v", f)
	}
	err = e.svc.ChangePassword(ctx, reg.User.ID, cur.Session.ID, "старый пароль", "старый пароль", ci)
	if f := mustValidation(t, err); !strings.Contains(f["new_password"], "совпадает") {
		t.Errorf("тот же пароль: %v", f)
	}
	if err := e.svc.ChangePassword(ctx, 999999, cur.Session.ID, "x", "новый пароль 2", ci); !errors.Is(err, ErrNoSession) {
		t.Errorf("несуществующий пользователь: %v", err)
	}
	// пароль не изменился
	if _, err := e.svc.Login(ctx, "changer2", "старый пароль", e.freshIP()); err != nil {
		t.Errorf("пароль не должен был измениться: %v", err)
	}
}

func TestChangePasswordCannotBeUsedToBruteForce(t *testing.T) {
	e := newEnv(t)
	reg := e.register("brute", "правильный пароль")
	cur, _ := e.svc.Authenticate(ctx, reg.Token)
	ci := ClientInfo{IP: "198.51.100.90"}
	for i := 0; i < 5; i++ {
		if err := e.svc.ChangePassword(ctx, reg.User.ID, cur.Session.ID, fmt.Sprintf("guess%d", i), "новый пароль 2", ci); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("попытка %d: %v", i+1, err)
		}
	}
	err := e.svc.ChangePassword(ctx, reg.User.ID, cur.Session.ID, "правильный пароль", "новый пароль 2", ci)
	mustRateLimited(t, err) // украденная сессия не даёт перебирать пароль без ограничений
}

// ---------------------------------------------------------------- удаление аккаунта

func TestDeleteAccount(t *testing.T) {
	e := newEnv(t)
	reg := e.register("leaver", "правильный пароль")
	_, _ = e.svc.Login(ctx, "leaver", "правильный пароль", e.freshIP())
	ci := e.freshIP()

	if err := e.svc.DeleteAccount(ctx, reg.User.ID, "неверный", ci); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("неверный пароль: %v", err)
	}
	if n := e.count("SELECT count(*) FROM users WHERE id = ?", reg.User.ID); n != 1 {
		t.Fatal("аккаунт удалён без подтверждения пароля")
	}

	if err := e.svc.DeleteAccount(ctx, reg.User.ID, "правильный пароль", ci); err != nil {
		t.Fatal(err)
	}
	if n := e.count("SELECT count(*) FROM users"); n != 0 {
		t.Errorf("пользователей осталось %d", n)
	}
	if n := e.count("SELECT count(*) FROM sessions"); n != 0 {
		t.Errorf("сессий осталось %d: удаление должно быть полным", n)
	}
	if _, err := e.svc.Authenticate(ctx, reg.Token); !errors.Is(err, ErrNoSession) {
		t.Error("сессия удалённого аккаунта жива")
	}
	if _, err := e.svc.Login(ctx, "leaver", "правильный пароль", e.freshIP()); !errors.Is(err, ErrInvalidCredentials) {
		t.Error("в удалённый аккаунт можно войти")
	}
	// логин освободился
	if _, err := e.registerWith("leaver", "password-1", e.freshIP()); err != nil {
		t.Errorf("логин после удаления должен освободиться: %v", err)
	}
	if err := e.svc.DeleteAccount(ctx, reg.User.ID, "x", ci); !errors.Is(err, ErrNoSession) {
		t.Errorf("повторное удаление: %v", err)
	}
}

// ---------------------------------------------------------------- восстановление по резервному коду

func TestRestoreAccessWithBackupCode(t *testing.T) {
	e := newEnv(t)
	reg := e.register("forgetful", "забытый пароль")
	old, _ := e.svc.Login(ctx, "forgetful", "забытый пароль", e.freshIP())
	e.clock.Advance(time.Hour)

	// код можно вводить в любом виде
	typed := strings.ToLower(strings.ReplaceAll(reg.BackupCode, "-", " "))
	res, err := e.svc.Restore(t, "Forgetful", typed, "новый пароль 1", e.freshIP())
	if err != nil {
		t.Fatal(err)
	}
	if res.BackupCode == reg.BackupCode || !backupRe.MatchString(res.BackupCode) {
		t.Errorf("новый код: %q (старый %q)", res.BackupCode, reg.BackupCode)
	}
	if _, err := e.svc.Login(ctx, "forgetful", "забытый пароль", e.freshIP()); !errors.Is(err, ErrInvalidCredentials) {
		t.Error("старый пароль должен перестать работать")
	}
	if _, err := e.svc.Login(ctx, "forgetful", "новый пароль 1", e.freshIP()); err != nil {
		t.Errorf("новый пароль: %v", err)
	}
	for name, tok := range map[string]string{"регистрационная": reg.Token, "по входу": old.Token} {
		if _, err := e.svc.Authenticate(ctx, tok); !errors.Is(err, ErrNoSession) {
			t.Errorf("сессия %s должна быть завершена", name)
		}
	}
	if _, err := e.svc.Authenticate(ctx, res.Token); err != nil {
		t.Errorf("новая сессия: %v", err)
	}

	// старый код сгорел, новый работает
	if _, err := e.svc.Restore(t, "forgetful", reg.BackupCode, "ещё пароль 3", e.freshIP()); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("использованный код: %v", err)
	}
	if _, err := e.svc.Restore(t, "forgetful", res.BackupCode, "ещё пароль 3", e.freshIP()); err != nil {
		t.Errorf("новый код: %v", err)
	}
}

// Restore — короткая обёртка для тестов.
func (s *Service) Restore(t *testing.T, login, code, newPassword string, ci ClientInfo) (*RestoreResult, error) {
	t.Helper()
	return s.RestoreAccess(ctx, login, code, newPassword, ci)
}

func TestRestoreAccessFailuresAreIndistinguishableAndLimited(t *testing.T) {
	e := newEnv(t)
	reg := e.register("guarded", "правильный пароль")
	ci := ClientInfo{IP: "198.51.100.100"}
	newPass := "новый пароль 1"

	wrongCode, _ := GenerateBackupCode()
	cases := map[string]struct{ login, code string }{
		"неверный код":       {"guarded", wrongCode},
		"нет такого логина":  {"ghost", reg.BackupCode},
		"код неверного вида": {"guarded", "не код"},
		"код с кириллицей":   {"guarded", "КУПОЛ-ААAA-BBBB-CCCC-DDDD"},
	}
	for name, c := range cases {
		if _, err := e.svc.Restore(t, c.login, c.code, newPass, ci); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Лимит: 5 неудач на пару IP+логин. На «guarded» уже потрачено 3 (неверный код, код неверного вида,
	// код с кириллицей); попытка с логином «ghost» считается на другой ключ.
	for i := 4; i <= 5; i++ {
		if _, err := e.svc.Restore(t, "guarded", wrongCode, newPass, ci); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("попытка %d: %v", i, err)
		}
	}
	_, err := e.svc.Restore(t, "guarded", reg.BackupCode, newPass, ci) // 6-я — даже верный код отклоняется
	mustRateLimited(t, err)
	// пароль остался прежним
	if _, err := e.svc.Login(ctx, "guarded", "правильный пароль", e.freshIP()); err != nil {
		t.Errorf("пароль не должен был измениться: %v", err)
	}
}

func TestRestoreAccessValidatesFieldsWithoutCountingAttempts(t *testing.T) {
	e := newEnv(t)
	reg := e.register("validator", "правильный пароль")
	ci := ClientInfo{IP: "198.51.100.110"}
	for i := 0; i < 20; i++ {
		_, err := e.svc.Restore(t, "validator", reg.BackupCode, "слабый", ci)
		if f := mustValidation(t, err); f["new_password"] == "" {
			t.Fatalf("поля: %v", f)
		}
	}
	// 20 ошибок валидации не заблокировали настоящее восстановление
	if _, err := e.svc.Restore(t, "validator", reg.BackupCode, "нормальный пароль", ci); err != nil {
		t.Fatalf("восстановление после ошибок валидации: %v", err)
	}
	_, err := e.svc.Restore(t, "", "", "", ci)
	f := mustValidation(t, err)
	if f["login"] == "" || f["backup_code"] == "" || f["new_password"] == "" {
		t.Errorf("пустая форма: %v", f)
	}
}

// ---------------------------------------------------------------- администрирование

func TestAdminOperations(t *testing.T) {
	e := newEnv(t)
	reg := e.register("Promotable", "password-1")

	if err := e.svc.SetLevel(ctx, "promotable", LevelCurator); err != nil {
		t.Fatal(err)
	}
	a, _ := e.svc.Authenticate(ctx, reg.Token)
	if a.User.Level != LevelCurator || a.User.LevelName() != "Куратор" {
		t.Errorf("уровень: %+v", a.User)
	}
	for _, bad := range []int{0, 7, -1, 100} {
		if err := e.svc.SetLevel(ctx, "promotable", bad); err == nil {
			t.Errorf("уровень %d должен отвергаться", bad)
		}
	}
	if err := e.svc.SetLevel(ctx, "nobody", 3); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("несуществующий пользователь: %v", err)
	}

	if err := e.svc.SetDirectorate(ctx, "PROMOTABLE", true); err != nil {
		t.Fatal(err)
	}
	a, _ = e.svc.Authenticate(ctx, reg.Token)
	if !a.User.Directorate || a.User.LevelName() != "Директорат" {
		t.Errorf("Директорат: %+v", a.User)
	}
	if err := e.svc.SetDirectorate(ctx, "promotable", false); err != nil {
		t.Fatal(err)
	}
	if a, _ = e.svc.Authenticate(ctx, reg.Token); a.User.Directorate {
		t.Error("Директорат не снят")
	}
}

func TestAdminResetPassword(t *testing.T) {
	e := newEnv(t)
	reg := e.register("lockedout", "потерянный пароль")
	// пользователя «забанили» перебором: блокировка не должна мешать входу с временным паролем
	ci := ClientInfo{IP: "198.51.100.120"}
	for i := 0; i < 15; i++ {
		_, _ = e.svc.Login(ctx, "lockedout", "неверно", ClientInfo{IP: fmt.Sprintf("192.0.2.%d", i+1)})
	}

	temp, err := e.svc.AdminResetPassword(ctx, "LockedOut")
	if err != nil {
		t.Fatal(err)
	}
	if len(temp) != 16 || strings.ContainsAny(temp, "0O1lI") {
		t.Errorf("временный пароль %q: 16 символов без похожих", temp)
	}
	if temp2, _ := e.svc.AdminResetPassword(ctx, "lockedout"); temp2 == temp {
		t.Error("временные пароли одинаковы")
	}
	temp, _ = e.svc.AdminResetPassword(ctx, "lockedout")

	if _, err := e.svc.Authenticate(ctx, reg.Token); !errors.Is(err, ErrNoSession) {
		t.Error("сессии должны быть завершены")
	}
	if _, err := e.svc.Login(ctx, "lockedout", "потерянный пароль", ClientInfo{IP: "198.51.100.121"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Error("старый пароль должен перестать работать")
	}
	if _, err := e.svc.Login(ctx, "lockedout", temp, ci); err != nil {
		t.Fatalf("вход с временным паролем: %v", err)
	}
	if _, err := e.svc.AdminResetPassword(ctx, "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("несуществующий пользователь: %v", err)
	}
}

// ---------------------------------------------------------------- обслуживание

func TestCleanupRemovesOnlyExpired(t *testing.T) {
	e := newEnv(t)
	old := e.register("olduser", "password-1")
	e.clock.Advance(SessionSlidingTTL + time.Hour)
	fresh := e.register("freshuser", "password-1")
	stale, _ := e.svc.NewCaptcha(ctx) // будет просрочена
	e.clock.Advance(captchaTTL + time.Second)
	live, _ := e.svc.NewCaptcha(ctx)

	sessions, captchas, err := e.svc.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Анкеты регистраций погашены при использовании; просрочена только stale.
	if sessions != 1 || captchas != 1 {
		t.Fatalf("удалено сессий %d, анкет %d; ожидалось 1 и 1", sessions, captchas)
	}
	if _, err := e.svc.Authenticate(ctx, fresh.Token); err != nil {
		t.Error("живая сессия удалена")
	}
	if n := e.count("SELECT count(*) FROM sessions WHERE user_id = ?", old.User.ID); n != 0 {
		t.Error("просроченная сессия не удалена")
	}
	if n := e.count("SELECT count(*) FROM captcha_challenges WHERE id = ?", live.ID); n != 1 {
		t.Error("живая анкета удалена")
	}
	if n := e.count("SELECT count(*) FROM captcha_challenges WHERE id = ?", stale.ID); n != 0 {
		t.Error("просроченная анкета не удалена")
	}
	// пользователи не затронуты
	if n := e.count("SELECT count(*) FROM users"); n != 2 {
		t.Errorf("пользователей %d", n)
	}
}

func TestRunCleanupStopsOnCancel(t *testing.T) {
	e := newEnv(t)
	c, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { e.svc.RunCleanup(c, 10*time.Millisecond); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunCleanup не остановился по отмене контекста")
	}
}

func TestNewServiceValidatesOptions(t *testing.T) {
	e := newEnv(t)
	h, _ := passwords.NewHasher(fastParams, 1)
	good := Options{DB: e.db, Hasher: h, Limiter: ratelimit.New(nil), Limits: config.DefaultLimits(), Log: testutil.Logger()}
	if _, err := NewService(good); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Options){
		"нет БД":       func(o *Options) { o.DB = nil },
		"нет хешера":   func(o *Options) { o.Hasher = nil },
		"нет лимитера": func(o *Options) { o.Limiter = nil },
		"нет логгера":  func(o *Options) { o.Log = nil },
		"нулевые лимиты": func(o *Options) {
			o.Limits = config.Limits{}
		},
	} {
		o := good
		mutate(&o)
		if _, err := NewService(o); err == nil {
			t.Errorf("%s: ожидалась ошибка", name)
		}
	}
}

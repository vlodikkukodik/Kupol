package accounts

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"kupol/internal/audit"
	"kupol/internal/config"
	"kupol/internal/passwords"
	"kupol/internal/ratelimit"
	"kupol/internal/testutil"
)

var testSecretKey = []byte("тестовый общий секрет сервера, не короче 32 байт")

const totpPassword = "правильный пароль"

// ---------------------------------------------------------------- чистые функции

// Векторы RFC 6238 (приложение B, SHA-1, секрет «12345678901234567890»): в таблице 8 цифр, мы берём последние 6.
func TestTOTPCodeMatchesRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	} {
		if got := totpCode(secret, unix/30); got != want {
			t.Errorf("t=%d: код %s, по RFC ожидался %s", unix, got, want)
		}
	}
}

func TestVerifyTOTPWindowReplayAndFormat(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111111, 0) // шаг 37037037
	cur := totpStep(now)

	for name, tc := range map[string]struct {
		code string
		last int64
		ok   bool
	}{
		"текущий шаг":                   {totpCode(secret, cur), 0, true},
		"шаг назад (часы телефона)":     {totpCode(secret, cur-1), 0, true},
		"шаг вперёд":                    {totpCode(secret, cur+1), 0, true},
		"два шага назад — поздно":       {totpCode(secret, cur-2), 0, false},
		"два шага вперёд — рано":        {totpCode(secret, cur+2), 0, false},
		"тот же шаг повторно":           {totpCode(secret, cur), cur, false},
		"более старый шаг после нового": {totpCode(secret, cur-1), cur, false},
		"следующий шаг после старого":   {totpCode(secret, cur+1), cur, true},
		"с пробелом, как в приложении":  {totpCode(secret, cur)[:3] + " " + totpCode(secret, cur)[3:], 0, true},
		"5 цифр": {totpCode(secret, cur)[:5], 0, false},
		"7 цифр": {totpCode(secret, cur) + "1", 0, false},
		"буквы":  {"12a456", 0, false},
		"пусто":  {"", 0, false},
	} {
		if _, ok := verifyTOTP(secret, tc.code, now, tc.last); ok != tc.ok {
			t.Errorf("%s: принят=%v, ожидалось %v", name, ok, tc.ok)
		}
	}
	// возвращается шаг, по которому код подошёл — по нему потом запрещается повтор
	if step, ok := verifyTOTP(secret, totpCode(secret, cur-1), now, 0); !ok || step != cur-1 {
		t.Errorf("шаг = %d, ожидался %d", step, cur-1)
	}
	// чужой секрет не подходит
	if _, ok := verifyTOTP([]byte("другой секрет 20 байт"), totpCode(secret, cur), now, 0); ok {
		t.Error("код принят по чужому секрету")
	}
}

func TestTOTPURI(t *testing.T) {
	secret := []byte("12345678901234567890")
	u, err := url.Parse(totpURI(secret, "Влад Тест"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "otpauth" || u.Host != "totp" || q.Get("secret") != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" ||
		q.Get("issuer") != "КУПОЛ" || q.Get("algorithm") != "SHA1" || q.Get("digits") != "6" || q.Get("period") != "30" {
		t.Errorf("ссылка: %s", u)
	}
	if label, _ := url.PathUnescape(strings.TrimPrefix(u.EscapedPath(), "/")); label != "КУПОЛ:Влад Тест" {
		t.Errorf("метка в приложении: %q", label)
	}
}

func TestSecretBoxBindsToUserAndDetectsTampering(t *testing.T) {
	box, err := newSecretBox(testSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("12345678901234567890")
	sealed, err := box.seal(7, secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, secret) {
		t.Fatal("секрет лежит в шифртексте открыто")
	}
	if got, err := box.open(7, sealed); err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("расшифровка: %q %v", got, err)
	}
	if _, err := box.open(8, sealed); err == nil {
		t.Error("шифртекст открылся под чужим номером пользователя")
	}
	bad := bytes.Clone(sealed)
	bad[len(bad)-1] ^= 1
	if _, err := box.open(7, bad); err == nil {
		t.Error("подделанный шифртекст принят")
	}
	if _, err := box.open(7, sealed[:5]); err == nil {
		t.Error("обрезанный шифртекст принят")
	}
	other, _ := newSecretBox([]byte("совсем другой общий секрет сервера, тоже длинный"))
	if _, err := other.open(7, sealed); err == nil {
		t.Error("шифртекст открылся другим секретом сервера")
	}
	again, _ := box.seal(7, secret)
	if bytes.Equal(sealed, again) {
		t.Error("два шифрования одного секрета совпали: nonce не меняется")
	}
	if _, err := newSecretBox([]byte("короткий")); err == nil {
		t.Error("короткий секрет сервера принят")
	}
}

func TestRecoveryCodesFormat(t *testing.T) {
	codes, err := newRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if seen[c] {
			t.Errorf("повтор кода %s", c)
		}
		seen[c] = true
		canon, ok := canonicalRecoveryCode(c)
		if !ok || len(c) != 11 || c[5] != '-' || c != strings.ToLower(c) {
			t.Errorf("код %q: канонический %q, ok=%v", c, canon, ok)
		}
	}
	if len(codes) != 10 {
		t.Errorf("кодов %d", len(codes))
	}
	c := codes[0]
	for _, variant := range []string{strings.ToUpper(c), strings.ReplaceAll(c, "-", ""), " " + c + " ", strings.ReplaceAll(c, "-", " ")} {
		if got, ok := canonicalRecoveryCode(variant); !ok || got != strings.ToUpper(strings.ReplaceAll(c, "-", "")) {
			t.Errorf("вариант %q не приведён к коду", variant)
		}
	}
	for _, bad := range []string{"", "abc", c + "x", "abcde-fghi0", "абвгд-еёжзи"} {
		if _, ok := canonicalRecoveryCode(bad); ok {
			t.Errorf("%q принят за одноразовый код", bad)
		}
	}
}

// ---------------------------------------------------------------- сценарии

// enrolled — пользователь с включённым кодом из приложения: возвращает секрет и одноразовые коды.
type enrolled struct {
	login  string
	id     int64
	secret []byte
	codes  []string
}

func (e *env) enroll(login string) *enrolled {
	e.t.Helper()
	reg := e.register(login, totpPassword)
	ctx := context.Background()
	setup, err := e.svc.TOTPBegin(ctx, reg.User.ID, totpPassword, e.freshIP())
	if err != nil {
		e.t.Fatalf("TOTPBegin: %v", err)
	}
	secret, err := b32.DecodeString(setup.Secret)
	if err != nil {
		e.t.Fatal(err)
	}
	codes, err := e.svc.TOTPEnable(ctx, reg.User.ID, 0, totpCode(secret, totpStep(e.clock.Now())))
	if err != nil {
		e.t.Fatalf("TOTPEnable: %v", err)
	}
	return &enrolled{login: login, id: reg.User.ID, secret: secret, codes: codes}
}

// code — верный сейчас код из приложения.
func (e *env) code(u *enrolled) string { return totpCode(u.secret, totpStep(e.clock.Now())) }

func (e *env) loginCode(u *enrolled, password, code string) (*AuthResult, error) {
	e.t.Helper()
	return e.svc.LoginWithCode(context.Background(), u.login, password, code, e.freshIP())
}

func TestTOTPEnrollmentIsOptionalAndNeedsFirstCode(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	reg := e.register("Vladislav", totpPassword)
	ci := e.freshIP()

	// без включения вход прежний
	if _, err := e.svc.Login(ctx, "Vladislav", totpPassword, ci); err != nil {
		t.Fatalf("вход без TOTP: %v", err)
	}
	// начать нельзя без пароля
	if _, err := e.svc.TOTPBegin(ctx, reg.User.ID, "не тот пароль", ci); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("подключение с чужим паролем: %v", err)
	}
	// «включить» без начала — нечего подтверждать
	if _, err := e.svc.TOTPEnable(ctx, reg.User.ID, 0, "123456"); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("включение без начала: %v", err)
	}
	setup, err := e.svc.TOTPBegin(ctx, reg.User.ID, totpPassword, ci)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := b32.DecodeString(setup.Secret)
	if len(secret) != 20 || !strings.HasPrefix(setup.URI, "otpauth://totp/") || !strings.Contains(setup.URI, "secret="+setup.Secret) {
		t.Fatalf("выдача: %+v", setup)
	}

	// пока код не подтверждён, защиты нет и вход прежний; неверный код её не включает
	if _, err := e.svc.TOTPEnable(ctx, reg.User.ID, 0, "000000"); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("неверный первый код: %v", err)
	}
	if u, _ := e.svc.Find(ctx, "Vladislav"); u.TOTPEnabled() {
		t.Fatal("защита включилась без верного кода")
	}
	if _, err := e.svc.Login(ctx, "Vladislav", totpPassword, ci); err != nil {
		t.Fatalf("вход до подтверждения: %v", err)
	}
	// секрет в БД зашифрован, а не лежит как есть
	var stored []byte
	if err := e.db.Raw("SELECT totp_pending FROM users WHERE id = ?", reg.User.ID).Row().Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) == 0 || bytes.Contains(stored, secret) || strings.Contains(string(stored), setup.Secret) {
		t.Fatal("секрет хранится открыто")
	}

	// повторное «начать» заменяет неподтверждённый секрет: старый QR уже не подойдёт
	setup2, err := e.svc.TOTPBegin(ctx, reg.User.ID, totpPassword, ci)
	if err != nil {
		t.Fatal(err)
	}
	if setup2.Secret == setup.Secret {
		t.Fatal("секрет не заменён")
	}
	if _, err := e.svc.TOTPEnable(ctx, reg.User.ID, 0, totpCode(secret, totpStep(e.clock.Now()))); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("код от заменённого секрета принят: %v", err)
	}

	// у остальных сессий защита включается вместе с завершением
	other, err := e.svc.Login(ctx, "Vladislav", totpPassword, e.freshIP())
	if err != nil {
		t.Fatal(err)
	}
	secret2, _ := b32.DecodeString(setup2.Secret)
	auth, err := e.svc.Authenticate(ctx, other.Token)
	if err != nil {
		t.Fatal(err)
	}
	codes, err := e.svc.TOTPEnable(ctx, reg.User.ID, auth.Session.ID, totpCode(secret2, totpStep(e.clock.Now())))
	if err != nil {
		t.Fatalf("включение: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("одноразовых кодов %d", len(codes))
	}
	if _, err := e.svc.Authenticate(ctx, other.Token); err != nil {
		t.Errorf("сессия, с которой включали, завершена: %v", err)
	}
	if _, err := e.svc.Authenticate(ctx, reg.Token); !errors.Is(err, ErrNoSession) {
		t.Errorf("прежняя сессия осталась: %v", err)
	}
	if n := e.count("SELECT count(*) FROM sessions WHERE user_id = ?", reg.User.ID); n != 1 {
		t.Errorf("сессий после включения %d", n)
	}
	// одноразовые коды в БД — хеши
	if n := e.count("SELECT count(*) FROM totp_recovery_codes WHERE user_id = ? AND used_at IS NULL", reg.User.ID); n != 10 {
		t.Errorf("кодов в БД %d", n)
	}
	for _, c := range codes {
		canon, _ := canonicalRecoveryCode(c)
		var stored []byte
		if err := e.db.Raw("SELECT code_hash FROM totp_recovery_codes WHERE user_id = ? AND code_hash = ?", reg.User.ID, recoveryHash(canon)).Row().Scan(&stored); err != nil {
			t.Fatalf("хеша кода %s нет в БД: %v", c, err)
		}
		if bytes.Contains(stored, []byte(canon)) || bytes.Contains(stored, []byte(c)) {
			t.Fatal("одноразовый код лежит в БД открыто")
		}
	}
	if rows := e.auditRows(audit.TOTPEnabled); len(rows) != 1 || rows[0].Actor == nil || *rows[0].Actor != "Vladislav" {
		t.Errorf("журнал: %+v", rows)
	}
	// уже включено: ни начать, ни подтвердить заново
	if _, err := e.svc.TOTPBegin(ctx, reg.User.ID, totpPassword, ci); !errors.Is(err, ErrTOTPAlreadyEnabled) {
		t.Errorf("повторное начало: %v", err)
	}
	if _, err := e.svc.TOTPEnable(ctx, reg.User.ID, 0, "123456"); !errors.Is(err, ErrTOTPAlreadyEnabled) {
		t.Errorf("повторное включение: %v", err)
	}
}

func TestLoginRequiresCodeOnlyAfterCorrectPassword(t *testing.T) {
	e := newEnv(t)
	u := e.enroll("Vera")
	e.clock.Advance(time.Minute) // код подтверждения подключения уже израсходован

	// неверный пароль — обычная ошибка входа, без намёка на защиту
	if _, err := e.loginCode(u, "не тот пароль", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("неверный пароль: %v", err)
	}
	if _, err := e.loginCode(u, "не тот пароль", e.code(u)); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("неверный пароль с верным кодом: %v", err)
	}
	// верный пароль без кода — нужен код, сессия не создана
	before := e.count("SELECT count(*) FROM sessions WHERE user_id = ?", u.id)
	if _, err := e.loginCode(u, totpPassword, ""); !errors.Is(err, ErrTOTPRequired) {
		t.Fatalf("без кода: %v", err)
	}
	if _, err := e.svc.Login(context.Background(), u.login, totpPassword, e.freshIP()); !errors.Is(err, ErrTOTPRequired) {
		t.Fatalf("Login без кода: %v", err)
	}
	if e.count("SELECT count(*) FROM sessions WHERE user_id = ?", u.id) != before {
		t.Fatal("сессия создана без кода")
	}
	// верный код — вход, а тот же код второй раз — нет
	res, err := e.loginCode(u, totpPassword, e.code(u))
	if err != nil {
		t.Fatalf("вход с кодом: %v", err)
	}
	if res.Token == "" || res.User.Login != "Vera" {
		t.Fatalf("итог входа: %+v", res)
	}
	if _, err := e.loginCode(u, totpPassword, e.code(u)); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("повтор кода: %v", err)
	}
	// следующий шаг подходит; код в виде «123 456» тоже
	e.clock.Advance(30 * time.Second)
	c := e.code(u)
	if _, err := e.loginCode(u, totpPassword, c[:3]+" "+c[3:]); err != nil {
		t.Fatalf("код с пробелом на следующем шаге: %v", err)
	}
}

func TestWrongCodesCountAsFailedLoginsAndPasswordWithoutCodeDoesNotResetThem(t *testing.T) {
	e := newEnv(t)
	u := e.enroll("Vera")
	e.clock.Advance(time.Minute)
	ci := e.freshIP() // один и тот же IP: лимит считается по IP и логину
	try := func(code string) error {
		_, err := e.svc.LoginWithCode(context.Background(), u.login, totpPassword, code, ci)
		return err
	}
	limits := config.DefaultLimits()
	// четыре неверных кода, между ними — «верный пароль без кода»: счётчик не сбрасывается
	for i := 0; i < limits.LoginAttempts-1; i++ {
		if err := try("000000"); !errors.Is(err, ErrTOTPInvalid) {
			t.Fatalf("неверный код %d: %v", i, err)
		}
		if err := try(""); !errors.Is(err, ErrTOTPRequired) {
			t.Fatalf("пароль без кода %d: %v", i, err)
		}
	}
	if err := try("111111"); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("пятый неверный код: %v", err)
	}
	// лимит исчерпан: даже верные пароль и код не пускают
	mustRateLimited(t, try(e.code(u)))
	// по истечении окна вход снова возможен
	e.clock.Advance(limits.LoginWindow + time.Minute)
	if err := try(e.code(u)); err != nil {
		t.Fatalf("вход после окна: %v", err)
	}
}

func TestRecoveryCodeWorksOnceAndIsLogged(t *testing.T) {
	e := newEnv(t)
	u := e.enroll("Vera")
	e.clock.Advance(time.Minute)
	ctx := context.Background()

	code := u.codes[3]
	if _, err := e.loginCode(u, totpPassword, strings.ToUpper(strings.ReplaceAll(code, "-", " "))); err != nil {
		t.Fatalf("вход по одноразовому коду: %v", err)
	}
	if _, err := e.loginCode(u, totpPassword, code); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("одноразовый код сработал дважды: %v", err)
	}
	if left, _ := e.svc.RecoveryCodesLeft(ctx, u.id); left != 9 {
		t.Errorf("осталось кодов %d", left)
	}
	// код, которого не выдавали, и чужой человек с тем же кодом
	if _, err := e.loginCode(u, totpPassword, "aaaaa-aaaaa"); !errors.Is(err, ErrTOTPInvalid) {
		t.Errorf("выдуманный код: %v", err)
	}
	other := e.enroll("Other")
	if _, err := e.loginCode(other, totpPassword, code); !errors.Is(err, ErrTOTPInvalid) {
		t.Errorf("одноразовый код другого человека: %v", err)
	}
	if rows := e.auditRows(audit.TOTPRecoveryUsed); len(rows) != 1 || rows[0].Target == nil || *rows[0].Target != "Vera" {
		t.Errorf("журнал: %+v", rows)
	}
}

func TestSameCodeCannotLogInTwiceEvenConcurrently(t *testing.T) {
	e := newEnv(t)
	u := e.enroll("Vera")
	e.clock.Advance(time.Minute)
	code := e.code(u)

	const n = 8
	var wg sync.WaitGroup
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		ci := e.freshIP()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.svc.LoginWithCode(context.Background(), u.login, totpPassword, code, ci)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
		} else if !errors.Is(err, ErrTOTPInvalid) {
			t.Errorf("неожиданная ошибка: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("один и тот же код впустил %d раз", ok)
	}

	// то же с одноразовым кодом
	rc := u.codes[0]
	results = make(chan error, n)
	for i := 0; i < n; i++ {
		ci := e.freshIP()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.svc.LoginWithCode(context.Background(), u.login, totpPassword, rc, ci)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok = 0
	for err := range results {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("один и тот же одноразовый код впустил %d раз", ok)
	}
}

func TestDisableAndRenewNeedPasswordAndCode(t *testing.T) {
	e := newEnv(t)
	u := e.enroll("Vera")
	e.clock.Advance(time.Minute)
	ctx := context.Background()
	ci := e.freshIP()

	// без пароля, без кода, с чужим кодом — нельзя
	if err := e.svc.TOTPDisable(ctx, u.id, "не тот пароль", e.code(u), ci); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("выключение с чужим паролем: %v", err)
	}
	if err := e.svc.TOTPDisable(ctx, u.id, totpPassword, "", ci); !errors.Is(err, ErrTOTPInvalid) {
		t.Errorf("выключение без кода: %v", err)
	}
	if err := e.svc.TOTPDisable(ctx, u.id, totpPassword, "000000", ci); !errors.Is(err, ErrTOTPInvalid) {
		t.Errorf("выключение с неверным кодом: %v", err)
	}
	if usr, _ := e.svc.Find(ctx, "Vera"); !usr.TOTPEnabled() {
		t.Fatal("защита выключилась без верных данных")
	}

	// новые коды: прежние перестают действовать
	fresh, err := e.svc.TOTPRenewRecoveryCodes(ctx, u.id, totpPassword, e.code(u), ci)
	if err != nil {
		t.Fatalf("новые коды: %v", err)
	}
	if len(fresh) != 10 || fresh[0] == u.codes[0] {
		t.Fatalf("новые коды: %v", fresh)
	}
	e.clock.Advance(30 * time.Second)
	if _, err := e.loginCode(u, totpPassword, u.codes[0]); !errors.Is(err, ErrTOTPInvalid) {
		t.Errorf("прежний одноразовый код действует: %v", err)
	}
	if _, err := e.loginCode(u, totpPassword, fresh[0]); err != nil {
		t.Errorf("новый одноразовый код: %v", err)
	}
	if rows := e.auditRows(audit.TOTPCodesRenewed); len(rows) != 1 {
		t.Errorf("журнал новых кодов: %+v", rows)
	}

	// выключить можно и одноразовым кодом (телефон потерян, пароль помнят)
	if err := e.svc.TOTPDisable(ctx, u.id, totpPassword, fresh[1], ci); err != nil {
		t.Fatalf("выключение: %v", err)
	}
	usr, _ := e.svc.Find(ctx, "Vera")
	if usr.TOTPEnabled() || usr.TOTPSecret != nil || usr.TOTPPending != nil || usr.TOTPLastStep != 0 {
		t.Errorf("остатки после выключения: %+v", usr)
	}
	if n := e.count("SELECT count(*) FROM totp_recovery_codes WHERE user_id = ?", u.id); n != 0 {
		t.Errorf("одноразовых кодов осталось %d", n)
	}
	if _, err := e.svc.Login(ctx, "Vera", totpPassword, e.freshIP()); err != nil {
		t.Errorf("вход одним паролем после выключения: %v", err)
	}
	if rows := e.auditRows(audit.TOTPDisabled); len(rows) != 1 {
		t.Errorf("журнал выключения: %+v", rows)
	}
	// выключенное выключить нельзя
	if err := e.svc.TOTPDisable(ctx, u.id, totpPassword, "123456", ci); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Errorf("повторное выключение: %v", err)
	}
	if _, err := e.svc.TOTPRenewRecoveryCodes(ctx, u.id, totpPassword, "123456", ci); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Errorf("новые коды без защиты: %v", err)
	}
}

func TestRestoreByBackupCodeDoesNotSkipTOTP(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	reg := e.register("Vera", totpPassword)
	setup, _ := e.svc.TOTPBegin(ctx, reg.User.ID, totpPassword, e.freshIP())
	secret, _ := b32.DecodeString(setup.Secret)
	if _, err := e.svc.TOTPEnable(ctx, reg.User.ID, 0, totpCode(secret, totpStep(e.clock.Now()))); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(time.Minute)
	u := &enrolled{login: "Vera", id: reg.User.ID, secret: secret}

	res, err := e.svc.RestoreAccess(ctx, "Vera", reg.BackupCode, "новый пароль 2026", e.freshIP())
	if err != nil {
		t.Fatalf("восстановление: %v", err)
	}
	if res.Token != "" || res.BackupCode == "" {
		t.Fatalf("итог: токен %q, резервный код %q — сессии быть не должно, код нужен", res.Token, res.BackupCode)
	}
	if n := e.count("SELECT count(*) FROM sessions WHERE user_id = ?", reg.User.ID); n != 0 {
		t.Errorf("после восстановления сессий %d", n)
	}
	if _, err := e.loginCode(u, "новый пароль 2026", ""); !errors.Is(err, ErrTOTPRequired) {
		t.Errorf("вход новым паролем без кода: %v", err)
	}
	if _, err := e.loginCode(u, "новый пароль 2026", e.code(u)); err != nil {
		t.Errorf("вход новым паролем с кодом: %v", err)
	}
}

func TestAdminResetTOTP(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	u := e.enroll("Vera")
	e.register("Plain", totpPassword)

	if err := e.svc.AdminResetTOTP(ctx, "Plain"); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Errorf("сброс без защиты: %v", err)
	}
	if err := e.svc.AdminResetTOTP(ctx, "Никто"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("сброс несуществующего: %v", err)
	}
	if err := e.svc.AdminResetTOTP(ctx, "vera"); err != nil { // логин без учёта регистра
		t.Fatalf("сброс: %v", err)
	}
	if _, err := e.svc.Login(ctx, "Vera", totpPassword, e.freshIP()); err != nil {
		t.Errorf("вход после сброса: %v", err)
	}
	if n := e.count("SELECT count(*) FROM totp_recovery_codes WHERE user_id = ?", u.id); n != 0 {
		t.Errorf("одноразовые коды остались: %d", n)
	}
	if rows := e.auditRows(audit.TOTPReset); len(rows) != 1 || rows[0].Actor != nil || rows[0].Target == nil || *rows[0].Target != "Vera" {
		t.Errorf("журнал: %+v", rows)
	}
}

func TestChangedServerSecretLeavesRecoveryCodesWorking(t *testing.T) {
	e := newEnv(t)
	u := e.enroll("Vera")
	e.clock.Advance(time.Minute)

	// тот же БД, но общий секрет сервера сменили: секрет TOTP не расшифровать
	hasher, _ := passwords.NewHasher(fastParams, 4)
	svc2, err := NewService(Options{DB: e.db, Hasher: hasher, Limiter: ratelimit.New(e.clock.Now), Limits: config.DefaultLimits(), Log: testutil.Logger(),
		SecretKey: []byte("новый общий секрет сервера после ротации, длинный"), Now: e.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := svc2.LoginWithCode(ctx, u.login, totpPassword, e.code(u), e.freshIP()); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("код при смене секрета сервера: %v", err)
	}
	if _, err := svc2.LoginWithCode(ctx, u.login, totpPassword, u.codes[0], e.freshIP()); err != nil {
		t.Fatalf("одноразовый код при смене секрета сервера: %v", err)
	}
}

func TestStoredSecretCannotBeMovedToAnotherUser(t *testing.T) {
	e := newEnv(t)
	victim := e.enroll("Vera")
	e.register("Mallory", totpPassword)
	// нарушитель с доступом к БД переносит зашифрованный секрет жертвы себе и включает «свою» защиту
	if err := e.db.Exec(`UPDATE users SET totp_secret = (SELECT totp_secret FROM users WHERE id = ?), totp_enabled_at = now() WHERE login = 'Mallory'`, victim.id).Error; err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(time.Minute)
	m := &enrolled{login: "Mallory", secret: victim.secret}
	if _, err := e.loginCode(m, totpPassword, totpCode(victim.secret, totpStep(e.clock.Now()))); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("перенесённый секрет сработал: %v", err)
	}
}

func TestTOTPColumnsStayConsistentAndDieWithAccount(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	u := e.enroll("Vera")
	// секрет без даты включения (и наоборот) база не примет
	e.register("Half", totpPassword)
	if err := e.db.Exec("UPDATE users SET totp_secret = '\\x01' WHERE login = 'Half'").Error; err == nil {
		t.Error("секрет без даты включения принят")
	}
	if err := e.db.Exec("UPDATE users SET totp_enabled_at = now() WHERE login = 'Half'").Error; err == nil {
		t.Error("дата включения без секрета принята")
	}
	// сдать дело: одноразовые коды уходят вместе с аккаунтом
	if err := e.svc.DeleteAccount(ctx, u.id, totpPassword, e.freshIP()); err != nil {
		t.Fatal(err)
	}
	if n := e.count("SELECT count(*) FROM totp_recovery_codes WHERE user_id = ?", u.id); n != 0 {
		t.Errorf("после удаления аккаунта осталось %d кодов", n)
	}
}

func TestRecoveryCodesLeftAndSetupFormat(t *testing.T) {
	e := newEnv(t)
	u := e.enroll("Vera")
	if left, err := e.svc.RecoveryCodesLeft(context.Background(), u.id); err != nil || left != 10 {
		t.Errorf("осталось %d %v", left, err)
	}
	if n, _ := e.svc.RecoveryCodesLeft(context.Background(), 999999); n != 0 {
		t.Errorf("у несуществующего %d", n)
	}
}

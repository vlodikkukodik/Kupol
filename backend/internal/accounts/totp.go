package accounts

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // TOTP по RFC 6238: SHA-1 — то, что понимают приложения-аутентификаторы; стойкость даёт HMAC
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Код из приложения (RFC 6238): HMAC-SHA1, 6 цифр, шаг 30 секунд — то, что понимают Google Authenticator, Aegis, 1Password и др.
// Всё здесь — чистые функции без БД; сценарии (подключить, войти, выключить) — в totp_service.go.

const (
	totpPeriod     = 30 // секунд в одном шаге
	totpDigits     = 6
	totpSecretLen  = 20 // байт (160 бит, как в RFC 4226)
	totpSkewSteps  = 1  // допускаются соседние шаги: часы телефона и сервера редко совпадают до секунды
	totpIssuer     = "КУПОЛ"
	recoveryCount  = 10
	recoveryGroups = 2
	recoverySize   = 5 // символов в группе: 10 знаков по 5 бит — 50 бит на код
)

// ErrTOTPRequired — пароль верен, но у пользователя включён код из приложения, а он не введён.
// Ошибка возвращается ПОСЛЕ проверки пароля, поэтому не позволяет узнать о включённой защите постороннему.
var ErrTOTPRequired = errors.New("accounts: нужен код из приложения")

// ErrTOTPInvalid — код из приложения (или одноразовый код) неверен, просрочен или уже использован.
var ErrTOTPInvalid = errors.New("accounts: неверный код")

// ErrTOTPAlreadyEnabled — код из приложения уже включён.
var ErrTOTPAlreadyEnabled = errors.New("accounts: код из приложения уже включён")

// ErrTOTPNotEnabled — код из приложения не включён (или подключение не начато).
var ErrTOTPNotEnabled = errors.New("accounts: код из приложения не включён")

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret — случайный секрет.
func newTOTPSecret() ([]byte, error) {
	s := make([]byte, totpSecretLen)
	if _, err := rand.Read(s); err != nil {
		return nil, err
	}
	return s, nil
}

// totpCode — код для шага step (число 30-секундных отрезков с 1970 года).
func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // шаг неотрицателен
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for i := 0; i < totpDigits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, bin%mod)
}

func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// canonicalTOTPCode оставляет в коде только цифры (приложения показывают «123 456»); не 6 цифр — не код.
func canonicalTOTPCode(input string) (string, bool) {
	var b strings.Builder
	for _, r := range input {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '\t':
		default:
			return "", false
		}
	}
	if b.Len() != totpDigits {
		return "", false
	}
	return b.String(), true
}

// verifyTOTP сверяет код с шагами вокруг now и возвращает шаг, по которому он подошёл. Шаг не больше lastStep не
// принимается: один и тот же код нельзя использовать дважды. Сверка не зависит от того, на каком знаке код разошёлся.
func verifyTOTP(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	code, ok := canonicalTOTPCode(code)
	if !ok {
		return 0, false
	}
	found := int64(-1)
	cur := totpStep(now)
	for d := int64(-totpSkewSteps); d <= totpSkewSteps; d++ {
		step := cur + d
		if step <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, step)), []byte(code)) == 1 && found < 0 {
			found = step
		}
	}
	return found, found >= 0
}

// totpURI — ссылка otpauth:// для приложения (её же кодирует QR): название и логин видны в приложении.
func totpURI(secret []byte, login string) string {
	label := url.PathEscape(totpIssuer + ":" + login)
	q := url.Values{}
	q.Set("secret", b32.EncodeToString(secret))
	q.Set("issuer", totpIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// ---------------------------------------------------------------- шифрование секрета

// secretBox шифрует секреты TOTP. Ключ выводится из общего секрета сервера (тот же, что подписывает запросы прокси),
// поэтому отдельного ключа заводить и хранить не нужно; цена — смена общего секрета делает сохранённые секреты
// нечитаемыми (пользователям придётся подключить код заново; администратор может снять его командой reset-totp).
type secretBox struct{ gcm cipher.AEAD }

func newSecretBox(serverSecret []byte) (*secretBox, error) {
	if len(serverSecret) < 32 {
		return nil, errors.New("секрет сервера короче 32 байт")
	}
	// выводимый ключ отделён от подписи прокси меткой назначения
	mac := hmac.New(sha256.New, serverSecret)
	mac.Write([]byte("kupol/totp-secret-box/v1"))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &secretBox{gcm: gcm}, nil
}

func userAAD(userID int64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(userID)) //nolint:gosec // номер положителен
	return b[:]
}

// seal шифрует секрет; шифртекст привязан к номеру пользователя: перенос строки другому пользователю не расшифруется.
func (b *secretBox) seal(userID int64, secret []byte) ([]byte, error) {
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.gcm.Seal(nonce, nonce, secret, userAAD(userID)), nil
}

func (b *secretBox) open(userID int64, sealed []byte) ([]byte, error) {
	n := b.gcm.NonceSize()
	if len(sealed) < n+b.gcm.Overhead() {
		return nil, errors.New("секрет TOTP повреждён")
	}
	return b.gcm.Open(nil, sealed[:n], sealed[n:], userAAD(userID))
}

// ---------------------------------------------------------------- одноразовые коды

// newRecoveryCodes — десять кодов вида «abcde-fghij» (алфавит резервного кода, строчные).
func newRecoveryCodes() ([]string, error) {
	out := make([]string, recoveryCount)
	for i := range out {
		raw := make([]byte, recoveryGroups*recoverySize)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		var b strings.Builder
		for j, v := range raw {
			if j > 0 && j%recoverySize == 0 {
				b.WriteByte('-')
			}
			b.WriteByte(strings.ToLower(backupAlphabet)[v&31])
		}
		out[i] = b.String()
	}
	return out, nil
}

// canonicalRecoveryCode приводит введённое к виду из 10 знаков алфавита или сообщает, что это не такой код.
func canonicalRecoveryCode(input string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.ToUpper(input) {
		switch {
		case r == ' ' || r == '-' || r == '_' || r == '\t':
		case r < 0x80:
			b.WriteRune(r)
		default:
			return "", false
		}
	}
	s := b.String()
	if len(s) != recoveryGroups*recoverySize {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(backupAlphabet, s[i]) < 0 {
			return "", false
		}
	}
	return s, true
}

func recoveryHash(canonical string) []byte {
	h := sha256.Sum256([]byte("kupol/totp-recovery/v1:" + canonical))
	return h[:]
}

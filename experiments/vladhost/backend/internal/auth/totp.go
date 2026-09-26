package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // TOTP по RFC 6238 с SHA-1 — то, что понимают все приложения-аутентификаторы
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vladhost/internal/apperr"
)

// Параметры TOTP: стандартные (30 секунд, 6 цифр, SHA-1) — их понимают Google Authenticator, Aegis, 1Password и другие.
const (
	totpPeriod    = 30
	totpDigits    = 6
	totpSkew      = 1 // принимаем соседний шаг: часы телефона могут уйти на полминуты
	recoveryCount = 10
	ticketTTL     = 5 * time.Minute
	ticketTries   = 5
	totpIssuer    = "Vladhost"
)

var (
	ErrTwoFactorCode     = apperr.New(http.StatusUnprocessableEntity, "two_factor_code", "wrong two-factor code").OnField("code")
	ErrTwoFactorTicket   = apperr.New(http.StatusUnauthorized, "two_factor_ticket", "two-factor login expired, sign in again")
	ErrTwoFactorEnabled  = apperr.New(http.StatusConflict, "two_factor_enabled", "two-factor login is already enabled")
	ErrTwoFactorDisabled = apperr.New(http.StatusConflict, "two_factor_disabled", "two-factor login is not enabled")
	ErrTwoFactorNoSetup  = apperr.New(http.StatusConflict, "two_factor_no_setup", "start two-factor setup first")
	ErrPasswordRequired  = apperr.New(http.StatusUnprocessableEntity, "wrong_password", "password is wrong").OnField("password")
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// totpCode — код для шага времени step (RFC 4226, динамическое усечение).
func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // шаг времени неотрицательный
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, v%1_000_000)
}

// TOTPCode — код для ключа (base32, как его показывают пользователю) в момент t. Нужен тестам и проверкам вне пакета.
func TOTPCode(secret string, t time.Time) (string, error) {
	raw, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", err
	}
	return totpCode(raw, t.Unix()/totpPeriod), nil
}

// matchTOTP ищет шаг, на котором код верен, среди соседних и более поздних, чем last (повтор уже принятого кода отклоняется).
func matchTOTP(secret []byte, code string, now time.Time, last int64) (int64, bool) {
	cur := now.Unix() / totpPeriod
	for d := -totpSkew; d <= totpSkew; d++ {
		step := cur + int64(d)
		if step <= last {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// normalizeCode убирает пробелы и дефисы: код копируют как «123 456», коды восстановления — с дефисом.
func normalizeCode(code string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(strings.TrimSpace(code)))
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// ---- шифрование секрета: утечка дампа базы не должна давать коды ----

func (s *Service) totpKey() []byte {
	sum := sha256.Sum256(append([]byte("vladhost-totp:"), s.secret...))
	return sum[:]
}

func (s *Service) seal(plain []byte) (string, error) {
	block, err := aes.NewCipher(s.totpKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, plain, nil)), nil
}

func (s *Service) open(sealed string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(s.totpKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("totp: ciphertext too short")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
}

// ---- настройка ----

// TwoFactorStatus — что показать в настройках.
type TwoFactorStatus struct {
	Enabled      bool       `json:"enabled"`
	EnabledAt    *time.Time `json:"enabled_at"`
	RecoveryLeft int64      `json:"recovery_left"`
}

func (s *Service) TwoFactorStatus(ctx context.Context, userID int64) (*TwoFactorStatus, error) {
	u, err := s.UserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	st := &TwoFactorStatus{Enabled: u.TOTPEnabledAt != nil, EnabledAt: u.TOTPEnabledAt}
	if st.Enabled {
		if err := s.db.WithContext(ctx).Model(&RecoveryCode{}).Where("user_id = ? AND used_at IS NULL", userID).Count(&st.RecoveryLeft).Error; err != nil {
			return nil, err
		}
	}
	return st, nil
}

// TwoFactorSetup — новый секрет для приложения: строкой и ссылкой otpauth:// (её показывают QR-кодом).
type TwoFactorSetup struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

// checkPassword сверяет пароль: включение и выключение второго фактора требуют его, чтобы украденная вкладка не могла сменить защиту.
func (s *Service) checkPassword(ctx context.Context, userID int64, password string) (*User, error) {
	u, err := s.UserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, ErrPasswordRequired
	}
	return u, nil
}

// StartTwoFactor создаёт секрет и запоминает его как ожидающий: включится он только после ввода верного кода.
func (s *Service) StartTwoFactor(ctx context.Context, userID int64, password string) (*TwoFactorSetup, error) {
	u, err := s.checkPassword(ctx, userID, password)
	if err != nil {
		return nil, err
	}
	if u.TOTPEnabledAt != nil {
		return nil, ErrTwoFactorEnabled
	}
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	sealed, err := s.seal(secret)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(u).Update("totp_pending", sealed).Error; err != nil {
		return nil, err
	}
	enc := b32.EncodeToString(secret)
	label := url.PathEscape(totpIssuer + ":" + u.Username)
	q := url.Values{"secret": {enc}, "issuer": {totpIssuer}, "algorithm": {"SHA1"}, "digits": {fmt.Sprint(totpDigits)}, "period": {fmt.Sprint(totpPeriod)}}
	return &TwoFactorSetup{Secret: enc, URI: "otpauth://totp/" + label + "?" + q.Encode()}, nil
}

// EnableTwoFactor проверяет код из приложения и включает второй фактор; возвращает коды восстановления (показываются один раз).
func (s *Service) EnableTwoFactor(ctx context.Context, userID int64, code string) ([]string, error) {
	var codes []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var u User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, userID).Error; err != nil {
			return err
		}
		if u.TOTPEnabledAt != nil {
			return ErrTwoFactorEnabled
		}
		if u.TOTPPending == nil {
			return ErrTwoFactorNoSetup
		}
		secret, err := s.open(*u.TOTPPending)
		if err != nil {
			return err
		}
		step, ok := matchTOTP(secret, normalizeCode(code), s.now(), 0)
		if !ok {
			return ErrTwoFactorCode
		}
		now := s.now()
		if err := tx.Model(&u).Updates(map[string]any{
			"totp_secret": *u.TOTPPending, "totp_pending": nil, "totp_enabled_at": now, "totp_last_step": step,
		}).Error; err != nil {
			return err
		}
		codes, err = s.newRecoveryCodes(tx, userID)
		return err
	})
	return codes, err
}

// DisableTwoFactor выключает второй фактор: нужен пароль и действующий код (или код восстановления).
func (s *Service) DisableTwoFactor(ctx context.Context, userID int64, password, code string) error {
	if _, err := s.checkPassword(ctx, userID, password); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var u User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, userID).Error; err != nil {
			return err
		}
		if u.TOTPEnabledAt == nil {
			return ErrTwoFactorDisabled
		}
		if _, err := s.verifySecond(tx, &u, code); err != nil {
			return err
		}
		return s.clearTwoFactor(tx, userID)
	})
}

func (s *Service) clearTwoFactor(tx *gorm.DB, userID int64) error {
	if err := tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"totp_secret": nil, "totp_pending": nil, "totp_enabled_at": nil, "totp_last_step": 0,
	}).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ?", userID).Delete(&RecoveryCode{}).Error
}

// ResetTwoFactor снимает второй фактор без кода — для администратора (команда vladhost admin reset-2fa), когда пользователь потерял
// и телефон, и коды восстановления. Все сессии закрываются.
func (s *Service) ResetTwoFactor(ctx context.Context, login string) (*User, error) {
	var u User
	if err := s.db.WithContext(ctx).Where("email = ? OR username = ?", lower(login), lower(login)).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.clearTwoFactor(tx, u.ID); err != nil {
			return err
		}
		return s.revokeSessions(tx, u.ID, 0, 0)
	})
	return &u, err
}

// RegenerateRecoveryCodes выдаёт новый набор кодов восстановления (старые перестают действовать).
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, userID int64, password string) ([]string, error) {
	u, err := s.checkPassword(ctx, userID, password)
	if err != nil {
		return nil, err
	}
	if u.TOTPEnabledAt == nil {
		return nil, ErrTwoFactorDisabled
	}
	var codes []string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		codes, err = s.newRecoveryCodes(tx, userID)
		return err
	})
	return codes, err
}

// RecoveryCode — одноразовый код восстановления (хранится только хеш).
type RecoveryCode struct {
	ID        int64 `gorm:"primaryKey"`
	UserID    int64
	CodeHash  string
	UsedAt    *time.Time
	CreatedAt time.Time
}

// newRecoveryCodes заменяет коды пользователя новыми: 10 кодов вида «abcde-fghij» (50 бит случайности каждый).
func (s *Service) newRecoveryCodes(tx *gorm.DB, userID int64) ([]string, error) {
	if err := tx.Where("user_id = ?", userID).Delete(&RecoveryCode{}).Error; err != nil {
		return nil, err
	}
	enc := base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)
	codes := make([]string, 0, recoveryCount)
	rows := make([]RecoveryCode, 0, recoveryCount)
	for range recoveryCount {
		b := make([]byte, 7)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		c := enc.EncodeToString(b)[:10]
		codes = append(codes, c[:5]+"-"+c[5:])
		rows = append(rows, RecoveryCode{UserID: userID, CodeHash: hashToken(c)})
	}
	return codes, tx.Create(&rows).Error
}

// verifySecond проверяет второй фактор: шестизначный код приложения или код восстановления (он гасится).
// Возвращает, был ли использован код восстановления.
func (s *Service) verifySecond(tx *gorm.DB, u *User, code string) (recovery bool, err error) {
	c := normalizeCode(code)
	if isDigits(c) && len(c) == totpDigits {
		if u.TOTPSecret == nil {
			return false, ErrTwoFactorCode
		}
		secret, err := s.open(*u.TOTPSecret)
		if err != nil {
			return false, err
		}
		step, ok := matchTOTP(secret, c, s.now(), u.TOTPLastStep)
		if !ok {
			return false, ErrTwoFactorCode
		}
		return false, tx.Model(u).Update("totp_last_step", step).Error
	}
	if len(c) != 10 {
		return false, ErrTwoFactorCode
	}
	res := tx.Model(&RecoveryCode{}).Where("user_id = ? AND code_hash = ? AND used_at IS NULL", u.ID, hashToken(c)).Update("used_at", s.now())
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, ErrTwoFactorCode
	}
	return true, nil
}

// ---- вход со вторым фактором ----

// ticketStore — билеты второго шага входа: пароль уже проверен, осталось ввести код. Живут 5 минут, дают 5 попыток.
type ticketStore struct {
	mu sync.Mutex
	m  map[string]*ticket
}

type ticket struct {
	userID  int64
	expires time.Time
	tries   int
}

func (t *ticketStore) put(userID int64, now time.Time) (string, error) {
	raw, err := randomToken(24)
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.m == nil {
		t.m = map[string]*ticket{}
	}
	for k, v := range t.m {
		if !v.expires.After(now) {
			delete(t.m, k)
		}
	}
	t.m[hashToken(raw)] = &ticket{userID: userID, expires: now.Add(ticketTTL)}
	return raw, nil
}

// take отдаёт пользователя билета и тратит одну попытку; исчерпанный или просроченный билет удаляется.
func (t *ticketStore) take(raw string, now time.Time) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := hashToken(raw)
	v, ok := t.m[k]
	if !ok || !v.expires.After(now) {
		delete(t.m, k)
		return 0, false
	}
	v.tries++
	if v.tries >= ticketTries {
		delete(t.m, k)
	}
	return v.userID, true
}

func (t *ticketStore) drop(raw string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, hashToken(raw))
}

// TicketUser — чей это билет (для журнала неудачных попыток); 0 — билет неизвестен. Попытку не тратит.
func (s *Service) TicketUser(raw string) int64 {
	s.tickets.mu.Lock()
	defer s.tickets.mu.Unlock()
	if v, ok := s.tickets.m[hashToken(raw)]; ok {
		return v.userID
	}
	return 0
}

// LoginSecondFactor завершает вход: билет из первого шага и код. recovery — вошли кодом восстановления.
func (s *Service) LoginSecondFactor(ctx context.Context, rawTicket, code string) (sess *Session, recovery bool, err error) {
	userID, ok := s.tickets.take(rawTicket, s.now())
	if !ok {
		return nil, false, ErrTwoFactorTicket
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var u User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, userID).Error; err != nil {
			return ErrTwoFactorTicket
		}
		if u.TOTPEnabledAt == nil { // второй фактор сняли, пока вводили код: пароль уже проверен
			sess, err = s.issue(ctx, tx, u, 0)
			return err
		}
		if recovery, err = s.verifySecond(tx, &u, code); err != nil {
			return err
		}
		sess, err = s.issue(ctx, tx, u, 0)
		return err
	})
	if err == nil {
		s.tickets.drop(rawTicket)
	}
	return sess, recovery, err
}

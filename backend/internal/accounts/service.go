package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"

	"kupol/internal/achievements"
	"kupol/internal/audit"
	"kupol/internal/config"
	"kupol/internal/i18n"
	"kupol/internal/inbox"
	"kupol/internal/mail"
	"kupol/internal/passwords"
	"kupol/internal/ratelimit"
	"kupol/internal/xp"
)

const (
	// SessionSlidingTTL — срок сессии; продлевается при активности.
	SessionSlidingTTL = 30 * 24 * time.Hour
	// SessionAbsoluteTTL — предел жизни сессии, сколько бы ни продлевали.
	SessionAbsoluteTTL = 90 * 24 * time.Hour

	sessionTouchEvery = 5 * time.Minute
	captchaTTL        = 10 * time.Minute
	tokenBytes        = 32
	tokenLen          = 43 // base64url без паддинга от 32 байт
	maxUserAgentLen   = 255
	registerWindow    = time.Hour
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ClientInfo — что известно о клиенте запроса: его IP (подтверждённый прокси или из соединения) и браузер.
type ClientInfo struct {
	IP        string
	UserAgent string
}

// AuthResult — итог входа/регистрации: пользователь и новая сессия.
type AuthResult struct {
	User      User
	Token     string // значение куки; в БД хранится только его хеш
	ExpiresAt time.Time
	// LevelUp — этот вход поднял уровень (XP перешёл порог 2 или 3); повод показать штамп «ДОПУСК ПОВЫШЕН».
	LevelUp bool
	// NewAchievements — грамоты, выданные этим входом (шаг 5.6): серия 7/30 дней.
	NewAchievements []achievements.Kind
}

// RegisterResult дополнительно содержит резервный код — он показывается только один раз.
type RegisterResult struct {
	AuthResult
	BackupCode string
}

// RegisterInput — поля формы регистрации.
type RegisterInput struct {
	Login         string
	Password      string
	CaptchaID     string
	CaptchaAnswer string
}

// Authenticated — пользователь и сессия по токену из куки.
type Authenticated struct {
	User    User
	Session Session
	// Renewed — срок сессии продлён; куку надо выставить заново с новым сроком.
	Renewed bool
}

// Captcha — вопрос анкеты для формы регистрации.
type Captcha struct {
	ID       string
	Question string
}

// Options — зависимости сервиса.
type Options struct {
	DB      *gorm.DB
	Hasher  *passwords.Hasher
	Limiter *ratelimit.Limiter
	Limits  config.Limits
	Log     *slog.Logger
	// SecretKey — общий секрет сервера (не короче 32 байт): из него выводится ключ, которым шифруются секреты кода из приложения.
	SecretKey []byte
	// Now — часы; nil — настоящее время (подмена нужна только тестам).
	Now func() time.Time
	// Mailer — отправка писем (шаг 5.1.1); nil — почта выключена (SMTP не настроен), письма просто не шлются.
	Mailer mail.Sender
	// SiteOrigin — для ссылки подтверждения почты (origin + "/email-confirm?token=…"); пусто, если Mailer тоже пуст.
	SiteOrigin string
}

// Service — вся логика аккаунтов. Безопасен для одновременного использования.
type Service struct {
	db         *gorm.DB
	hasher     *passwords.Hasher
	limiter    *ratelimit.Limiter
	limits     config.Limits
	log        *slog.Logger
	box        *secretBox
	now        func() time.Time
	mailer     mail.Sender
	siteOrigin string
}

func NewService(o Options) (*Service, error) {
	switch {
	case o.DB == nil:
		return nil, errors.New("accounts: не задана БД")
	case o.Hasher == nil:
		return nil, errors.New("accounts: не задан хешер паролей")
	case o.Limiter == nil:
		return nil, errors.New("accounts: не задан ограничитель частоты")
	case o.Log == nil:
		return nil, errors.New("accounts: не задан логгер")
	}
	l := o.Limits
	if l.LoginAttempts < 1 || l.LoginLockAttempts < 1 || l.LoginWindow <= 0 || l.RegisterPerHour < 1 {
		return nil, fmt.Errorf("accounts: недопустимые лимиты %+v", l)
	}
	now := o.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	box, err := newSecretBox(o.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("accounts: %w", err)
	}
	return &Service{db: o.DB, hasher: o.Hasher, limiter: o.Limiter, limits: l, log: o.Log, box: box, now: now, mailer: o.Mailer, siteOrigin: o.SiteOrigin}, nil
}

// sendMail отправляет письмо в фоне (не блокирует запрос, который его вызвал) с собственным таймаутом, не зависящим
// от контекста запроса — тот завершается сразу после ответа клиенту. Молчит, если почта не настроена (mailer == nil).
func (s *Service) sendMail(msg mail.Message) {
	if s.mailer == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.mailer.Send(ctx, msg); err != nil {
			s.log.Warn("почта: не удалось отправить", "to", msg.To, "err", err)
		}
	}()
}

// ---------------------------------------------------------------- лимиты входа

func loginIPKey(ip, login string) string { return "login:" + ip + "|" + LoginKey(login) }
func loginLockKey(login string) string   { return "loginlock:" + LoginKey(login) }

// checkLoginLimits отказывает, если по этой паре IP+логин или по самому логину слишком много неудач.
func (s *Service) checkLoginLimits(ip, login string) error {
	var retry time.Duration
	blocked := false
	if b, r := s.limiter.Blocked(loginIPKey(ip, login), s.limits.LoginAttempts, s.limits.LoginWindow); b {
		blocked, retry = true, r
	}
	if b, r := s.limiter.Blocked(loginLockKey(login), s.limits.LoginLockAttempts, s.limits.LoginWindow); b {
		blocked = true
		retry = max(retry, r)
	}
	if !blocked {
		return nil
	}
	s.log.Warn("вход заблокирован по числу неудач", "ip", ip, "login", LoginKey(login), "retry_after", retry.Round(time.Second))
	return &RateLimitedError{RetryAfter: retry}
}

func (s *Service) recordLoginFailure(ip, login string) {
	s.limiter.Hit(loginIPKey(ip, login), s.limits.LoginWindow)
	s.limiter.Hit(loginLockKey(login), s.limits.LoginWindow)
}

// recordLoginSuccess сбрасывает счётчик пары IP+логин. Счётчик по одному логину не сбрасывается:
// иначе владелец, войдя посреди перебора, «обнулял» бы попытки злоумышленника.
func (s *Service) recordLoginSuccess(ip, login string) {
	s.limiter.Reset(loginIPKey(ip, login))
}

// ---------------------------------------------------------------- токены и сессии

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func newToken() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) createSession(tx *gorm.DB, userID int64, ci ClientInfo) (string, time.Time, error) {
	token, err := newToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.now()
	sess := Session{
		TokenHash:         hashToken(token),
		UserID:            userID,
		CreatedAt:         now,
		LastSeenAt:        now,
		ExpiresAt:         now.Add(SessionSlidingTTL),
		AbsoluteExpiresAt: now.Add(SessionAbsoluteTTL),
		UserAgent:         truncateRunes(ci.UserAgent, maxUserAgentLen),
	}
	if ip := net.ParseIP(ci.IP); ip != nil {
		v := ip.String()
		sess.IP = &v
	}
	if err := tx.Create(&sess).Error; err != nil {
		return "", time.Time{}, err
	}
	return token, sess.ExpiresAt, nil
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// Authenticate находит пользователя по токену из куки и при необходимости продлевает сессию.
func (s *Service) Authenticate(ctx context.Context, token string) (*Authenticated, error) {
	if len(token) != tokenLen {
		return nil, ErrNoSession
	}
	now := s.now()
	db := s.db.WithContext(ctx)

	var sess Session
	err := db.Where("token_hash = ? AND expires_at > ? AND absolute_expires_at > ?", hashToken(token), now, now).Take(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	var user User
	if err := db.Take(&user, sess.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoSession
		}
		return nil, err
	}
	if err := s.loadRoles(db, &user); err != nil {
		return nil, err
	}

	renewed := false
	if now.Sub(sess.LastSeenAt) >= sessionTouchEvery {
		exp := now.Add(SessionSlidingTTL)
		if exp.After(sess.AbsoluteExpiresAt) {
			exp = sess.AbsoluteExpiresAt
		}
		if err := db.Model(&Session{}).Where("id = ?", sess.ID).Updates(map[string]any{"last_seen_at": now, "expires_at": exp}).Error; err != nil {
			return nil, err
		}
		sess.LastSeenAt, sess.ExpiresAt = now, exp
		renewed = true
	}
	return &Authenticated{User: user, Session: sess, Renewed: renewed}, nil
}

// Logout завершает сессию по токену. Несуществующая сессия — не ошибка.
func (s *Service) Logout(ctx context.Context, token string) error {
	if len(token) != tokenLen {
		return nil
	}
	return s.db.WithContext(ctx).Where("token_hash = ?", hashToken(token)).Delete(&Session{}).Error
}

// RevokeSessions мгновенно завершает все сессии пользователя, кроме exceptSessionID (0 — завершить все).
func (s *Service) RevokeSessions(ctx context.Context, userID, exceptSessionID int64) error {
	return s.revokeSessions(s.db.WithContext(ctx), userID, exceptSessionID)
}

func (s *Service) revokeSessions(tx *gorm.DB, userID, exceptSessionID int64) error {
	q := tx.Where("user_id = ?", userID)
	if exceptSessionID != 0 {
		q = q.Where("id <> ?", exceptSessionID)
	}
	return q.Delete(&Session{}).Error
}

// ---------------------------------------------------------------- анкета (капча)

// NewCaptcha выдаёт случайный вопрос анкеты; ответ принимается один раз в течение captchaTTL.
func (s *Service) NewCaptcha(ctx context.Context) (*Captcha, error) {
	q, err := randomQuestion()
	if err != nil {
		return nil, err
	}
	var id string
	err = s.db.WithContext(ctx).Raw(
		"INSERT INTO captcha_challenges (question_id, expires_at) VALUES (?, ?) RETURNING id",
		q.ID, s.now().Add(captchaTTL),
	).Scan(&id).Error
	if err != nil {
		return nil, err
	}
	return &Captcha{ID: id, Question: q.text(i18n.From(ctx))}, nil
}

// consumeCaptcha проверяет ответ и в любом случае гасит вопрос: подобрать ответ повторными
// попытками на одном и том же вопросе нельзя.
func (s *Service) consumeCaptcha(ctx context.Context, id, answer string) (bool, error) {
	if !uuidRe.MatchString(id) {
		return false, nil
	}
	var questionID string
	res := s.db.WithContext(ctx).Raw(
		"DELETE FROM captcha_challenges WHERE id = ? AND expires_at > ? RETURNING question_id",
		id, s.now(),
	).Scan(&questionID)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	q, ok := questionByID(questionID)
	return ok && q.accepts(answer), nil
}

// ---------------------------------------------------------------- регистрация

// Register создаёт аккаунт и сразу входит в него. Резервный код возвращается один раз.
func (s *Service) Register(ctx context.Context, in RegisterInput, ci ClientInfo) (*RegisterResult, error) {
	fields := map[string]string{}
	login, err := NormalizeLogin(in.Login)
	if err != nil {
		fields["login"] = err.Error()
	}
	if err := ValidatePassword(in.Password, login); err != nil {
		fields["password"] = err.Error()
	}
	if strings.TrimSpace(in.CaptchaAnswer) == "" {
		fields["captcha_answer"] = "Ответьте на вопрос анкеты"
	}
	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}

	ok, err := s.consumeCaptcha(ctx, in.CaptchaID, in.CaptchaAnswer)
	if err != nil {
		return nil, err
	}
	if !ok {
		s.log.Warn("регистрация: неверный ответ на анкету", "ip", ci.IP)
		return nil, ErrCaptcha
	}

	// Лимит считается только для попыток, дошедших до создания аккаунта: опечатка в форме его не расходует.
	if allowed, retry := s.limiter.Allow("register:"+ci.IP, s.limits.RegisterPerHour, registerWindow); !allowed {
		s.log.Warn("регистрация: превышен лимит с IP", "ip", ci.IP, "retry_after", retry.Round(time.Second))
		return nil, &RateLimitedError{RetryAfter: retry}
	}

	passHash, err := s.hasher.Hash(ctx, preparePassword(in.Password))
	if err != nil {
		return nil, err
	}
	code, err := GenerateBackupCode()
	if err != nil {
		return nil, err
	}
	canon, _ := CanonicalBackupCode(code)
	codeHash, err := s.hasher.Hash(ctx, canon)
	if err != nil {
		return nil, err
	}

	now := s.now()
	user := User{
		Login:             login,
		PasswordHash:      passHash,
		BackupCodeHash:    codeHash,
		Level:             LevelVisitor,
		CreatedAt:         now,
		LastLoginAt:       &now,
		PasswordChangedAt: now,
	}
	var token string
	var expires time.Time
	var xpRes xp.LoginResult
	var newAchievements []achievements.Kind
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		var xerr error
		xpRes, xerr = xp.AwardLogin(ctx, tx, user.ID, now)
		if xerr != nil {
			return xerr
		}
		var aerr error
		newAchievements, aerr = achievements.CheckStreak(ctx, tx, user.ID, xpRes.Streak, now)
		if aerr != nil {
			return aerr
		}
		var serr error
		token, expires, serr = s.createSession(tx, user.ID, ci)
		return serr
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, ErrLoginTaken
	}
	if err != nil {
		return nil, err
	}
	user.XP, user.LoginStreak = xpRes.XP, xpRes.Streak
	s.log.Info("зарегистрирован пользователь", "user_id", user.ID, "ip", ci.IP)
	return &RegisterResult{AuthResult: AuthResult{User: user, Token: token, ExpiresAt: expires, NewAchievements: newAchievements}, BackupCode: code}, nil
}

// ---------------------------------------------------------------- вход

// Find возвращает пользователя по логину (без учёта регистра) или ErrUserNotFound.
func (s *Service) Find(ctx context.Context, login string) (*User, error) {
	return s.findByLogin(ctx, login)
}

func (s *Service) findByLogin(ctx context.Context, login string) (*User, error) {
	var u User
	err := s.db.WithContext(ctx).Where("login = ?", norm.NFC.String(strings.TrimSpace(login))).Take(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	return &u, err
}

// Login — вход по логину и паролю. Если у пользователя включён код из приложения, возвращает ErrTOTPRequired.
func (s *Service) Login(ctx context.Context, login, password string, ci ClientInfo) (*AuthResult, error) {
	return s.LoginWithCode(ctx, login, password, "", ci)
}

// LoginWithCode проверяет логин и пароль (и код из приложения или одноразовый код, если он включён) и создаёт сессию.
//
// Неверный логин и неверный пароль неразличимы ни по ответу, ни по времени (для несуществующего
// логина считается «холостой» хеш). После LoginAttempts неудач с одного IP на один логин
// (или LoginLockAttempts с любых IP) вход блокируется на окно, даже с верным паролем.
//
// Код из приложения спрашивается только после верного пароля: ErrTOTPRequired не раскрывает постороннему, включена ли
// защита. Верный пароль без кода счётчики неудач не сбрасывает (иначе код можно было бы перебирать «бесплатно»), неверный
// код считается неудачей входа наравне с неверным паролем.
func (s *Service) LoginWithCode(ctx context.Context, login, password, code string, ci ClientInfo) (*AuthResult, error) {
	fields := map[string]string{}
	if strings.TrimSpace(login) == "" {
		fields["login"] = "Введите логин"
	}
	if password == "" {
		fields["password"] = "Введите пароль"
	}
	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}
	if err := s.checkLoginLimits(ci.IP, login); err != nil {
		return nil, err
	}

	user, err := s.findByLogin(ctx, login)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, err
	}
	pw := preparePassword(password)
	if user == nil {
		if err := s.hasher.VerifyDummy(ctx, pw); err != nil {
			return nil, err
		}
		s.recordLoginFailure(ci.IP, login)
		s.log.Warn("вход: неверные данные", "ip", ci.IP, "login", LoginKey(login))
		return nil, ErrInvalidCredentials
	}

	ok, rehash, err := s.hasher.Verify(ctx, pw, user.PasswordHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		s.recordLoginFailure(ci.IP, login)
		s.log.Warn("вход: неверные данные", "ip", ci.IP, "login", LoginKey(login))
		return nil, ErrInvalidCredentials
	}
	if user.BannedAt != nil {
		return nil, ErrBanned
	}
	if user.TOTPEnabled() {
		if strings.TrimSpace(code) == "" {
			return nil, ErrTOTPRequired
		}
		kind, good, err := s.verifySecondFactor(ctx, user, code)
		if err != nil {
			return nil, err
		}
		if !good {
			s.recordLoginFailure(ci.IP, login)
			s.log.Warn("вход: неверный код из приложения", "ip", ci.IP, "login", LoginKey(login))
			return nil, ErrTOTPInvalid
		}
		if kind == "recovery" {
			s.log.Warn("вход по одноразовому коду", "user_id", user.ID, "ip", ci.IP)
		}
	}
	s.recordLoginSuccess(ci.IP, login)

	now := s.now()
	updates := map[string]any{"last_login_at": now}
	if rehash {
		if h, err := s.hasher.Hash(ctx, pw); err == nil {
			updates["password_hash"] = h
			user.PasswordHash = h
		} else {
			s.log.Warn("не удалось обновить хеш пароля", "user_id", user.ID, "err", err)
		}
	}
	var token string
	var expires time.Time
	var xpRes xp.LoginResult
	var newAchievements []achievements.Kind
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", user.ID).Updates(updates).Error; err != nil {
			return err
		}
		var xerr error
		xpRes, xerr = xp.AwardLogin(ctx, tx, user.ID, now)
		if xerr != nil {
			return xerr
		}
		if xpRes.Promoted {
			if err := audit.Record(tx, now, audit.LevelPromoted, audit.Event{
				TargetUserID: &user.ID, Details: audit.Details("level", xpRes.Level, "xp", xpRes.XP),
			}); err != nil {
				return err
			}
		}
		if xpRes.Promoted {
			if err := inbox.Send(ctx, tx, user.ID, inbox.KindLevelUp, map[string]any{"level": xpRes.Level}, now); err != nil {
				return err
			}
		}
		var aerr error
		newAchievements, aerr = achievements.CheckStreak(ctx, tx, user.ID, xpRes.Streak, now)
		if aerr != nil {
			return aerr
		}
		var serr error
		token, expires, serr = s.createSession(tx, user.ID, ci)
		return serr
	})
	if err != nil {
		return nil, err
	}
	user.LastLoginAt = &now
	user.XP, user.LoginStreak, user.Level = xpRes.XP, xpRes.Streak, xpRes.Level
	if err := s.loadRoles(s.db.WithContext(ctx), user); err != nil {
		return nil, err
	}
	if xpRes.Promoted {
		s.log.Info("уровень повышен по XP", "user_id", user.ID, "level", xpRes.Level, "xp", xpRes.XP)
		s.notifyLevelUp(ctx, user)
	}
	s.log.Info("вход выполнен", "user_id", user.ID, "ip", ci.IP)
	return &AuthResult{User: *user, Token: token, ExpiresAt: expires, LevelUp: xpRes.Promoted, NewAchievements: newAchievements}, nil
}

// notifyLevelUp шлёт письмо о повышении уровня, если у пользователя есть подтверждённая почта.
func (s *Service) notifyLevelUp(ctx context.Context, user *User) {
	if user.Email == nil {
		return
	}
	l := i18n.From(ctx)
	msg := mail.LevelUp(l, user.Login, user.LevelNameIn(l), user.Level)
	msg.To = *user.Email
	s.sendMail(msg)
}

// ---------------------------------------------------------------- действия вошедшего

// verifyOwnPassword подтверждает пароль уже вошедшего пользователя. Попытки считаются теми же
// счётчиками, что и вход: украденную сессию нельзя использовать для перебора пароля.
func (s *Service) verifyOwnPassword(ctx context.Context, user *User, password string, ci ClientInfo) error {
	if err := s.checkLoginLimits(ci.IP, user.Login); err != nil {
		return err
	}
	ok, _, err := s.hasher.Verify(ctx, preparePassword(password), user.PasswordHash)
	if err != nil {
		return err
	}
	if !ok {
		s.recordLoginFailure(ci.IP, user.Login)
		s.log.Warn("неверный текущий пароль", "user_id", user.ID, "ip", ci.IP)
		return ErrWrongPassword
	}
	s.recordLoginSuccess(ci.IP, user.Login)
	return nil
}

// ChangePassword меняет пароль и завершает все остальные сессии пользователя (текущая остаётся).
func (s *Service) ChangePassword(ctx context.Context, userID, currentSessionID int64, current, next string, ci ClientInfo) error {
	var user User
	if err := s.db.WithContext(ctx).Take(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNoSession
		}
		return err
	}
	if err := ValidatePassword(next, user.Login); err != nil {
		return fieldError("new_password", err.Error())
	}
	if preparePassword(next) == preparePassword(current) {
		return fieldError("new_password", "Новый пароль совпадает с текущим")
	}
	if err := s.verifyOwnPassword(ctx, &user, current, ci); err != nil {
		return err
	}

	hash, err := s.hasher.Hash(ctx, preparePassword(next))
	if err != nil {
		return err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", user.ID).
			Updates(map[string]any{"password_hash": hash, "password_changed_at": s.now()}).Error; err != nil {
			return err
		}
		if err := audit.Record(tx, s.now(), audit.PasswordChanged, audit.Event{ActorID: &user.ID, TargetUserID: &user.ID}); err != nil {
			return err
		}
		return s.revokeSessions(tx, user.ID, currentSessionID)
	})
	if err != nil {
		return err
	}
	s.log.Info("пароль изменён", "user_id", user.ID, "ip", ci.IP)
	return nil
}

// ---------------------------------------------------------------- почта (шаг 5.1.1, по желанию)

const (
	emailConfirmTTL      = 24 * time.Hour
	emailChangesPerHour  = 3
	emailConfirmsPerHour = 10 // переходов по ссылке подтверждения (защита от перебора чужих токенов)
)

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func normalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func validateEmail(s string) error {
	if n := utf8.RuneCountInString(s); n < 3 || n > 254 {
		return errors.New("от 3 до 254 знаков")
	}
	if !emailRe.MatchString(s) {
		return errors.New("не похоже на адрес почты")
	}
	return nil
}

// SetEmail проверяет пароль и адрес, гасит прежние неподтверждённые ссылки этого пользователя и высылает новую —
// почта становится действующей только после перехода по ней (ConfirmEmail). Ничего не меняет в users.email сразу:
// иначе чужой ввёл бы чужой адрес и незаметно бы его "занял" до проверки.
func (s *Service) SetEmail(ctx context.Context, userID int64, password, email string, ci ClientInfo) error {
	var user User
	if err := s.db.WithContext(ctx).Take(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNoSession
		}
		return err
	}
	email = normalizeEmail(email)
	if err := validateEmail(email); err != nil {
		return fieldError("email", err.Error())
	}
	if err := s.verifyOwnPassword(ctx, &user, password, ci); err != nil {
		return err
	}
	if allowed, retry := s.limiter.Allow(fmt.Sprintf("email:set:%d", userID), emailChangesPerHour, time.Hour); !allowed {
		return &RateLimitedError{RetryAfter: retry}
	}

	token, err := newToken()
	if err != nil {
		return err
	}
	now := s.now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&emailConfirmationRow{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", userID).Update("pending_email", email).Error; err != nil {
			return err
		}
		row := emailConfirmationRow{UserID: userID, Email: email, TokenHash: hashToken(token), CreatedAt: now, ExpiresAt: now.Add(emailConfirmTTL)}
		return tx.Create(&row).Error
	})
	if err != nil {
		return err
	}
	l := i18n.From(ctx)
	msg := mail.EmailConfirmation(l, user.Login, s.siteOrigin+"/email-confirm?token="+token)
	msg.To = email
	s.sendMail(msg)
	s.log.Info("почта: запрошено подтверждение", "user_id", userID)
	return nil
}

type emailConfirmationRow struct {
	ID        int64 `gorm:"primaryKey"`
	UserID    int64
	Email     string
	TokenHash []byte
	CreatedAt time.Time
	ExpiresAt time.Time
}

func (emailConfirmationRow) TableName() string { return "email_confirmations" }

// ConfirmEmail подтверждает почту по токену из письма; не требует входа — ссылку могут открыть в другом браузере.
func (s *Service) ConfirmEmail(ctx context.Context, token string, ci ClientInfo) error {
	if allowed, retry := s.limiter.Allow("email:confirm:"+ci.IP, emailConfirmsPerHour, time.Hour); !allowed {
		return &RateLimitedError{RetryAfter: retry}
	}
	var row emailConfirmationRow
	err := s.db.WithContext(ctx).Where("token_hash = ? AND expires_at > ?", hashToken(token), s.now()).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrEmailTokenInvalid
	}
	if err != nil {
		return err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&User{}).Where("id = ?", row.UserID).Updates(map[string]any{"email": row.Email, "pending_email": nil})
		if res.Error != nil {
			if errors.Is(res.Error, gorm.ErrDuplicatedKey) {
				return ErrEmailTaken
			}
			return res.Error
		}
		if err := tx.Where("user_id = ?", row.UserID).Delete(&emailConfirmationRow{}).Error; err != nil {
			return err
		}
		return audit.Record(tx, s.now(), audit.EmailConfirmed, audit.Event{ActorID: &row.UserID, TargetUserID: &row.UserID})
	})
	if err != nil {
		return err
	}
	s.log.Info("почта подтверждена", "user_id", row.UserID)
	return nil
}

// RemoveEmail снимает подтверждённую и неподтверждённую почту (пароль подтверждает владельца).
func (s *Service) RemoveEmail(ctx context.Context, userID int64, password string, ci ClientInfo) error {
	var user User
	if err := s.db.WithContext(ctx).Take(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNoSession
		}
		return err
	}
	if err := s.verifyOwnPassword(ctx, &user, password, ci); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&emailConfirmationRow{}).Error; err != nil {
			return err
		}
		return tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{"email": nil, "pending_email": nil}).Error
	})
}

// DeleteAccount («сдать дело в архив») полностью удаляет аккаунт и всё, что с ним связано (сессии,
// а по мере появления функций — комментарии, отметки и т.д. через ON DELETE CASCADE).
func (s *Service) DeleteAccount(ctx context.Context, userID int64, password string, ci ClientInfo) error {
	var user User
	if err := s.db.WithContext(ctx).Take(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNoSession
		}
		return err
	}
	if err := s.verifyOwnPassword(ctx, &user, password, ci); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&User{}, user.ID).Error; err != nil {
		return err
	}
	s.log.Info("аккаунт удалён владельцем", "user_id", user.ID, "ip", ci.IP)
	return nil
}

// ---------------------------------------------------------------- восстановление доступа

// RestoreResult — итог восстановления: новая сессия и НОВЫЙ резервный код (старый сгорает).
type RestoreResult struct {
	AuthResult
	BackupCode string
}

// RestoreAccess задаёт новый пароль по резервному коду. Все прежние сессии завершаются,
// резервный код заменяется новым. Попытки считаются теми же счётчиками, что и вход.
func (s *Service) RestoreAccess(ctx context.Context, login, backupCode, newPassword string, ci ClientInfo) (*RestoreResult, error) {
	fields := map[string]string{}
	if strings.TrimSpace(login) == "" {
		fields["login"] = "Введите логин"
	}
	if strings.TrimSpace(backupCode) == "" {
		fields["backup_code"] = "Введите резервный код"
	}
	if err := ValidatePassword(newPassword, strings.TrimSpace(login)); err != nil {
		fields["new_password"] = err.Error()
	}
	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}
	if err := s.checkLoginLimits(ci.IP, login); err != nil {
		return nil, err
	}

	fail := func() (*RestoreResult, error) {
		s.recordLoginFailure(ci.IP, login)
		s.log.Warn("восстановление: неверные данные", "ip", ci.IP, "login", LoginKey(login))
		return nil, ErrInvalidCredentials
	}

	canon, wellFormed := CanonicalBackupCode(backupCode)
	user, err := s.findByLogin(ctx, login)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, err
	}
	if user == nil || !wellFormed {
		// время тратится так же, как при настоящей проверке
		if err := s.hasher.VerifyDummy(ctx, canon); err != nil {
			return nil, err
		}
		return fail()
	}
	ok, _, err := s.hasher.Verify(ctx, canon, user.BackupCodeHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		return fail()
	}
	if user.BannedAt != nil {
		return nil, ErrBanned
	}
	s.recordLoginSuccess(ci.IP, login)

	passHash, err := s.hasher.Hash(ctx, preparePassword(newPassword))
	if err != nil {
		return nil, err
	}
	newCode, err := GenerateBackupCode()
	if err != nil {
		return nil, err
	}
	newCanon, _ := CanonicalBackupCode(newCode)
	newCodeHash, err := s.hasher.Hash(ctx, newCanon)
	if err != nil {
		return nil, err
	}

	now := s.now()
	var token string
	var expires time.Time
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"password_hash": passHash, "backup_code_hash": newCodeHash, "password_changed_at": now}
		if !user.TOTPEnabled() {
			updates["last_login_at"] = now
		}
		if err := tx.Model(&User{}).Where("id = ?", user.ID).Updates(updates).Error; err != nil {
			return err
		}
		if err := audit.Record(tx, now, audit.AccessRestored, audit.Event{ActorID: &user.ID, TargetUserID: &user.ID}); err != nil {
			return err
		}
		if err := s.revokeSessions(tx, user.ID, 0); err != nil {
			return err
		}
		if user.TOTPEnabled() {
			// Резервный код меняет пароль, но не заменяет код из приложения: вход — обычный, с обоими.
			return nil
		}
		var serr error
		token, expires, serr = s.createSession(tx, user.ID, ci)
		return serr
	})
	if err != nil {
		return nil, err
	}
	if token != "" {
		user.LastLoginAt = &now
	}
	if err := s.loadRoles(s.db.WithContext(ctx), user); err != nil {
		return nil, err
	}
	s.log.Info("доступ восстановлен по резервному коду", "user_id", user.ID, "ip", ci.IP, "signed_in", token != "")
	return &RestoreResult{AuthResult: AuthResult{User: *user, Token: token, ExpiresAt: expires}, BackupCode: newCode}, nil
}

// ---------------------------------------------------------------- администрирование (CLI)

// SetLevel задаёт уровень допуска пользователя (1–6).
func (s *Service) SetLevel(ctx context.Context, login string, level int) error {
	if !ValidLevel(level) {
		return fmt.Errorf("уровень должен быть от %d до %d", LevelVisitor, LevelCouncil)
	}
	return s.updateUser(ctx, login, map[string]any{"level": level})
}

// SetDirectorate включает или снимает статус Директората.
func (s *Service) SetDirectorate(ctx context.Context, login string, on bool) error {
	return s.updateUser(ctx, login, map[string]any{"directorate": on})
}

func (s *Service) updateUser(ctx context.Context, login string, updates map[string]any) error {
	user, err := s.findByLogin(ctx, login)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Model(&User{}).Where("id = ?", user.ID).Updates(updates).Error; err != nil {
		return err
	}
	s.log.Info("администратор изменил пользователя", "user_id", user.ID, "changes", fmt.Sprint(keys(updates)))
	return nil
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

const tempPasswordAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// AdminResetPassword («сброс через админа») задаёт пользователю случайный временный пароль
// и завершает все его сессии. Пароль возвращается один раз; пользователь меняет его при входе.
func (s *Service) AdminResetPassword(ctx context.Context, login string) (string, error) {
	user, err := s.findByLogin(ctx, login)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	alphabetLen := big.NewInt(int64(len(tempPasswordAlphabet)))
	for i := 0; i < 16; i++ {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", err
		}
		b.WriteByte(tempPasswordAlphabet[n.Int64()])
	}
	temp := b.String()

	hash, err := s.hasher.Hash(ctx, preparePassword(temp))
	if err != nil {
		return "", err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", user.ID).
			Updates(map[string]any{"password_hash": hash, "password_changed_at": s.now()}).Error; err != nil {
			return err
		}
		// без исполнителя: это команда автора на сервере
		if err := audit.Record(tx, s.now(), audit.PasswordReset, audit.Event{TargetUserID: &user.ID}); err != nil {
			return err
		}
		return s.revokeSessions(tx, user.ID, 0)
	})
	if err != nil {
		return "", err
	}
	// снять блокировки входа, наложенные перебором, чтобы владелец мог войти с новым паролем
	s.limiter.Reset(loginLockKey(user.Login))
	s.log.Info("администратор сбросил пароль", "user_id", user.ID)
	return temp, nil
}

// ---------------------------------------------------------------- обслуживание

// Cleanup удаляет просроченные сессии и вопросы анкеты.
func (s *Service) Cleanup(ctx context.Context) (sessions, captchas int64, err error) {
	now := s.now()
	res := s.db.WithContext(ctx).Where("expires_at <= ? OR absolute_expires_at <= ?", now, now).Delete(&Session{})
	if res.Error != nil {
		return 0, 0, res.Error
	}
	sessions = res.RowsAffected
	res = s.db.WithContext(ctx).Exec("DELETE FROM captcha_challenges WHERE expires_at <= ?", now)
	if res.Error != nil {
		return sessions, 0, res.Error
	}
	return sessions, res.RowsAffected, nil
}

// RunCleanup периодически вызывает Cleanup до отмены ctx.
func (s *Service) RunCleanup(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sessions, captchas, err := s.Cleanup(ctx)
			switch {
			case err != nil && ctx.Err() == nil:
				s.log.Error("очистка просроченных данных не удалась", "err", err)
			case sessions+captchas > 0:
				s.log.Debug("очистка просроченных данных", "sessions", sessions, "captchas", captchas)
			}
		}
	}
}

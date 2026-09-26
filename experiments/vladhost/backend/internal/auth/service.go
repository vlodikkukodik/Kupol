package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service struct {
	db         *gorm.DB
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
	revoked    revokedSet  // закрытые сессии, чьи access-токены ещё не истекли
	tickets    ticketStore // билеты второго шага входа (2FA)
}

func NewService(db *gorm.DB, secret []byte, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{db: db, secret: secret, accessTTL: accessTTL, refreshTTL: refreshTTL, now: time.Now}
}

type RegisterInput struct {
	Invite, Email, Username, Password string
	Lang                              string // язык писем: по языку интерфейса при регистрации
}

// NormalizeLang приводит язык к поддерживаемому (ru или it), иначе русский.
func NormalizeLang(l string) string {
	if l == "it" {
		return "it"
	}
	return "ru"
}

// dummyHash выравнивает время ответа при несуществующем логине, чтобы нельзя было перебирать пользователей.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("vladhost-dummy"), bcrypt.DefaultCost)

func (s *Service) Register(ctx context.Context, in RegisterInput) (*Session, error) {
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return nil, err
	}
	username, err := normalizeUsername(in.Username)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(in.Password); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	var user User
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var inv Invite
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", in.Invite).First(&inv).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidInvite
		}
		if err != nil {
			return err
		}
		now := s.now()
		if inv.UsedBy != nil || !inv.ExpiresAt.After(now) {
			return ErrInvalidInvite
		}
		user = User{Email: email, Username: username, PasswordHash: string(hash), Role: RoleUser, Lang: NormalizeLang(in.Lang), NotifyEmail: true}
		if err := tx.Create(&user).Error; err != nil {
			return mapUnique(err)
		}
		return tx.Model(&inv).Updates(map[string]any{"used_by": user.ID, "used_at": now}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.issue(ctx, s.db, user, 0)
}

// CreateAdmin заводит администратора без инвайта (первый запуск, CLI).
func (s *Service) CreateAdmin(ctx context.Context, email, username, password string) (*User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return nil, err
	}
	username, err = normalizeUsername(username)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := s.now()
	u := User{Email: email, Username: username, PasswordHash: string(hash), Role: RoleAdmin, Lang: "ru", NotifyEmail: true, EmailVerifiedAt: &now}
	if err := s.db.WithContext(ctx).Create(&u).Error; err != nil {
		return nil, mapUnique(err)
	}
	return &u, nil
}

// Login принимает email или имя пользователя. Если у аккаунта включён второй фактор, сессия не выдаётся:
// возвращается билет, с которым вход завершает LoginSecondFactor.
func (s *Service) Login(ctx context.Context, login, password string) (*Session, string, error) {
	var u User
	err := s.db.WithContext(ctx).Where("email = ? OR username = ?", lower(login), lower(login)).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, "", ErrInvalidCredentials
	}
	if err != nil {
		return nil, "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, "", ErrInvalidCredentials
	}
	if u.TOTPEnabledAt != nil {
		t, err := s.tickets.put(u.ID, s.now())
		return nil, t, err
	}
	sess, err := s.issue(ctx, s.db, u, 0)
	return sess, "", err
}

// Refresh меняет refresh-токен на новую пару. Повторное предъявление уже использованного токена
// считается кражей: все сессии пользователя закрываются.
func (s *Service) Refresh(ctx context.Context, raw string) (*Session, error) {
	var (
		sess   *Session
		reused int64
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rt RefreshToken
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ?", hashToken(raw)).First(&rt).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidToken
		}
		if err != nil {
			return err
		}
		sr, err := sessionOf(tx, rt)
		if err != nil {
			return err
		}
		if rt.RevokedAt != nil {
			// Токен закрытой сессии (выход, «завершить сеанс», смена пароля) — просто недействителен. Кража — это повтор
			// уже обменянного токена при живой сессии: тогда закрываем всё.
			if sr == nil || sr.RevokedAt == nil {
				reused = rt.UserID
			}
			return ErrInvalidToken
		}
		if !rt.ExpiresAt.After(s.now()) {
			return ErrInvalidToken
		}
		var u User
		if err := tx.First(&u, rt.UserID).Error; err != nil {
			return ErrInvalidToken
		}
		if sr != nil && (sr.RevokedAt != nil || sr.UserID != u.ID) {
			return ErrInvalidToken
		}
		if err := tx.Model(&rt).Update("revoked_at", s.now()).Error; err != nil {
			return err
		}
		var sid int64
		if sr != nil {
			sid = sr.ID
		}
		sess, err = s.issue(ctx, tx, u, sid)
		return err
	})
	if reused != 0 {
		_ = s.revokeSessions(s.db.WithContext(ctx), reused, 0, 0)
	}
	return sess, err
}

// ChangePassword меняет пароль после проверки текущего. Все прежние сессии закрываются — если пароль подобрали или украли,
// вход по старым сессиям не сохранится, — и текущему устройству выдаётся новая (currentSID — её номер в списке сессий, чтобы
// устройство не выглядело новым входом).
func (s *Service) ChangePassword(ctx context.Context, userID, currentSID int64, current, next string) (*Session, error) {
	if err := validatePassword(next); err != nil {
		return nil, applyField(err, "new_password")
	}
	var u User
	if err := s.db.WithContext(ctx).First(&u, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return nil, ErrWrongPassword
	}
	if current == next {
		return nil, ErrPasswordSame
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	var sess *Session
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&u).Update("password_hash", string(hash)).Error; err != nil {
			return err
		}
		if err := s.revokeSessions(tx, u.ID, 0, cmpNonZero(currentSID)); err != nil {
			return err
		}
		if err := tx.Model(&RefreshToken{}).Where("user_id = ? AND revoked_at IS NULL", u.ID).
			Update("revoked_at", s.now()).Error; err != nil {
			return err
		}
		u.PasswordHash = string(hash)
		sess, err = s.issue(ctx, tx, u, currentSID)
		return err
	})
	return sess, err
}

// Logout закрывает сессию, которой принадлежит refresh-токен (вместе с её access-токеном).
func (s *Service) Logout(ctx context.Context, raw string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rt RefreshToken
		err := tx.Where("token_hash = ? AND revoked_at IS NULL", hashToken(raw)).First(&rt).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if rt.SessionID != nil {
			return s.revokeSessions(tx, rt.UserID, *rt.SessionID, 0)
		}
		return tx.Model(&rt).Update("revoked_at", s.now()).Error
	})
}

func (s *Service) UserByID(ctx context.Context, id int64) (*User, error) {
	var u User
	if err := s.db.WithContext(ctx).First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	return &u, nil
}

type Claims struct {
	UserID    int64
	Role      Role
	SessionID int64 // 0 — токен выдан до появления сессий
}

func (s *Service) ParseAccess(token string) (*Claims, error) {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithTimeFunc(s.now))
	if err != nil || !parsed.Valid {
		return nil, ErrInvalidToken
	}
	m, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrInvalidToken
	}
	sub, _ := m["uid"].(float64)
	role, _ := m["role"].(string)
	if sub == 0 || role == "" {
		return nil, ErrInvalidToken
	}
	sid, _ := m["sid"].(float64)
	if sid != 0 && s.revoked.has(int64(sid), s.now()) {
		return nil, ErrInvalidToken
	}
	return &Claims{UserID: int64(sub), Role: Role(role), SessionID: int64(sid)}, nil
}

func (s *Service) CreateInvite(ctx context.Context, adminID int64, ttl time.Duration) (*Invite, error) {
	code, err := randomToken(12)
	if err != nil {
		return nil, err
	}
	inv := Invite{Code: code, CreatedBy: adminID, ExpiresAt: s.now().Add(ttl)}
	if err := s.db.WithContext(ctx).Create(&inv).Error; err != nil {
		return nil, err
	}
	return &inv, nil
}

func (s *Service) ListInvites(ctx context.Context) ([]InviteView, error) {
	var out []InviteView
	err := s.db.WithContext(ctx).Table("invites AS i").
		Select("i.*, u.username AS used_by_username").
		Joins("LEFT JOIN users u ON u.id = i.used_by").
		Order("i.id DESC").Limit(100).Scan(&out).Error
	return out, err
}

// issue выдаёт пару токенов. sessionID — продолжение существующей сессии (обновление, смена пароля); 0 — новый вход, новая сессия.
// Адрес и программа клиента берутся из контекста (WithClient).
func (s *Service) issue(ctx context.Context, db *gorm.DB, u User, sessionID int64) (*Session, error) {
	now := s.now()
	cl := clientFrom(ctx)
	expires := now.Add(s.refreshTTL)
	if sessionID != 0 {
		// Продолжаем только действующую сессию этого же пользователя; закрытую не воскрешаем — тогда это новый вход.
		upd := map[string]any{"last_seen_at": now, "expires_at": expires}
		if cl.IP != "" {
			upd["ip"] = cl.IP
		}
		if cl.UserAgent != "" {
			upd["user_agent"] = cl.UserAgent
		}
		res := db.WithContext(ctx).Model(&SessionRow{}).Where("id = ? AND user_id = ? AND revoked_at IS NULL", sessionID, u.ID).Updates(upd)
		if res.Error != nil {
			return nil, fmt.Errorf("update session: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			sessionID = 0
		}
	}
	if sessionID == 0 {
		sr := SessionRow{UserID: u.ID, IP: cl.IP, UserAgent: cl.UserAgent, CreatedAt: now, LastSeenAt: now, ExpiresAt: expires}
		if err := db.WithContext(ctx).Create(&sr).Error; err != nil {
			return nil, fmt.Errorf("save session: %w", err)
		}
		sessionID = sr.ID
	}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": u.ID, "role": string(u.Role), "sid": sessionID, "iat": now.Unix(), "exp": now.Add(s.accessTTL).Unix(),
	}).SignedString(s.secret)
	if err != nil {
		return nil, err
	}
	raw, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	rt := RefreshToken{UserID: u.ID, SessionID: &sessionID, TokenHash: hashToken(raw), ExpiresAt: expires}
	if err := db.WithContext(ctx).Create(&rt).Error; err != nil {
		return nil, fmt.Errorf("save refresh token: %w", err)
	}
	return &Session{
		AccessToken: access, ExpiresIn: int(s.accessTTL.Seconds()),
		RefreshToken: raw, RefreshExpires: rt.ExpiresAt, SessionID: sessionID, User: u,
	}, nil
}

func mapUnique(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		switch pg.ConstraintName {
		case "users_email_key":
			return ErrEmailTaken
		case "users_username_key":
			return ErrUsernameTaken
		}
	}
	return err
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func lower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

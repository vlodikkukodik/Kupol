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
}

func NewService(db *gorm.DB, secret []byte, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{db: db, secret: secret, accessTTL: accessTTL, refreshTTL: refreshTTL, now: time.Now}
}

type RegisterInput struct {
	Invite, Email, Username, Password string
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
		user = User{Email: email, Username: username, PasswordHash: string(hash), Role: RoleUser}
		if err := tx.Create(&user).Error; err != nil {
			return mapUnique(err)
		}
		return tx.Model(&inv).Updates(map[string]any{"used_by": user.ID, "used_at": now}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.issue(ctx, s.db, user)
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
	u := User{Email: email, Username: username, PasswordHash: string(hash), Role: RoleAdmin}
	if err := s.db.WithContext(ctx).Create(&u).Error; err != nil {
		return nil, mapUnique(err)
	}
	return &u, nil
}

// Login принимает email или имя пользователя.
func (s *Service) Login(ctx context.Context, login, password string) (*Session, error) {
	var u User
	err := s.db.WithContext(ctx).Where("email = ? OR username = ?", lower(login), lower(login)).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return s.issue(ctx, s.db, u)
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
		if rt.RevokedAt != nil {
			reused = rt.UserID
			return ErrInvalidToken
		}
		if !rt.ExpiresAt.After(s.now()) {
			return ErrInvalidToken
		}
		var u User
		if err := tx.First(&u, rt.UserID).Error; err != nil {
			return ErrInvalidToken
		}
		if err := tx.Model(&rt).Update("revoked_at", s.now()).Error; err != nil {
			return err
		}
		sess, err = s.issue(ctx, tx, u)
		return err
	})
	if reused != 0 {
		s.db.WithContext(ctx).Model(&RefreshToken{}).
			Where("user_id = ? AND revoked_at IS NULL", reused).Update("revoked_at", s.now())
	}
	return sess, err
}

func (s *Service) Logout(ctx context.Context, raw string) error {
	return s.db.WithContext(ctx).Model(&RefreshToken{}).
		Where("token_hash = ? AND revoked_at IS NULL", hashToken(raw)).Update("revoked_at", s.now()).Error
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
	UserID int64
	Role   Role
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
	return &Claims{UserID: int64(sub), Role: Role(role)}, nil
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

func (s *Service) issue(ctx context.Context, db *gorm.DB, u User) (*Session, error) {
	now := s.now()
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": u.ID, "role": string(u.Role), "iat": now.Unix(), "exp": now.Add(s.accessTTL).Unix(),
	}).SignedString(s.secret)
	if err != nil {
		return nil, err
	}
	raw, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	rt := RefreshToken{UserID: u.ID, TokenHash: hashToken(raw), ExpiresAt: now.Add(s.refreshTTL)}
	if err := db.WithContext(ctx).Create(&rt).Error; err != nil {
		return nil, fmt.Errorf("сохранение refresh-токена: %w", err)
	}
	return &Session{
		AccessToken: access, ExpiresIn: int(s.accessTTL.Seconds()),
		RefreshToken: raw, RefreshExpires: rt.ExpiresAt, User: u,
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

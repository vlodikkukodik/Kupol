package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"vladhost/internal/apperr"
)

// Одноразовые ссылки из писем: подтверждение адреса и сброс пароля. В БД лежит только хеш токена.

const (
	TokenVerify = "verify"
	TokenReset  = "reset"

	verifyTTL = 48 * time.Hour
	resetTTL  = time.Hour

	// Письма одного вида одному пользователю: не чаще раза в минуту и не больше пяти в час (защита от «бомбардировки» почты).
	mailMinGap  = time.Minute
	mailPerHour = 5
)

var (
	ErrInvalidMailToken = apperr.New(http.StatusBadRequest, "invalid_mail_token", "link is invalid, expired or already used")
	ErrMailCooldown     = apperr.New(http.StatusTooManyRequests, "mail_cooldown", "too many emails requested")
	ErrAlreadyVerified  = apperr.New(http.StatusConflict, "email_verified", "email is already verified")
)

// MailToken — ссылка из письма.
type MailToken struct {
	ID        int64 `gorm:"primaryKey"`
	UserID    int64
	Kind      string
	TokenHash string
	Email     string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// UserByEmail находит пользователя по адресу почты (без учёта регистра).
func (s *Service) UserByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	if err := s.db.WithContext(ctx).Where("email = ?", lower(email)).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	return &u, nil
}

// IssueMailToken выпускает одноразовую ссылку для письма. Прежние неиспользованные ссылки того же вида перестают
// действовать: действует только последняя. Слишком частые запросы отклоняются ErrMailCooldown.
func (s *Service) IssueMailToken(ctx context.Context, userID int64, kind string) (string, *User, error) {
	var u User
	if err := s.db.WithContext(ctx).First(&u, userID).Error; err != nil {
		return "", nil, err
	}
	if kind == TokenVerify && u.EmailVerifiedAt != nil {
		return "", &u, ErrAlreadyVerified
	}
	ttl := verifyTTL
	if kind == TokenReset {
		ttl = resetTTL
	}
	now := s.now()
	raw, err := randomToken(32)
	if err != nil {
		return "", nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Блокировка строки пользователя сериализует параллельные запросы: лимит нельзя обойти гонкой.
		if err := tx.Raw("SELECT id FROM users WHERE id = ? FOR UPDATE", userID).Scan(new(int64)).Error; err != nil {
			return err
		}
		var lastMinute, lastHour int64
		if err := tx.Model(&MailToken{}).Where("user_id = ? AND kind = ? AND created_at > ?", userID, kind, now.Add(-mailMinGap)).Count(&lastMinute).Error; err != nil {
			return err
		}
		if err := tx.Model(&MailToken{}).Where("user_id = ? AND kind = ? AND created_at > ?", userID, kind, now.Add(-time.Hour)).Count(&lastHour).Error; err != nil {
			return err
		}
		if lastMinute > 0 || lastHour >= mailPerHour {
			return ErrMailCooldown
		}
		if err := tx.Model(&MailToken{}).Where("user_id = ? AND kind = ? AND used_at IS NULL", userID, kind).
			Update("used_at", now).Error; err != nil {
			return err
		}
		return tx.Create(&MailToken{UserID: userID, Kind: kind, TokenHash: hashToken(raw), Email: u.Email, ExpiresAt: now.Add(ttl), CreatedAt: now}).Error
	})
	if err != nil {
		return "", nil, err
	}
	return raw, &u, nil
}

// consume находит действующую ссылку и помечает её использованной в рамках tx.
func (s *Service) consume(tx *gorm.DB, raw, kind string) (*MailToken, error) {
	var t MailToken
	err := tx.Raw(`SELECT * FROM mail_tokens WHERE token_hash = ? AND kind = ? AND used_at IS NULL AND expires_at > ? FOR UPDATE`,
		hashToken(raw), kind, s.now()).Scan(&t).Error
	if err != nil {
		return nil, err
	}
	if t.ID == 0 {
		return nil, ErrInvalidMailToken
	}
	if err := tx.Model(&MailToken{}).Where("id = ?", t.ID).Update("used_at", s.now()).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// VerifyEmail подтверждает адрес по ссылке из письма. Ссылка привязана к тому адресу, на который отправлена:
// если адрес с тех пор изменился, она недействительна.
func (s *Service) VerifyEmail(ctx context.Context, raw string) (*User, error) {
	var u User
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t, err := s.consume(tx, raw, TokenVerify)
		if err != nil {
			return err
		}
		if err := tx.First(&u, t.UserID).Error; err != nil {
			return err
		}
		if u.Email != t.Email {
			return ErrInvalidMailToken
		}
		now := s.now()
		if u.EmailVerifiedAt == nil {
			u.EmailVerifiedAt = &now
			return tx.Model(&u).Update("email_verified_at", now).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ResetPassword задаёт новый пароль по ссылке из письма. Все сессии аккаунта закрываются, остальные ссылки сброса
// гасятся, а адрес считается подтверждённым: письмо дошло до владельца. Сессию не выдаём — пользователь входит сам.
func (s *Service) ResetPassword(ctx context.Context, raw, password string) (*User, error) {
	if err := validatePassword(password); err != nil {
		return nil, applyField(err, "password")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	var u User
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t, err := s.consume(tx, raw, TokenReset)
		if err != nil {
			return err
		}
		if err := tx.First(&u, t.UserID).Error; err != nil {
			return err
		}
		now := s.now()
		upd := map[string]any{"password_hash": string(hash)}
		if u.EmailVerifiedAt == nil && u.Email == t.Email {
			upd["email_verified_at"] = now
		}
		if err := tx.Model(&u).Updates(upd).Error; err != nil {
			return err
		}
		if err := tx.Model(&MailToken{}).Where("user_id = ? AND kind = ? AND used_at IS NULL", u.ID, TokenReset).Update("used_at", now).Error; err != nil {
			return err
		}
		return s.revokeSessions(tx, u.ID, 0, 0)
	})
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Preferences — что пользователь может менять в настройках почты.
type Preferences struct {
	Lang        *string
	NotifyEmail *bool
}

// UpdatePreferences меняет язык писем и согласие на уведомления.
func (s *Service) UpdatePreferences(ctx context.Context, userID int64, p Preferences) (*User, error) {
	upd := map[string]any{}
	if p.Lang != nil {
		upd["lang"] = NormalizeLang(*p.Lang)
	}
	if p.NotifyEmail != nil {
		upd["notify_email"] = *p.NotifyEmail
	}
	if len(upd) > 0 {
		if err := s.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Updates(upd).Error; err != nil {
			return nil, err
		}
	}
	return s.UserByID(ctx, userID)
}

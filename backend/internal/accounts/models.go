package accounts

import (
	"time"

	"kupol/internal/i18n"
)

// User — запись пользователя (таблица users).
type User struct {
	ID                int64 `gorm:"primaryKey"`
	Login             string
	PasswordHash      string
	BackupCodeHash    string
	Level             int
	Directorate       bool
	CreatedAt         time.Time
	LastLoginAt       *time.Time
	PasswordChangedAt time.Time

	// Код из приложения (TOTP): секреты хранятся зашифрованными, наружу не выходят (см. totp.go).
	TOTPPending   []byte     `gorm:"column:totp_pending"`
	TOTPSecret    []byte     `gorm:"column:totp_secret"`
	TOTPEnabledAt *time.Time `gorm:"column:totp_enabled_at"`
	TOTPLastStep  int64      `gorm:"column:totp_last_step"`

	// Roles — роли команды (таблица user_roles). Не колонка: подгружается при входе и проверке сессии.
	Roles []Role `gorm:"-"`
}

func (User) TableName() string { return "users" }

// TOTPEnabled — включён ли вход с кодом из приложения.
func (u User) TOTPEnabled() bool { return u.TOTPEnabledAt != nil }

// LevelName — звание пользователя (Директорат показывается вместо уровня).
func (u User) LevelName() string { return LevelName(u.Level, u.Directorate) }

// LevelNameIn — звание пользователя на языке l.
func (u User) LevelNameIn(l i18n.Lang) string { return LevelNameIn(l, u.Level, u.Directorate) }

// Session — серверная сессия (таблица sessions). В БД хранится только SHA-256 токена.
type Session struct {
	ID                int64 `gorm:"primaryKey"`
	TokenHash         []byte
	UserID            int64
	CreatedAt         time.Time
	LastSeenAt        time.Time
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
	IP                *string
	UserAgent         string
}

func (Session) TableName() string { return "sessions" }

package accounts

import "time"

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

	// Roles — роли команды (таблица user_roles). Не колонка: подгружается при входе и проверке сессии.
	Roles []Role `gorm:"-"`
}

func (User) TableName() string { return "users" }

// LevelName — звание пользователя (Директорат показывается вместо уровня).
func (u User) LevelName() string { return LevelName(u.Level, u.Directorate) }

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

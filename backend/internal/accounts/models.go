package accounts

import (
	"time"

	"kupol/internal/i18n"
	"kupol/internal/xp"
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

	// XP, LoginStreak, LastXPDay — очки опыта и серия ежедневных входов (internal/xp, шаг 5.1).
	XP          int        `gorm:"column:xp"`
	LoginStreak int        `gorm:"column:login_streak"`
	LastXPDay   *time.Time `gorm:"column:last_xp_day"`

	// Email — подтверждённая почта (шаг 5.1.1: по желанию, спецификация «без почты» смягчена — пишем письма-уведомления,
	// вход по-прежнему только по логину и паролю). PendingEmail — указан, но ссылку подтверждения ещё не открыли.
	Email        *string `gorm:"column:email"`
	PendingEmail *string `gorm:"column:pending_email"`

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

// NextLevelXP — сколько XP нужно для следующего уровня; 0, если дальше только по решению Особого Совета.
func (u User) NextLevelXP() int { return xp.NextLevelThreshold(u.Level) }

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

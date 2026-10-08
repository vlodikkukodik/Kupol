// Package mailsettings — настройки почтового сервера, которые Директорат включает и правит в панели команды.
//
// Пока настройки не сохраняли, действует окружение (KUPOL_SMTP_*): панель показывает его как исходное
// и при первом сохранении переносит в базу, после чего решает уже база — в том числе можно почту выключить,
// даже если в окружении она задана. Пароль хранится зашифрованным (AES-GCM, ключ выводится из общего
// секрета сервера) и наружу не выходит: в ответе остаётся только признак «пароль задан».
package mailsettings

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"kupol/internal/audit"
	"kupol/internal/config"
	"kupol/internal/i18n"
	"kupol/internal/mail"
)

// ErrForbidden — настройки почты меняет только Директорат.
var ErrForbidden = errors.New("mailsettings: почту настраивает только Директорат")

// ErrNotEnabled — отправка выключена: письмо-проверку отправлять нечего.
var ErrNotEnabled = errors.New("mailsettings: отправка писем выключена")

// SendError — письмо-проверка не дошло до сервера. Подробности (адрес, ответ SMTP) — в журнале, читателю — только совет.
type SendError struct {
	Err error `tstype:"Error"`
}

func (e *SendError) Error() string { return "mailsettings: " + e.Err.Error() }
func (e *SendError) Unwrap() error { return e.Err }

// ValidationError — не прошла проверка полей формы; Message уже на языке запроса.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("mailsettings: ошибки в полях: %v", e.Fields)
}

// Actor — кто открыл настройки: правит только Директорат.
type Actor struct {
	UserID      int64
	Directorate bool
}

// Out — настройки в панели команды. Пароль не входит: видно только, задан ли он.
type Out struct {
	Enabled     bool       `json:"enabled"`
	Host        string     `json:"host"`
	Port        string     `json:"port"`
	Username    string     `json:"username"`
	PasswordSet bool       `json:"password_set"`
	From        string     `json:"from"`
	FromName    string     `json:"from_name"`
	Source      string     `json:"source"` // "env" — из окружения, "db" — сохранены в панели
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
	UpdatedBy   *string    `json:"updated_by,omitempty"`
	// CanEdit — вправе ли человек менять настройки (только Директорат).
	CanEdit bool `json:"can_edit"`
}

// Input — что прислал Директорат.
type Input struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
	// Password — nil (ключ не прислали): сохранённый остаётся; "": снять пароль. Интерфейс при пустом
	// поле ключ не шлёт (TeamSmtpView.vue), поэтому пустое поле формы сохранённый не затирает.
	Password *string `json:"password"`
	From     string  `json:"from"`
	FromName string  `json:"from_name"`
}

type row struct {
	Singleton bool `gorm:"primaryKey"`
	Enabled   bool
	Host      string
	Port      string
	Username  string
	Password  []byte
	FromAddr  string `gorm:"column:from_addr"`
	FromName  string `gorm:"column:from_name"`
	UpdatedAt *time.Time
	UpdatedBy *int64
}

func (row) TableName() string { return "smtp_settings" }

const (
	maxHostLen  = 255
	maxUserLen  = 255
	maxFromLen  = 320
	maxNameLen  = 255
	defaultPort = "587"
	defaultName = "КУПОЛ"
	envSource   = "env"
	dbSource    = "db"
)

// Service — настройки почты.
type Service struct {
	db  *gorm.DB
	env config.SMTP
	box *secretBox
	log *slog.Logger
	now func() time.Time
}

// New создаёт сервис. now == nil — настоящее время.
func New(db *gorm.DB, env config.SMTP, secretKey []byte, log *slog.Logger, now func() time.Time) (*Service, error) {
	box, err := newSecretBox(secretKey)
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{db: db, env: env, box: box, log: log, now: now}, nil
}

// Get — настройки для панели команды: действующие (окружение или база) и право их менять.
func (s *Service) Get(ctx context.Context, a Actor) (*Out, error) {
	r, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := &Out{CanEdit: a.Directorate, Source: envSource}
	enabled, host, port, username, _, from, fromName := s.effective(r)
	if r != nil {
		out.Source = dbSource
		out.UpdatedAt = r.UpdatedAt
		if r.UpdatedBy != nil {
			var login string
			if err := s.db.WithContext(ctx).Raw(`SELECT login::text FROM users WHERE id = ?`, *r.UpdatedBy).Scan(&login).Error; err != nil {
				return nil, err
			}
			out.UpdatedBy = &login
		}
	}
	out.Enabled, out.Host, out.Port, out.Username, out.From, out.FromName = enabled, host, port, username, from, fromName
	out.PasswordSet = s.password(r) != ""
	return out, nil
}

// Update сохраняет настройки. Только Директорат; журнал получает факт и длины, но не пароль.
func (s *Service) Update(ctx context.Context, a Actor, in Input) (*Out, error) {
	if !a.Directorate {
		return nil, ErrForbidden
	}
	r, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	lang := i18n.From(ctx)
	probs := map[string]string{}

	host := strings.TrimSpace(in.Host)
	port := strings.TrimSpace(in.Port)
	username := strings.TrimSpace(in.Username)
	from := strings.TrimSpace(in.From)
	fromName := strings.TrimSpace(in.FromName)
	if port == "" {
		port = defaultPort
	}
	if fromName == "" {
		fromName = defaultName
	}

	if n := utf8.RuneCountInString(host); n > maxHostLen {
		probs["host"] = lang.T("слишком длинное значение (%d символов, не больше %d)", n, maxHostLen)
	}
	if n := utf8.RuneCountInString(username); n > maxUserLen {
		probs["username"] = lang.T("слишком длинное значение (%d символов, не больше %d)", n, maxUserLen)
	}
	if n := utf8.RuneCountInString(from); n > maxFromLen {
		probs["from"] = lang.T("слишком длинное значение (%d символов, не больше %d)", n, maxFromLen)
	}
	if n := utf8.RuneCountInString(fromName); n > maxNameLen {
		probs["from_name"] = lang.T("слишком длинное значение (%d символов, не больше %d)", n, maxNameLen)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		probs["port"] = lang.Translate("Номер порта: от 1 до 65535")
	}
	if in.Enabled && host == "" {
		probs["host"] = lang.Translate("Укажите адрес SMTP-сервера")
	}
	if in.Enabled && from == "" {
		probs["from"] = lang.Translate("Укажите адрес отправителя")
	} else if from != "" && !strings.Contains(from, "@") {
		probs["from"] = lang.Translate("Адрес отправителя должен быть вида name@example.org")
	}
	// пароль: новый не прислали — остаётся прежний (из базы или из окружения при первом сохранении)
	prev := s.password(r)
	if in.Password != nil {
		prev = *in.Password
	}
	if username != "" && strings.TrimSpace(prev) == "" {
		probs["password"] = lang.Translate("Укажите пароль SMTP")
	}
	if len(probs) > 0 {
		return nil, &ValidationError{Fields: probs}
	}

	sealed, err := s.box.seal([]byte(prev))
	if err != nil {
		return nil, err
	}
	now := s.now()
	uid := a.UserID
	nr := row{
		Singleton: true, Enabled: in.Enabled, Host: host, Port: port, Username: username,
		Password: sealed, FromAddr: from, FromName: fromName, UpdatedAt: &now, UpdatedBy: &uid,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "singleton"}},
			DoUpdates: clause.AssignmentColumns([]string{"enabled", "host", "port", "username", "password", "from_addr", "from_name", "updated_at", "updated_by"}),
		}).Create(&nr).Error
		if err != nil {
			return err
		}
		// в журнал — включена ли почта и длины полей; пароль и содержимое полей не пишем
		return audit.Record(tx, now, audit.SMTPUpdated, audit.Event{
			ActorID: &uid,
			Details: audit.Details("enabled", in.Enabled, "host_length", utf8.RuneCountInString(host), "has_password", prev != ""),
		})
	})
	if err != nil {
		return nil, err
	}
	s.log.Info("настройки почты сохранены", "enabled", in.Enabled, "host", host)
	return s.Get(ctx, a)
}

// Test отправляет письмо-проверку на указанный адрес текущими настройками.
func (s *Service) Test(ctx context.Context, a Actor, to string) error {
	if !a.Directorate {
		return ErrForbidden
	}
	to = strings.TrimSpace(to)
	lang := i18n.From(ctx)
	if !strings.Contains(to, "@") || utf8.RuneCountInString(to) > maxFromLen {
		return &ValidationError{Fields: map[string]string{"to": lang.Translate("Укажите адрес почты получателя")}}
	}
	sender, ok := s.Sender(ctx)
	if !ok {
		return ErrNotEnabled
	}
	msg := mail.TestMessage(lang)
	msg.To = to
	tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := sender.Send(tctx, msg); err != nil {
		return &SendError{Err: err}
	}
	s.log.Info("отправлено письмо-проверка", "to", to)
	return nil
}

// Sender — текущий отправитель: nil, если почта выключена. Читает базу при каждом вызове, поэтому
// сохранение настроек в панели действует сразу, без перезапуска.
func (s *Service) Sender(ctx context.Context) (*mail.SMTPSender, bool) {
	r, err := s.load(ctx)
	if err != nil {
		s.log.Warn("почта: не удалось прочитать настройки", "err", err)
		return nil, false
	}
	enabled, host, port, username, password, from, fromName := s.effective(r)
	if !enabled || host == "" || from == "" {
		return nil, false
	}
	return mail.NewSMTPSender(host, port, username, password, from, fromName), true
}

// load — строка настроек; nil, пока ничего не сохраняли (обновлено_at заполняется только при сохранении).
func (s *Service) load(ctx context.Context) (*row, error) {
	var r row
	err := s.db.WithContext(ctx).Raw(
		`SELECT singleton, enabled, host, port, username, password, from_addr, from_name, updated_at, updated_by
		 FROM smtp_settings LIMIT 1`).Scan(&r).Error
	if err != nil {
		return nil, err
	}
	if r.UpdatedAt == nil {
		return nil, nil
	}
	return &r, nil
}

// effective — действующие настройки: база, если сохраняли, иначе окружение.
func (s *Service) effective(r *row) (enabled bool, host, port, username, password, from, fromName string) {
	if r == nil {
		return s.env.Enabled(), s.env.Host, s.env.Port, s.env.Username, s.env.Password, s.env.From, s.env.FromName
	}
	return r.Enabled, r.Host, r.Port, r.Username, s.open(r), r.FromAddr, r.FromName
}

// password — пароль из строки настроек (или окружения, если строки нет), расшифрованный.
func (s *Service) password(r *row) string {
	if r == nil {
		return s.env.Password
	}
	return s.open(r)
}

// open расшифровывает пароль; повреждённый шифртекст — пустой пароль (настройки придётся ввести заново).
func (s *Service) open(r *row) string {
	if len(r.Password) == 0 {
		return ""
	}
	plain, err := s.box.open(r.Password)
	if err != nil {
		s.log.Warn("почта: сохранённый пароль не читается (сменился общий секрет?)", "err", err)
		return ""
	}
	return string(plain)
}

// ---------------------------------------------------------------- шифрование пароля

// secretBox шифрует пароль SMTP тем же способом, что accounts шифрует секреты TOTP: ключ выводится
// из общего секрета сервера, поэтому отдельного ключа заводить не нужно. Цена та же — смена общего
// секрета делает сохранённый пароль нечитаемым, его придётся ввести заново.
type secretBox struct{ gcm cipher.AEAD }

func newSecretBox(serverSecret []byte) (*secretBox, error) {
	if len(serverSecret) < 32 {
		return nil, errors.New("mailsettings: общий секрет сервера короче 32 байт")
	}
	mac := hmac.New(sha256.New, serverSecret)
	mac.Write([]byte("kupol/smtp-password-box/v1"))
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

func (b *secretBox) seal(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return []byte{}, nil // пустой, а не nil: колонка NOT NULL, а GORM пишет nil как NULL
	}
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.gcm.Seal(nonce, nonce, plain, nil), nil
}

func (b *secretBox) open(sealed []byte) ([]byte, error) {
	n := b.gcm.NonceSize()
	if len(sealed) < n+b.gcm.Overhead() {
		return nil, errors.New("mailsettings: пароль повреждён")
	}
	return b.gcm.Open(nil, sealed[:n], sealed[n:], nil)
}

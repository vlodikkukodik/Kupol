// Package mailhost — почта на своих доменах: домены, ящики, алиасы, ключи DKIM и подсказки DNS. Панель хранит желаемое состояние в БД и
// отдаёт его серверу файлом mail/state.json; раскладывает файлы для exim и dovecot исполнитель от root (deploy/bin/mail-sync.py).
package mailhost

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// Domain — почтовый домен пользователя.
type Domain struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	UserID       int64     `json:"-"`
	Domain       string    `json:"domain"`
	Enabled      bool      `json:"enabled"`
	Verified     bool      `json:"verified"` // владение подтверждено: только такой домен принимает и отправляет почту
	Token        string    `json:"-"`        // код для TXT-записи подтверждения
	DKIMSelector string    `gorm:"column:dkim_selector" json:"dkim_selector"`
	DKIMPrivate  string    `gorm:"column:dkim_private" json:"-"`
	DKIMPublic   string    `gorm:"column:dkim_public" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

func (Domain) TableName() string { return "mail_domains" }

// Mailbox — почтовый ящик.
type Mailbox struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	DomainID     int64     `json:"domain_id"`
	LocalPart    string    `json:"local"`
	PasswordHash string    `json:"-"`
	QuotaMB      int       `gorm:"column:quota_mb" json:"quota_mb"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	// Правила ящика хранятся плоскими колонками, а наружу отдаются готовыми объектами (AutoReply, Forward).
	AutoreplyEnabled bool      `gorm:"column:autoreply_enabled" json:"-"`
	AutoreplySubject string    `gorm:"column:autoreply_subject" json:"-"`
	AutoreplyBody    string    `gorm:"column:autoreply_body" json:"-"`
	AutoreplyFrom    string    `gorm:"column:autoreply_from" json:"-"`
	AutoreplyTo      string    `gorm:"column:autoreply_to" json:"-"`
	AutoreplyDays    int       `gorm:"column:autoreply_days" json:"-"`
	ForwardTo        string    `gorm:"column:forward_to" json:"-"`
	ForwardKeep      bool      `gorm:"column:forward_keep" json:"-"`
	AutoReply        AutoReply `gorm:"-" json:"autoreply"`
	Forward          Forward   `gorm:"-" json:"forward"`
	// UsedBytes — сколько занимает почта (по последней синхронизации); в таблице не хранится.
	UsedBytes int64 `gorm:"-" json:"used_bytes"`
}

// AutoReply — автоответчик ящика: на письма в указанные даты (если заданы) отвечает не чаще одного раза за Days дней одному адресу.
type AutoReply struct {
	Enabled bool   `json:"enabled"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	From    string `json:"from"` // ГГГГ-ММ-ДД или пусто
	To      string `json:"to"`   // ГГГГ-ММ-ДД или пусто
	Days    int    `json:"days"`
}

// Forward — пересылка писем ящика на другие адреса; KeepCopy — оставлять ли копию в самом ящике.
type Forward struct {
	To       []string `json:"to"`
	KeepCopy bool     `json:"keep_copy"`
}

// AfterFind собирает объекты правил из колонок при каждом чтении ящика.
func (m *Mailbox) AfterFind(*gorm.DB) error {
	m.AutoReply = AutoReply{Enabled: m.AutoreplyEnabled, Subject: m.AutoreplySubject, Body: m.AutoreplyBody, From: m.AutoreplyFrom, To: m.AutoreplyTo, Days: m.AutoreplyDays}
	m.Forward = Forward{To: []string{}, KeepCopy: m.ForwardKeep}
	if m.ForwardTo != "" {
		m.Forward.To = strings.Split(m.ForwardTo, "\n")
	}
	return nil
}

func (Mailbox) TableName() string { return "mailboxes" }

// Alias — пересылка адреса на другие адреса; «*» — общий ящик домена.
type Alias struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	DomainID     int64     `json:"domain_id"`
	LocalPart    string    `json:"local"`
	Destinations string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	// To — адреса назначения списком (заполняется при чтении).
	To []string `gorm:"-" json:"to"`
}

func (Alias) TableName() string { return "mail_aliases" }

type purge struct {
	Path string `gorm:"primaryKey"`
}

func (purge) TableName() string { return "mail_purge" }

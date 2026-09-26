// Package notify — письма панели: подтверждение почты, сброс пароля и уведомления о проблемах (сертификат, диск).
// Письма не отправляются прямо из запроса: они попадают в очередь (таблица mail_outbox), а фоновая служба отправляет их
// с повторами при сбоях, поэтому недоступный SMTP-сервер не ломает панель и письма не теряются.
package notify

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"vladhost/internal/auth"
	"vladhost/internal/mailer"
)

// ErrDisabled — почта не настроена (нет SMTP-сервера).
var ErrDisabled = errors.New("notify: почта не настроена")

const (
	maxAttempts    = 6
	sendLease      = 5 * time.Minute // на это время письмо «занято» отправляющим: другой воркер его не возьмёт
	keepFinished   = 30 * 24 * time.Hour
	maxSubjectLen  = 200
	batchPerTick   = 20
	cleanupEvery   = time.Hour
	errorTextLimit = 300
)

// backoff — пауза перед следующей попыткой после неудачи номер n (n ≥ 1).
var backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

// Outbox — письмо в очереди.
type Outbox struct {
	ID            int64 `gorm:"primaryKey"`
	UserID        *int64
	Kind          string
	ToEmail       string
	Subject       string
	TextBody      string
	HTMLBody      string
	Status        string
	Attempts      int
	NextAttemptAt time.Time
	LastError     string
	CreatedAt     time.Time
	SentAt        *time.Time
}

func (Outbox) TableName() string { return "mail_outbox" }

// Service — очередь писем и правила уведомлений.
type Service struct {
	db     *gorm.DB
	sender mailer.Sender
	origin string // https://app.vladinc.ru — из него строятся ссылки в письмах
	now    func() time.Time
	// retry — паузы между попытками; в тестах сокращаются.
	retry []time.Duration
	// dbLimit — лимит размера пользовательской базы (для текста письма о заморозке); 0 — в письме не указывается.
	dbLimit int64
	// mailFill — сведения о заполненности почтовых ящиков; nil — раздел «Почта» выключен.
	mailFill func(ctx context.Context) ([]MailboxFill, error)
	fillMu   sync.RWMutex
}

// MailboxFill — заполненность почтового ящика; источником служит раздел «Почта».
type MailboxFill struct {
	UserID  int64
	Address string
	Used    int64
	Quota   int64
}

// wantsMail: письма о тикетах получают только те, кто подтвердил адрес и не отказался от уведомлений.
func wantsMail(u auth.User) bool { return u.NotifyEmail && u.EmailVerifiedAt != nil }

// TicketNew сообщает администраторам о новом обращении или о новом сообщении пользователя в нём (followUp).
func (s *Service) TicketNew(ctx context.Context, admins []auth.User, author auth.User, id int64, subject, body string, followUp bool) {
	if !s.Enabled() {
		return
	}
	kind := KindTicketNew
	if followUp {
		kind = KindTicketUserReply
	}
	for _, a := range admins {
		if a.ID == author.ID || !wantsMail(a) {
			continue
		}
		if err := s.send(ctx, nil, a, kind, Data{Host: subject, Reason: body, Author: author.Username, Ticket: id, Link: s.Link("/support/" + strconv.FormatInt(id, 10))}); err != nil {
			log.Printf("уведомления: обращение %d: %v", id, err)
		}
	}
}

// TicketReply сообщает пользователю об ответе поддержки.
func (s *Service) TicketReply(ctx context.Context, owner auth.User, id int64, subject, body string) {
	if !s.Enabled() || !wantsMail(owner) {
		return
	}
	if err := s.send(ctx, nil, owner, KindTicketReply, Data{Host: subject, Reason: body, Ticket: id, Link: s.Link("/support/" + strconv.FormatInt(id, 10))}); err != nil {
		log.Printf("уведомления: ответ по обращению %d: %v", id, err)
	}
}

// SetMailboxFill подключает источник сведений о заполненности почтовых ящиков (без него письма о заполнении не отправляются).
func (s *Service) SetMailboxFill(f func(ctx context.Context) ([]MailboxFill, error)) {
	if s != nil {
		s.fillMu.Lock()
		s.mailFill = f
		s.fillMu.Unlock()
	}
}

// SetDatabaseLimit задаёт лимит размера базы, о котором сообщается в письме о заморозке.
func (s *Service) SetDatabaseLimit(n int64) {
	if s != nil {
		s.dbLimit = n
	}
}

// New создаёт службу. sender == nil — почта выключена: Enabled() ложно, письма не ставятся в очередь.
func New(db *gorm.DB, sender mailer.Sender, origin string) *Service {
	return &Service{db: db, sender: sender, origin: strings.TrimRight(origin, "/"), now: time.Now, retry: backoff}
}

// Enabled сообщает, настроена ли отправка почты.
func (s *Service) Enabled() bool { return s != nil && s.sender != nil }

// Link строит абсолютную ссылку на страницу панели.
func (s *Service) Link(path string) string { return s.origin + path }

// Mail — письмо для очереди.
type Mail struct {
	UserID  *int64
	Kind    string
	To      string
	Subject string
	Text    string
	HTML    string
}

// Enqueue ставит письмо в очередь в рамках db (можно передать транзакцию).
func (s *Service) Enqueue(ctx context.Context, db *gorm.DB, m Mail) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	if db == nil {
		db = s.db
	}
	subject := m.Subject
	if len(subject) > maxSubjectLen {
		subject = strings.ToValidUTF8(subject[:maxSubjectLen], "")
	}
	return db.WithContext(ctx).Create(&Outbox{
		UserID: m.UserID, Kind: m.Kind, ToEmail: m.To, Subject: subject, TextBody: m.Text, HTMLBody: m.HTML,
		Status: "queued", NextAttemptAt: s.now(), CreatedAt: s.now(),
	}).Error
}

// send рендерит письмо для пользователя на его языке и ставит в очередь.
func (s *Service) send(ctx context.Context, db *gorm.DB, u auth.User, kind string, d Data) error {
	if d.Name == "" {
		d.Name = u.Username
	}
	r, err := Render(kind, u.Lang, d)
	if err != nil {
		return err
	}
	uid := u.ID
	return s.Enqueue(ctx, db, Mail{UserID: &uid, Kind: kind, To: u.Email, Subject: r.Subject, Text: r.Text, HTML: r.HTML})
}

// SendVerification отправляет ссылку для подтверждения адреса.
func (s *Service) SendVerification(ctx context.Context, u auth.User, token string) error {
	return s.send(ctx, nil, u, KindVerifyEmail, Data{Link: s.Link("/verify-email?token=" + token)})
}

// SendPasswordReset отправляет ссылку для сброса пароля.
func (s *Service) SendPasswordReset(ctx context.Context, u auth.User, token string) error {
	return s.send(ctx, nil, u, KindResetPassword, Data{Link: s.Link("/reset-password?token=" + token)})
}

// SendPasswordChanged сообщает о смене пароля (безопасность: владелец должен узнать, даже если менял не он).
func (s *Service) SendPasswordChanged(ctx context.Context, u auth.User) error {
	return s.send(ctx, nil, u, KindPasswordChanged, Data{})
}

// ---- отправка ----

// Run отправляет письма из очереди каждые every, пока не отменён ctx. Раз в час удаляет записи старше 30 суток.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	if !s.Enabled() {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	lastCleanup := time.Time{}
	for {
		s.Drain(ctx)
		if time.Since(lastCleanup) > cleanupEvery {
			lastCleanup = time.Now()
			if err := s.db.WithContext(ctx).Where("status <> 'queued' AND created_at < ?", s.now().Add(-keepFinished)).Delete(&Outbox{}).Error; err != nil && ctx.Err() == nil {
				log.Printf("почта: очистка очереди: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Drain отправляет письма, которым пора. Возвращает число обработанных.
func (s *Service) Drain(ctx context.Context) int {
	n := 0
	for i := 0; i < batchPerTick && ctx.Err() == nil; i++ {
		m, ok, err := s.claim(ctx)
		if err != nil {
			log.Printf("почта: очередь: %v", err)
			return n
		}
		if !ok {
			return n
		}
		s.deliver(ctx, m)
		n++
	}
	return n
}

// claim берёт одно письмо и «арендует» его: если процесс упадёт посреди отправки, письмо вернётся в очередь по истечении аренды.
func (s *Service) claim(ctx context.Context) (Outbox, bool, error) {
	var m Outbox
	now := s.now()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Raw(`SELECT * FROM mail_outbox WHERE status = 'queued' AND next_attempt_at <= ? ORDER BY next_attempt_at, id LIMIT 1 FOR UPDATE SKIP LOCKED`, now).Scan(&m).Error
		if err != nil || m.ID == 0 {
			return err
		}
		return tx.Model(&Outbox{}).Where("id = ?", m.ID).
			Updates(map[string]any{"attempts": gorm.Expr("attempts + 1"), "next_attempt_at": now.Add(sendLease)}).Error
	})
	if err != nil || m.ID == 0 {
		return Outbox{}, false, err
	}
	m.Attempts++
	return m, true, nil
}

func (s *Service) deliver(ctx context.Context, m Outbox) {
	err := s.sender.Send(ctx, mailer.Message{To: m.ToEmail, Subject: m.Subject, Text: m.TextBody, HTML: m.HTMLBody})
	now := s.now()
	switch {
	case err == nil:
		s.finish(ctx, m.ID, map[string]any{"status": "sent", "sent_at": now, "last_error": ""})
	case mailer.IsTemporary(err) && m.Attempts < maxAttempts:
		i := min(m.Attempts-1, len(s.retry)-1)
		s.finish(ctx, m.ID, map[string]any{"next_attempt_at": now.Add(s.retry[i]), "last_error": trim(err.Error())}, false)
	default:
		s.finish(ctx, m.ID, map[string]any{"status": "failed", "last_error": trim(err.Error())})
		log.Printf("почта: письмо %d (%s) не доставлено: %v", m.ID, m.Kind, err)
	}
}

func trim(s string) string {
	if len(s) > errorTextLimit {
		return strings.ToValidUTF8(s[:errorTextLimit], "") + "…"
	}
	return s
}

// finish сохраняет итог. Для окончательных исходов (по умолчанию) тела писем стираются: в них бывают ссылки для входа.
func (s *Service) finish(ctx context.Context, id int64, upd map[string]any, final ...bool) {
	if len(final) == 0 || final[0] {
		upd["text_body"], upd["html_body"] = "", ""
	}
	if err := s.db.WithContext(context.WithoutCancel(ctx)).Model(&Outbox{}).Where("id = ?", id).Updates(upd).Error; err != nil {
		log.Printf("почта: сохранение итога письма %d: %v", id, err)
	}
}

// Status — сводка очереди для проверок и тестов.
func (s *Service) Status(ctx context.Context) (queued, sent, failed int64, err error) {
	for status, dst := range map[string]*int64{"queued": &queued, "sent": &sent, "failed": &failed} {
		if err = s.db.WithContext(ctx).Model(&Outbox{}).Where("status = ?", status).Count(dst).Error; err != nil {
			return
		}
	}
	return
}

// Package tickets — обращения в поддержку: пользователь пишет тикет, администратор панели отвечает, обе стороны видят переписку.
// О новых обращениях и ответах сообщают письма (пакет notify, если он подключён).
package tickets

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"vladhost/internal/apperr"
	"vladhost/internal/auth"
)

// Статусы тикета.
const (
	StatusOpen     = "open"     // ждёт ответа поддержки
	StatusAnswered = "answered" // поддержка ответила, ждём пользователя
	StatusClosed   = "closed"
)

// Категории обращений.
var Categories = []string{"general", "sites", "domains", "mail", "dns", "databases", "other"}

// Пределы.
const (
	MaxOpenPerUser    = 5   // тикетов, не закрытых одновременно
	MaxMessages       = 100 // сообщений в одном тикете
	MaxSubject        = 120
	MinSubject        = 3
	MaxBody           = 5000
	MaxUserPerHour    = 20 // сообщений пользователя (в любых тикетах) за час
	ListLimit         = 200
	notifySnippetSize = 300
)

var (
	ErrNotFound   = apperr.New(http.StatusNotFound, "ticket_not_found", "ticket not found")
	ErrLimit      = apperr.New(http.StatusForbidden, "ticket_limit", "too many open tickets")
	ErrFull       = apperr.New(http.StatusForbidden, "ticket_full", "ticket has too many messages")
	ErrRate       = apperr.New(http.StatusTooManyRequests, "ticket_rate", "too many messages, try later")
	ErrSubject    = apperr.Validation("subject", "ticket_subject", "invalid subject")
	ErrBody       = apperr.Validation("message", "ticket_body", "invalid message")
	ErrCategory   = apperr.Validation("category", "ticket_category", "invalid category")
	ErrNotClosed  = apperr.New(http.StatusConflict, "ticket_not_closed", "ticket is not closed")
	ErrAlreadyOff = apperr.New(http.StatusConflict, "ticket_closed", "ticket is already closed")
)

// Ticket — обращение.
type Ticket struct {
	ID        int64      `gorm:"primaryKey" json:"id"`
	UserID    int64      `json:"-"`
	Subject   string     `json:"subject"`
	Category  string     `json:"category"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at"`
}

func (Ticket) TableName() string { return "tickets" }

// Message — сообщение в тикете.
type Message struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	TicketID  int64     `json:"-"`
	AuthorID  int64     `json:"-"`
	Staff     bool      `json:"staff"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

func (Message) TableName() string { return "ticket_messages" }

// Item — строка списка.
type Item struct {
	Ticket
	Username string `json:"username"` // автор обращения (для очереди администратора)
	Messages int    `json:"messages"`
}

// MessageView — сообщение с именем автора.
type MessageView struct {
	Message
	Author string `json:"author"`
}

// View — тикет с перепиской.
type View struct {
	Item
	Email    string        `json:"email,omitempty"` // только администратору
	Thread   []MessageView `json:"thread"`
	CanReply bool          `json:"can_reply"`
}

// Notifier — письма о тикетах (реализует notify.Service); nil — писем нет.
type Notifier interface {
	TicketNew(ctx context.Context, admins []auth.User, author auth.User, ticketID int64, subject, body string, followUp bool)
	TicketReply(ctx context.Context, owner auth.User, ticketID int64, subject, body string)
}

// Service — тикеты.
type Service struct {
	db  *gorm.DB
	now func() time.Time
	n   Notifier
}

// New создаёт службу.
func New(db *gorm.DB, n Notifier) *Service { return &Service{db: db, now: time.Now, n: n} }

func isStaff(u auth.User) bool { return u.Role == auth.RoleAdmin }

func cleanSubject(v string) (string, bool) {
	v = strings.TrimSpace(v)
	n := utf8.RuneCountInString(v)
	if n < MinSubject || n > MaxSubject || !utf8.ValidString(v) {
		return "", false
	}
	for _, r := range v {
		if r < 32 || r == 127 {
			return "", false
		}
	}
	return v, true
}

func cleanBody(v string) (string, bool) {
	v = strings.TrimSpace(strings.ReplaceAll(v, "\r\n", "\n"))
	if v == "" || utf8.RuneCountInString(v) > MaxBody || !utf8.ValidString(v) {
		return "", false
	}
	for _, r := range v {
		if (r < 32 && r != '\n' && r != '\t') || r == 127 {
			return "", false
		}
	}
	return v, true
}

func validCategory(c string) bool {
	for _, x := range Categories {
		if x == c {
			return true
		}
	}
	return false
}

func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > notifySnippetSize {
		s = string(r[:notifySnippetSize]) + "…"
	}
	return s
}

func (s *Service) admins(ctx context.Context) []auth.User {
	var out []auth.User
	_ = s.db.WithContext(ctx).Where("role = ?", auth.RoleAdmin).Find(&out).Error
	return out
}

// Create заводит тикет с первым сообщением.
func (s *Service) Create(ctx context.Context, u auth.User, subject, category, body string) (*Ticket, error) {
	subject, ok := cleanSubject(subject)
	if !ok {
		return nil, ErrSubject.With(MinSubject, MaxSubject)
	}
	if !validCategory(category) {
		return nil, ErrCategory
	}
	body, ok = cleanBody(body)
	if !ok {
		return nil, ErrBody.With(MaxBody)
	}
	t := &Ticket{UserID: u.ID, Subject: subject, Category: category, Status: StatusOpen, CreatedAt: s.now(), UpdatedAt: s.now()}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", u.ID+9_000_000).Error; err != nil {
			return err
		}
		var open int64
		if err := tx.Model(&Ticket{}).Where("user_id = ? AND status <> ?", u.ID, StatusClosed).Count(&open).Error; err != nil {
			return err
		}
		if open >= MaxOpenPerUser {
			return ErrLimit.With(MaxOpenPerUser)
		}
		if err := s.rate(tx, u.ID); err != nil {
			return err
		}
		if err := tx.Create(t).Error; err != nil {
			return err
		}
		return tx.Create(&Message{TicketID: t.ID, AuthorID: u.ID, Body: body, CreatedAt: s.now()}).Error
	})
	if err != nil {
		return nil, err
	}
	if s.n != nil && !isStaff(u) {
		s.n.TicketNew(ctx, s.admins(ctx), u, t.ID, subject, body, false)
	}
	return t, nil
}

// rate ограничивает поток сообщений пользователя.
func (s *Service) rate(tx *gorm.DB, userID int64) error {
	var n int64
	if err := tx.Model(&Message{}).Where("author_id = ? AND staff = false AND created_at > ?", userID, s.now().Add(-time.Hour)).Count(&n).Error; err != nil {
		return err
	}
	if n >= MaxUserPerHour {
		return ErrRate
	}
	return nil
}

func (s *Service) load(ctx context.Context, viewer auth.User, id int64) (*Ticket, error) {
	var t Ticket
	q := s.db.WithContext(ctx).Where("id = ?", id)
	if !isStaff(viewer) {
		q = q.Where("user_id = ?", viewer.ID)
	}
	if err := q.Take(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

type itemRow struct {
	Ticket
	Username string
	Messages int
}

func (s *Service) items(ctx context.Context, where string, args ...any) ([]Item, error) {
	var rows []itemRow
	q := s.db.WithContext(ctx).Table("tickets t").
		Select("t.*, u.username AS username, (SELECT count(*) FROM ticket_messages m WHERE m.ticket_id = t.id) AS messages").
		Joins("JOIN users u ON u.id = t.user_id")
	if where != "" {
		q = q.Where(where, args...)
	}
	if err := q.Order("t.updated_at DESC, t.id DESC").Limit(ListLimit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(rows))
	for _, r := range rows {
		out = append(out, Item{Ticket: r.Ticket, Username: r.Username, Messages: r.Messages})
	}
	return out, nil
}

// List — тикеты пользователя, свежие сверху.
func (s *Service) List(ctx context.Context, userID int64) ([]Item, error) {
	return s.items(ctx, "t.user_id = ?", userID)
}

// AdminList — очередь администратора: все тикеты, можно отфильтровать по статусу и найти по теме или имени.
func (s *Service) AdminList(ctx context.Context, viewer auth.User, status, query string) ([]Item, error) {
	if !isStaff(viewer) {
		return nil, ErrNotFound
	}
	where, args := "1=1", []any{}
	if status == StatusOpen || status == StatusAnswered || status == StatusClosed {
		where += " AND t.status = ?"
		args = append(args, status)
	}
	if q := strings.TrimSpace(query); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q)) + "%"
		where += " AND (lower(t.subject) LIKE ? OR lower(u.username) LIKE ? OR lower(u.email) LIKE ?)"
		args = append(args, like, like, like)
	}
	return s.items(ctx, where, args...)
}

// Get возвращает тикет с перепиской: владельцу или администратору.
func (s *Service) Get(ctx context.Context, viewer auth.User, id int64) (*View, error) {
	t, err := s.load(ctx, viewer, id)
	if err != nil {
		return nil, err
	}
	var owner auth.User
	if err := s.db.WithContext(ctx).Take(&owner, t.UserID).Error; err != nil {
		return nil, err
	}
	var msgs []Message
	if err := s.db.WithContext(ctx).Where("ticket_id = ?", t.ID).Order("id").Find(&msgs).Error; err != nil {
		return nil, err
	}
	names := map[int64]string{}
	v := &View{Item: Item{Ticket: *t, Username: owner.Username, Messages: len(msgs)}, Thread: make([]MessageView, 0, len(msgs)), CanReply: len(msgs) < MaxMessages}
	for _, m := range msgs {
		name, ok := names[m.AuthorID]
		if !ok {
			var a auth.User
			if err := s.db.WithContext(ctx).Take(&a, m.AuthorID).Error; err == nil {
				name = a.Username
			}
			names[m.AuthorID] = name
		}
		// пользователь видит «Поддержка», а не имя конкретного администратора
		author := name
		if m.Staff && !isStaff(viewer) {
			author = ""
		}
		v.Thread = append(v.Thread, MessageView{Message: m, Author: author})
	}
	if isStaff(viewer) {
		v.Email = owner.Email
	}
	return v, nil
}

// Reply добавляет сообщение. Ответ поддержки переводит тикет в «отвечен», сообщение владельца — в «ждёт ответа»; закрытый тикет при этом открывается снова.
func (s *Service) Reply(ctx context.Context, viewer auth.User, id int64, body string) (*Message, error) {
	body, ok := cleanBody(body)
	if !ok {
		return nil, ErrBody.With(MaxBody)
	}
	t, err := s.load(ctx, viewer, id)
	if err != nil {
		return nil, err
	}
	staff := isStaff(viewer) && t.UserID != viewer.ID // администратор, пишущий в собственный тикет, — обычный автор
	m := &Message{TicketID: t.ID, AuthorID: viewer.ID, Staff: staff, Body: body, CreatedAt: s.now()}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", t.ID+9_500_000).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&Message{}).Where("ticket_id = ?", t.ID).Count(&n).Error; err != nil {
			return err
		}
		if n >= MaxMessages {
			return ErrFull
		}
		if !staff {
			if err := s.rate(tx, viewer.ID); err != nil {
				return err
			}
		}
		if err := tx.Create(m).Error; err != nil {
			return err
		}
		status := StatusOpen
		if staff {
			status = StatusAnswered
		}
		return tx.Model(&Ticket{}).Where("id = ?", t.ID).Updates(map[string]any{"status": status, "updated_at": s.now(), "closed_at": nil}).Error
	})
	if err != nil {
		return nil, err
	}
	if s.n != nil {
		if staff {
			var owner auth.User
			if s.db.WithContext(ctx).Take(&owner, t.UserID).Error == nil {
				s.n.TicketReply(ctx, owner, t.ID, t.Subject, snippet(body))
			}
		} else if !isStaff(viewer) {
			s.n.TicketNew(ctx, s.admins(ctx), viewer, t.ID, t.Subject, snippet(body), true)
		}
	}
	return m, nil
}

// Close закрывает тикет (владелец или администратор).
func (s *Service) Close(ctx context.Context, viewer auth.User, id int64) error {
	t, err := s.load(ctx, viewer, id)
	if err != nil {
		return err
	}
	if t.Status == StatusClosed {
		return ErrAlreadyOff
	}
	return s.db.WithContext(ctx).Model(&Ticket{}).Where("id = ?", t.ID).Updates(map[string]any{"status": StatusClosed, "closed_at": s.now(), "updated_at": s.now()}).Error
}

// Reopen открывает закрытый тикет снова; у пользователя действует тот же предел открытых тикетов.
func (s *Service) Reopen(ctx context.Context, viewer auth.User, id int64) error {
	t, err := s.load(ctx, viewer, id)
	if err != nil {
		return err
	}
	if t.Status != StatusClosed {
		return ErrNotClosed
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", t.UserID+9_000_000).Error; err != nil {
			return err
		}
		var open int64
		if err := tx.Model(&Ticket{}).Where("user_id = ? AND status <> ?", t.UserID, StatusClosed).Count(&open).Error; err != nil {
			return err
		}
		if open >= MaxOpenPerUser && !isStaff(viewer) {
			return ErrLimit.With(MaxOpenPerUser)
		}
		return tx.Model(&Ticket{}).Where("id = ?", t.ID).Updates(map[string]any{"status": StatusOpen, "closed_at": nil, "updated_at": s.now()}).Error
	})
}

// Waiting — сколько тикетов ждут действия: у пользователя — с ответом поддержки, у администратора — ждущих ответа.
func (s *Service) Waiting(ctx context.Context, viewer auth.User) (int64, error) {
	var n int64
	q := s.db.WithContext(ctx).Model(&Ticket{})
	if isStaff(viewer) {
		q = q.Where("status = ?", StatusOpen)
	} else {
		q = q.Where("user_id = ? AND status = ?", viewer.ID, StatusAnswered)
	}
	err := q.Count(&n).Error
	return n, err
}

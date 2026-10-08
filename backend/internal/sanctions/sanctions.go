// Package sanctions — наказания (шаг 5.9, спецификация §8): предупреждение → временная блокировка комментариев → бан
// аккаунта, причина в журнале. Накладывает Модератор (право moderate_comments) или Директорат; нельзя наказать себя и
// Директорат. Бан закрывает вход и снимает все сессии; блокировка комментариев запрещает писать пометки на полях
// (documents.CreateRemark спрашивает CommentBlockedUntil). Всё снимается отзывом. Наказанному приходит записка (кроме бана:
// войти он уже не может).
package sanctions

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"kupol/internal/audit"
	"kupol/internal/inbox"
)

type Kind string

const (
	Warning    Kind = "warning"
	CommentBan Kind = "comment_ban"
	Ban        Kind = "ban"
)

const (
	MaxReason  = 1000
	MaxDays    = 365
	DefaultDay = 7
)

var (
	ErrNotFound    = errors.New("sanctions: не найдено")
	ErrForbidden   = errors.New("sanctions: недостаточно прав")
	ErrUserMissing = errors.New("sanctions: пользователь не найден")
	ErrProtected   = errors.New("sanctions: этого пользователя наказать нельзя")
	ErrRevoked     = errors.New("sanctions: уже снято")
)

// ValidationError — поле и сообщение (по-русски, ключ перевода).
type ValidationError struct{ Field, Message string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

// Actor — кто накладывает.
type Actor struct {
	ID          int64
	Directorate bool
	Moderator   bool // право moderate_comments
}

func (a Actor) can() bool { return a.Directorate || a.Moderator }

// Item — наказание в ответе.
type Item struct {
	ID        int64      `json:"id"`
	User      string     `json:"user"`
	Kind      Kind       `json:"kind"`
	Reason    string     `json:"reason"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	IssuedBy  *string    `json:"issued_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	// Active — действует сейчас: не снято и, если блокировка комментариев, не истекла.
	Active bool `json:"active"`
}

// Input — новое наказание. Days — срок блокировки комментариев (только для comment_ban); 0 — по умолчанию.
type Input struct {
	Login  string `json:"login"`
	Kind   Kind   `json:"kind"`
	Reason string `json:"reason"`
	Days   int    `json:"days"`
}

type Service struct {
	db  *gorm.DB
	now func() time.Time
}

func NewService(db *gorm.DB, now func() time.Time) *Service {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{db: db, now: now}
}

type joined struct {
	ID        int64
	UserLogin string
	Kind      string
	Reason    string
	ExpiresAt *time.Time
	IssuerLog *string
	CreatedAt time.Time
	RevokedAt *time.Time
}

func (s *Service) toItem(j joined) Item {
	it := Item{ID: j.ID, User: j.UserLogin, Kind: Kind(j.Kind), Reason: j.Reason, ExpiresAt: j.ExpiresAt, IssuedBy: j.IssuerLog, CreatedAt: j.CreatedAt.UTC(), RevokedAt: j.RevokedAt}
	switch it.Kind {
	case Warning:
		it.Active = false // предупреждение — запись в деле, а не действующее ограничение
	case CommentBan:
		it.Active = j.RevokedAt == nil && j.ExpiresAt != nil && j.ExpiresAt.After(s.now())
	case Ban:
		it.Active = j.RevokedAt == nil
	}
	return it
}

func (s *Service) query(tx *gorm.DB) *gorm.DB {
	return tx.Table("sanctions s").
		Select("s.id, u.login::text AS user_login, s.kind, s.reason, s.expires_at, i.login::text AS issuer_log, s.created_at, s.revoked_at").
		Joins("JOIN users u ON u.id = s.user_id").Joins("LEFT JOIN users i ON i.id = s.issued_by")
}

// Issue накладывает наказание.
func (s *Service) Issue(ctx context.Context, a Actor, in Input) (*Item, error) {
	if !a.can() {
		return nil, ErrForbidden
	}
	reason := strings.TrimSpace(in.Reason)
	switch {
	case in.Kind != Warning && in.Kind != CommentBan && in.Kind != Ban:
		return nil, &ValidationError{"kind", "Вид наказания: warning, comment_ban или ban"}
	case reason == "" || utf8.RuneCountInString(reason) > MaxReason:
		return nil, &ValidationError{"reason", "Причина: от 1 до 1000 знаков"}
	case in.Kind == CommentBan && (in.Days < 0 || in.Days > MaxDays):
		return nil, &ValidationError{"days", "Срок блокировки комментариев: от 1 до 365 дней"}
	}
	var out *Item
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var u struct {
			ID          int64
			Directorate bool
		}
		err := tx.Table("users").Select("id, directorate").Where("login = ?", strings.TrimSpace(in.Login)).Take(&u).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserMissing
		}
		if err != nil {
			return err
		}
		if u.Directorate || u.ID == a.ID {
			return ErrProtected
		}
		now := s.now()
		var expires *time.Time
		if in.Kind == CommentBan {
			days := in.Days
			if days == 0 {
				days = DefaultDay
			}
			e := now.Add(time.Duration(days) * 24 * time.Hour)
			expires = &e
		}
		var id int64
		if err := tx.Raw(`INSERT INTO sanctions (user_id, kind, reason, expires_at, issued_by, created_at) VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
			u.ID, string(in.Kind), reason, expires, a.ID, now).Scan(&id).Error; err != nil {
			return err
		}
		if in.Kind == Ban {
			if err := tx.Exec("UPDATE users SET banned_at = ? WHERE id = ? AND banned_at IS NULL", now, u.ID).Error; err != nil {
				return err
			}
			if err := tx.Exec("DELETE FROM sessions WHERE user_id = ?", u.ID).Error; err != nil {
				return err
			}
		} else {
			params := map[string]any{"kind": string(in.Kind), "reason": reason}
			if expires != nil {
				params["until"] = expires.UTC().Format(time.RFC3339)
			}
			if err := inbox.Send(ctx, tx, u.ID, inbox.KindSanction, params, now); err != nil {
				return err
			}
		}
		aid, uid := a.ID, u.ID
		if err := audit.Record(tx, now, audit.SanctionIssued, audit.Event{ActorID: &aid, TargetUserID: &uid, Details: audit.Details("kind", string(in.Kind), "reason", reason)}); err != nil {
			return err
		}
		var j joined
		if err := s.query(tx).Where("s.id = ?", id).Take(&j).Error; err != nil {
			return err
		}
		it := s.toItem(j)
		out = &it
		return nil
	})
	return out, err
}

// Revoke снимает наказание (бан — открывает вход снова, если другого действующего бана нет).
func (s *Service) Revoke(ctx context.Context, a Actor, id int64) (*Item, error) {
	if !a.can() {
		return nil, ErrForbidden
	}
	var out *Item
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var r struct {
			UserID    int64
			Kind      string
			RevokedAt *time.Time
		}
		err := tx.Table("sanctions").Where("id = ?", id).Take(&r).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if r.RevokedAt != nil {
			return ErrRevoked
		}
		now := s.now()
		if err := tx.Exec("UPDATE sanctions SET revoked_at = ?, revoked_by = ? WHERE id = ?", now, a.ID, id).Error; err != nil {
			return err
		}
		if Kind(r.Kind) == Ban {
			if err := tx.Exec(`UPDATE users SET banned_at = NULL WHERE id = ?
				AND NOT EXISTS (SELECT 1 FROM sanctions WHERE user_id = ? AND kind = 'ban' AND revoked_at IS NULL)`, r.UserID, r.UserID).Error; err != nil {
				return err
			}
		}
		aid, uid := a.ID, r.UserID
		if err := audit.Record(tx, now, audit.SanctionRevoked, audit.Event{ActorID: &aid, TargetUserID: &uid, Details: audit.Details("kind", r.Kind, "sanction_id", id)}); err != nil {
			return err
		}
		var j joined
		if err := s.query(tx).Where("s.id = ?", id).Take(&j).Error; err != nil {
			return err
		}
		it := s.toItem(j)
		out = &it
		return nil
	})
	return out, err
}

// List — последние наказания (все или одного пользователя), сначала новые.
func (s *Service) List(ctx context.Context, a Actor, login string) ([]Item, error) {
	if !a.can() {
		return nil, ErrForbidden
	}
	q := s.query(s.db.WithContext(ctx))
	if login = strings.TrimSpace(login); login != "" {
		q = q.Where("u.login = ?", login)
	}
	var rows []joined
	if err := q.Order("s.created_at DESC, s.id DESC").Limit(200).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Item, len(rows))
	for i, r := range rows {
		out[i] = s.toItem(r)
	}
	return out, nil
}

// CommentBlockedUntil — до какого момента пользователю запрещены комментарии; nil — не запрещены.
func CommentBlockedUntil(ctx context.Context, db *gorm.DB, userID int64, now time.Time) (*time.Time, error) {
	var ends []time.Time
	err := db.WithContext(ctx).Raw(`SELECT expires_at FROM sanctions
		WHERE user_id = ? AND kind = 'comment_ban' AND revoked_at IS NULL AND expires_at > ?
		ORDER BY expires_at DESC LIMIT 1`, userID, now).Scan(&ends).Error
	if err != nil || len(ends) == 0 {
		return nil, err
	}
	return &ends[0], nil
}

package auth

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vladhost/internal/apperr"
)

var ErrSessionNotFound = apperr.New(http.StatusNotFound, "session_not_found", "session not found")

// SessionRow — вход с одного устройства. Refresh-токены внутри сессии меняются при каждом обновлении, сама она остаётся.
type SessionRow struct {
	ID         int64      `gorm:"primaryKey" json:"id"`
	UserID     int64      `json:"-"`
	IP         string     `gorm:"column:ip" json:"ip"`
	UserAgent  string     `json:"user_agent"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"-"`
	Current    bool       `gorm:"-" json:"current"`
}

func (SessionRow) TableName() string { return "sessions" }

// Client — откуда пришёл запрос входа: адрес и программа попадают в список сессий.
type Client struct {
	IP        string
	UserAgent string
}

type clientKey struct{}

// WithClient кладёт сведения о клиенте в контекст запроса (их читают вход, регистрация и обновление сессии).
func WithClient(ctx context.Context, c Client) context.Context {
	return context.WithValue(ctx, clientKey{}, c)
}

func clientFrom(ctx context.Context) Client {
	c, _ := ctx.Value(clientKey{}).(Client)
	if len(c.IP) > 45 {
		c.IP = c.IP[:45]
	}
	c.UserAgent = truncateUTF8(c.UserAgent, 300)
	return c
}

func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// revokedSet — закрытые сессии, чьи access-токены ещё не истекли. Access-токен проверяется без базы, поэтому без этого списка
// закрытая сессия работала бы до конца срока токена (15 минут). Панель — один процесс, так что памяти достаточно.
type revokedSet struct {
	mu sync.Mutex
	m  map[int64]time.Time // сессия → до какого момента помнить
}

func (r *revokedSet) add(until time.Time, ids ...int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m == nil {
		r.m = map[int64]time.Time{}
	}
	for _, id := range ids {
		r.m[id] = until
	}
}

func (r *revokedSet) has(id int64, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, until := range r.m { // чистка по ходу: записей немного
		if !until.After(now) {
			delete(r.m, k)
		}
	}
	_, ok := r.m[id]
	return ok
}

// revokeSessions закрывает сессии пользователя (все или одну, кроме keep) вместе с их refresh-токенами.
// Номера закрытых сессий запоминаются, чтобы их access-токены перестали приниматься сразу.
func (s *Service) revokeSessions(tx *gorm.DB, userID, only, keep int64) error {
	now := s.now()
	q := tx.Model(&SessionRow{}).Where("user_id = ? AND revoked_at IS NULL", userID)
	if only != 0 {
		q = q.Where("id = ?", only)
	}
	if keep != 0 {
		q = q.Where("id <> ?", keep)
	}
	var ids []int64
	if err := q.Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) > 0 {
		if err := tx.Model(&SessionRow{}).Where("id IN ?", ids).Update("revoked_at", now).Error; err != nil {
			return err
		}
	}
	rt := tx.Model(&RefreshToken{}).Where("user_id = ? AND revoked_at IS NULL", userID)
	switch {
	case only != 0:
		rt = rt.Where("session_id = ?", only)
	case keep != 0:
		rt = rt.Where("session_id IS DISTINCT FROM ?", keep)
	}
	if err := rt.Update("revoked_at", now).Error; err != nil {
		return err
	}
	if len(ids) > 0 {
		s.revoked.add(now.Add(s.accessTTL), ids...)
	}
	return nil
}

// ListSessions — действующие сессии пользователя, свежие сверху; current — та, из которой пришёл запрос.
func (s *Service) ListSessions(ctx context.Context, userID, current int64) ([]SessionRow, error) {
	var out []SessionRow
	err := s.db.WithContext(ctx).Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", userID, s.now()).
		Order("last_seen_at DESC, id DESC").Limit(100).Find(&out).Error
	for i := range out {
		out[i].Current = out[i].ID == current
	}
	return out, err
}

// RevokeSession закрывает одну сессию пользователя.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID int64) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&SessionRow{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ?", sessionID, userID, s.now()).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return ErrSessionNotFound
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return s.revokeSessions(tx, userID, sessionID, 0) })
}

// RevokeOtherSessions закрывает все сессии пользователя, кроме текущей; возвращает, сколько закрыто.
func (s *Service) RevokeOtherSessions(ctx context.Context, userID, current int64) (int, error) {
	var n int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&SessionRow{}).Where("user_id = ? AND revoked_at IS NULL AND expires_at > ? AND id <> ?", userID, s.now(), current).Count(&n).Error; err != nil {
			return err
		}
		return s.revokeSessions(tx, userID, 0, cmpNonZero(current))
	})
	return int(n), err
}

// cmpNonZero: без номера текущей сессии (старый токен) «кроме текущей» не отличить от «все» — тогда закрываем все, кроме несуществующей.
func cmpNonZero(id int64) int64 {
	if id == 0 {
		return -1
	}
	return id
}

// sessionOf возвращает сессию refresh-токена (nil — токен выдан до появления сессий).
func sessionOf(tx *gorm.DB, rt RefreshToken) (*SessionRow, error) {
	if rt.SessionID == nil {
		return nil, nil
	}
	var sr SessionRow
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sr, *rt.SessionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &sr, err
}

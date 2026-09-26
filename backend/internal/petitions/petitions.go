// Package petitions — ходатайства о повышении допуска до 4–6 (шаг 5.8, спецификация §3: уровни 4–6 выдаёт Особый
// Совет). Читатель подаёт ходатайство из личного дела на уровень на единицу выше своего (не ниже 4); решает любой
// член Особого Совета (уровень 6) или Директорат, но не по собственному ходатайству. Одобрение поднимает уровень,
// обе развязки приходят читателю запиской во внутреннюю почту.
package petitions

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"kupol/internal/audit"
	"kupol/internal/inbox"
)

const (
	MaxText    = 2000
	MaxComment = 2000
	MinTarget  = 4
	MaxTarget  = 6
	CouncilLvl = 6 // Особый Совет
)

var (
	ErrNotFound     = errors.New("petitions: ходатайство не найдено")
	ErrForbidden    = errors.New("petitions: недостаточно прав")
	ErrSelf         = errors.New("petitions: своё ходатайство решает другой")
	ErrPending      = errors.New("petitions: уже есть нерассмотренное ходатайство")
	ErrNoNextLevel  = errors.New("petitions: ходатайство на следующий уровень недоступно")
	ErrAlreadyDone  = errors.New("petitions: ходатайство уже решено")
	ErrBadVerdict   = errors.New("petitions: решение — approved или rejected")
	ErrLevelChanged = errors.New("petitions: уровень автора уже изменился")
)

// ValidationError — поле и сообщение (по-русски, ключ перевода).
type ValidationError struct{ Field, Message string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

// Actor — тот, кто решает.
type Actor struct {
	ID          int64
	Level       int
	Directorate bool
}

func (a Actor) CanDecide() bool { return a.Directorate || a.Level >= CouncilLvl }

// Item — ходатайство в ответе.
type Item struct {
	ID          int64      `json:"id"`
	Author      string     `json:"author,omitempty"`
	AuthorLevel int        `json:"author_level,omitempty"`
	TargetLevel int        `json:"target_level"`
	Text        string     `json:"text"`
	Status      string     `json:"status"`
	Comment     string     `json:"comment,omitempty"`
	DecidedBy   *string    `json:"decided_by,omitempty"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
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
	ID           int64
	TargetLevel  int
	Text         string
	Status       string
	Comment      string
	DecidedAt    *time.Time
	CreatedAt    time.Time
	AuthorLogin  string
	AuthorLevel  int
	DeciderLogin *string
}

func (s *Service) query(tx *gorm.DB) *gorm.DB {
	return tx.Table("petitions p").
		Select("p.id, p.target_level, p.text, p.status, p.comment, p.decided_at, p.created_at, u.login::text AS author_login, u.level AS author_level, d.login::text AS decider_login").
		Joins("JOIN users u ON u.id = p.user_id").Joins("LEFT JOIN users d ON d.id = p.decided_by")
}

func toItem(j joined, withAuthor bool) Item {
	it := Item{ID: j.ID, TargetLevel: j.TargetLevel, Text: j.Text, Status: j.Status, Comment: j.Comment, DecidedBy: j.DeciderLogin, DecidedAt: j.DecidedAt, CreatedAt: j.CreatedAt.UTC()}
	if withAuthor {
		it.Author, it.AuthorLevel = j.AuthorLogin, j.AuthorLevel
	}
	return it
}

// Create подаёт ходатайство на уровень выше нынешнего. Доступно с уровня 3 (Сотрудник) до 5.
func (s *Service) Create(ctx context.Context, userID int64, text string) (*Item, error) {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n < 1 || n > MaxText {
		return nil, &ValidationError{"text", "Текст ходатайства: от 1 до 2000 знаков"}
	}
	var out *Item
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var level int
		if err := tx.Raw("SELECT level FROM users WHERE id = ? FOR UPDATE", userID).Scan(&level).Error; err != nil {
			return err
		}
		if target := level + 1; level < MinTarget-1 || target > MaxTarget {
			return ErrNoNextLevel
		}
		res := tx.Exec(`INSERT INTO petitions (user_id, target_level, text, created_at) VALUES (?, ?, ?, ?)`, userID, level+1, text, s.now())
		if res.Error != nil {
			if errors.Is(res.Error, gorm.ErrDuplicatedKey) {
				return ErrPending
			}
			return res.Error
		}
		var j joined
		if err := s.query(tx).Where("p.user_id = ? AND p.status = 'pending'", userID).Take(&j).Error; err != nil {
			return err
		}
		it := toItem(j, false)
		out = &it
		return nil
	})
	return out, err
}

// Mine — собственные ходатайства, сначала новые.
func (s *Service) Mine(ctx context.Context, userID int64) ([]Item, error) {
	var rows []joined
	if err := s.query(s.db.WithContext(ctx)).Where("p.user_id = ?", userID).Order("p.created_at DESC, p.id DESC").Limit(50).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Item, len(rows))
	for i, r := range rows {
		out[i] = toItem(r, false)
	}
	return out, nil
}

// Queue — очередь для Совета: нерассмотренные (старейшие первыми), затем решённые.
func (s *Service) Queue(ctx context.Context, a Actor) ([]Item, error) {
	if !a.CanDecide() {
		return nil, ErrForbidden
	}
	var rows []joined
	err := s.query(s.db.WithContext(ctx)).
		Order("(p.status = 'pending') DESC, CASE WHEN p.status = 'pending' THEN p.created_at END ASC, p.decided_at DESC NULLS LAST, p.id DESC").
		Limit(200).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]Item, len(rows))
	for i, r := range rows {
		out[i] = toItem(r, true)
	}
	return out, nil
}

// Decide выносит решение. Одобрение поднимает уровень автора до запрошенного (если его уровень с тех пор не менялся).
func (s *Service) Decide(ctx context.Context, a Actor, id int64, verdict, comment string) (*Item, error) {
	if !a.CanDecide() {
		return nil, ErrForbidden
	}
	if verdict != "approved" && verdict != "rejected" {
		return nil, ErrBadVerdict
	}
	comment = strings.TrimSpace(comment)
	if utf8.RuneCountInString(comment) > MaxComment {
		return nil, &ValidationError{"comment", "Комментарий: не больше 2000 знаков"}
	}
	var out *Item
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p struct {
			UserID      int64
			TargetLevel int
			Status      string
		}
		err := tx.Table("petitions").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&p).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if p.UserID == a.ID {
			return ErrSelf
		}
		if p.Status != "pending" {
			return ErrAlreadyDone
		}
		now := s.now()
		if verdict == "approved" {
			res := tx.Exec("UPDATE users SET level = ? WHERE id = ? AND level = ?", p.TargetLevel, p.UserID, p.TargetLevel-1)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return ErrLevelChanged
			}
		}
		if err := tx.Exec(`UPDATE petitions SET status = ?, comment = ?, decided_by = ?, decided_at = ? WHERE id = ?`, verdict, comment, a.ID, now, id).Error; err != nil {
			return err
		}
		if err := inbox.Send(ctx, tx, p.UserID, inbox.KindPetition, map[string]any{"status": verdict, "level": p.TargetLevel, "comment": comment}, now); err != nil {
			return err
		}
		aid, uid := a.ID, p.UserID
		if err := audit.Record(tx, now, audit.PetitionDecided, audit.Event{ActorID: &aid, TargetUserID: &uid, Details: audit.Details("verdict", verdict, "level", p.TargetLevel)}); err != nil {
			return err
		}
		var j joined
		if err := s.query(tx).Where("p.id = ?", id).Take(&j).Error; err != nil {
			return err
		}
		it := toItem(j, true)
		out = &it
		return nil
	})
	return out, err
}

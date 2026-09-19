package documents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// LockTTL — на сколько «взят в работу» документ; редактор продлевает замок, пока идёт правка (спецификация §6: 15 минут).
const LockTTL = 15 * time.Minute

// Lock — «документ взят в работу» (таблица document_locks).
type Lock struct {
	DocumentID int64 `gorm:"primaryKey"`
	UserID     int64
	AcquiredAt time.Time
	ExpiresAt  time.Time
}

func (Lock) TableName() string { return "document_locks" }

// LockInfo — кто правит документ сейчас.
type LockInfo struct {
	Holder     string    `json:"holder"`
	Mine       bool      `json:"mine"`
	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// LockedError — документ правит другой человек.
type LockedError struct {
	Holder    string
	ExpiresAt time.Time
}

func (e *LockedError) Error() string {
	return fmt.Sprintf("documents: документ правит %s (до %s)", e.Holder, e.ExpiresAt.Format(time.RFC3339))
}

// ErrForbidden — документ виден, но действие этому человеку не разрешено.
var ErrForbidden = errors.New("documents: недостаточно прав")

type lockRow struct {
	Lock
	HolderLogin string
}

// activeLock возвращает действующий замок документа или nil (нет замка или он просрочен).
func (s *Service) activeLock(db *gorm.DB, docID int64, a Actor) (*LockInfo, error) {
	var rows []lockRow
	err := db.Table("document_locks").
		Select("document_locks.*, users.login AS holder_login").
		Joins("JOIN users ON users.id = document_locks.user_id").
		Where("document_locks.document_id = ? AND document_locks.expires_at > ?", docID, s.now()).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	r := rows[0]
	return &LockInfo{Holder: r.HolderLogin, Mine: r.UserID == a.UserID, AcquiredAt: r.AcquiredAt.UTC(), ExpiresAt: r.ExpiresAt.UTC()}, nil
}

// acquireLock берёт замок или продлевает свой. Вызывается внутри транзакции, где строка документа уже заблокирована
// (FOR UPDATE): поэтому два человека не возьмут документ одновременно. Замок другого человека — LockedError.
func (s *Service) acquireLock(tx *gorm.DB, docID int64, a Actor) (*LockInfo, error) {
	cur, err := s.activeLock(tx, docID, a)
	if err != nil {
		return nil, err
	}
	if cur != nil && !cur.Mine {
		return nil, &LockedError{Holder: cur.Holder, ExpiresAt: cur.ExpiresAt}
	}
	now := s.now()
	acquired := now
	if cur != nil {
		acquired = cur.AcquiredAt // свой замок продлевается, время «взят» не меняется
	}
	err = tx.Exec(`INSERT INTO document_locks (document_id, user_id, acquired_at, expires_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (document_id) DO UPDATE SET user_id = EXCLUDED.user_id, acquired_at = EXCLUDED.acquired_at, expires_at = EXCLUDED.expires_at`,
		docID, a.UserID, acquired, now.Add(LockTTL)).Error
	if err != nil {
		return nil, err
	}
	return &LockInfo{Holder: a.Login, Mine: true, AcquiredAt: acquired.UTC(), ExpiresAt: now.Add(LockTTL).UTC()}, nil
}

// TakeLock «берёт документ в работу» (или продлевает свой замок). Нужно право править документ.
func (s *Service) TakeLock(ctx context.Context, a Actor, docID int64) (*LockInfo, error) {
	var out *LockInfo
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.lockedDoc(tx, a, docID, true); err != nil {
			return err
		}
		var err error
		out, err = s.acquireLock(tx, docID, a)
		return err
	})
	return out, err
}

// ReleaseLock снимает замок. Свой снимает любой; чужой — Редактор и Директорат (например, если человек ушёл и не снял).
// Снять то, чего нет или что уже просрочено, — не ошибка.
func (s *Service) ReleaseLock(ctx context.Context, a Actor, docID int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.lockedDoc(tx, a, docID, false); err != nil {
			return err
		}
		cur, err := s.activeLock(tx, docID, a)
		if err != nil {
			return err
		}
		if cur == nil {
			return tx.Where("document_id = ?", docID).Delete(&Lock{}).Error // убрать просроченную строку
		}
		if !cur.Mine && !a.CanBreakLocks() {
			return ErrForbidden
		}
		if !cur.Mine {
			s.log.Info("замок документа снят не владельцем", "document_id", docID, "holder", cur.Holder, "by", a.Login)
		}
		return tx.Where("document_id = ?", docID).Delete(&Lock{}).Error
	})
}

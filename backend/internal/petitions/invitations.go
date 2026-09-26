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

// Приглашения Совета: член Особого Совета или Директорат приглашает читателя уровня 3–5 на следующий уровень без
// ходатайства. Читатель принимает (уровень поднимается сразу, нерассмотренное ходатайство закрывается) или отклоняет;
// Совет может отозвать нерассмотренное приглашение. Приглашение и его исход — записки во внутренней почте.

var (
	ErrInvNotFound = errors.New("petitions: приглашение не найдено")
	ErrUserMissing = errors.New("petitions: пользователь не найден")
	ErrInvDone     = errors.New("petitions: приглашение уже закрыто")
	ErrInvSelf     = errors.New("petitions: себя пригласить нельзя")
)

// Invitation — приглашение в ответе.
type Invitation struct {
	ID          int64      `json:"id"`
	User        string     `json:"user,omitempty"`
	TargetLevel int        `json:"target_level"`
	Message     string     `json:"message,omitempty"`
	Status      string     `json:"status"`
	InvitedBy   *string    `json:"invited_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	AnsweredAt  *time.Time `json:"answered_at,omitempty"`
}

type invJoined struct {
	ID          int64
	UserLogin   string
	TargetLevel int
	Message     string
	Status      string
	InviterLog  *string
	CreatedAt   time.Time
	AnsweredAt  *time.Time
}

func invItem(j invJoined, withUser bool) Invitation {
	it := Invitation{ID: j.ID, TargetLevel: j.TargetLevel, Message: j.Message, Status: j.Status, InvitedBy: j.InviterLog, CreatedAt: j.CreatedAt.UTC(), AnsweredAt: j.AnsweredAt}
	if withUser {
		it.User = j.UserLogin
	}
	return it
}

func (s *Service) invQuery(tx *gorm.DB) *gorm.DB {
	return tx.Table("invitations i").
		Select("i.id, u.login::text AS user_login, i.target_level, i.message, i.status, b.login::text AS inviter_log, i.created_at, i.answered_at").
		Joins("JOIN users u ON u.id = i.user_id").Joins("LEFT JOIN users b ON b.id = i.invited_by")
}

// Invite приглашает читателя (по логину) на уровень на единицу выше его нынешнего.
func (s *Service) Invite(ctx context.Context, a Actor, login, message string) (*Invitation, error) {
	if !a.CanDecide() {
		return nil, ErrForbidden
	}
	message = strings.TrimSpace(message)
	if utf8.RuneCountInString(message) > MaxComment {
		return nil, &ValidationError{"message", "Комментарий: не больше 2000 знаков"}
	}
	var out *Invitation
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var u struct {
			ID          int64
			Level       int
			Directorate bool
		}
		err := tx.Table("users").Select("id, level, directorate").Where("login = ?", strings.TrimSpace(login)).Take(&u).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserMissing
		}
		if err != nil {
			return err
		}
		if u.ID == a.ID {
			return ErrInvSelf
		}
		if u.Directorate || u.Level < MinTarget-1 || u.Level+1 > MaxTarget {
			return ErrNoNextLevel
		}
		now := s.now()
		if err := tx.Exec(`INSERT INTO invitations (user_id, target_level, message, invited_by, created_at) VALUES (?, ?, ?, ?, ?)`,
			u.ID, u.Level+1, message, a.ID, now).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrPending
			}
			return err
		}
		var id int64
		if err := tx.Raw("SELECT id FROM invitations WHERE user_id = ? AND status = 'pending'", u.ID).Scan(&id).Error; err != nil {
			return err
		}
		if err := inbox.Send(ctx, tx, u.ID, inbox.KindInvitation, map[string]any{"level": u.Level + 1, "message": message}, now); err != nil {
			return err
		}
		aid, uid := a.ID, u.ID
		if err := audit.Record(tx, now, audit.InvitationSent, audit.Event{ActorID: &aid, TargetUserID: &uid, Details: audit.Details("level", u.Level+1)}); err != nil {
			return err
		}
		var j invJoined
		if err := s.invQuery(tx).Where("i.id = ?", id).Take(&j).Error; err != nil {
			return err
		}
		it := invItem(j, true)
		out = &it
		return nil
	})
	return out, err
}

// MyInvitations — приглашения читателя, сначала новые.
func (s *Service) MyInvitations(ctx context.Context, userID int64) ([]Invitation, error) {
	var rows []invJoined
	if err := s.invQuery(s.db.WithContext(ctx)).Where("i.user_id = ?", userID).Order("i.created_at DESC, i.id DESC").Limit(50).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Invitation, len(rows))
	for i, r := range rows {
		out[i] = invItem(r, false)
	}
	return out, nil
}

// Invitations — приглашения для Совета: нерассмотренные первыми.
func (s *Service) Invitations(ctx context.Context, a Actor) ([]Invitation, error) {
	if !a.CanDecide() {
		return nil, ErrForbidden
	}
	var rows []invJoined
	if err := s.invQuery(s.db.WithContext(ctx)).Order("(i.status = 'pending') DESC, i.created_at DESC, i.id DESC").Limit(200).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Invitation, len(rows))
	for i, r := range rows {
		out[i] = invItem(r, true)
	}
	return out, nil
}

// Respond — ответ читателя (accept — принять, иначе отклонить) или отзыв Советом (withdraw).
func (s *Service) Respond(ctx context.Context, a Actor, id int64, answer string) (*Invitation, error) {
	if answer != "accept" && answer != "decline" && answer != "withdraw" {
		return nil, &ValidationError{"answer", "Ответ: accept, decline или withdraw"}
	}
	var out *Invitation
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var inv struct {
			UserID      int64
			TargetLevel int
			Status      string
			InvitedBy   *int64
		}
		err := tx.Table("invitations").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&inv).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvNotFound
		}
		if err != nil {
			return err
		}
		mine := inv.UserID == a.ID
		if answer == "withdraw" {
			if !a.CanDecide() {
				return ErrForbidden
			}
		} else if !mine {
			return ErrInvNotFound // чужое приглашение неотличимо от несуществующего
		}
		if inv.Status != "pending" {
			return ErrInvDone
		}
		now := s.now()
		status := map[string]string{"accept": "accepted", "decline": "declined", "withdraw": "withdrawn"}[answer]
		if answer == "accept" {
			res := tx.Exec("UPDATE users SET level = ? WHERE id = ? AND level = ?", inv.TargetLevel, inv.UserID, inv.TargetLevel-1)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return ErrLevelChanged
			}
			// ходатайство на этот уровень становится ненужным
			if err := tx.Exec(`UPDATE petitions SET status = 'approved', decided_by = ?, decided_at = ? WHERE user_id = ? AND status = 'pending'`,
				inv.InvitedBy, now, inv.UserID).Error; err != nil {
				return err
			}
			aid := a.ID
			if err := audit.Record(tx, now, audit.InvitationAccepted, audit.Event{ActorID: &aid, TargetUserID: &aid, Details: audit.Details("level", inv.TargetLevel)}); err != nil {
				return err
			}
		}
		if err := tx.Exec("UPDATE invitations SET status = ?, answered_at = ? WHERE id = ?", status, now, id).Error; err != nil {
			return err
		}
		if inv.InvitedBy != nil && answer != "withdraw" {
			if err := inbox.Send(ctx, tx, *inv.InvitedBy, inbox.KindInvitationAnswer, map[string]any{"status": status, "level": inv.TargetLevel}, now); err != nil {
				return err
			}
		}
		var j invJoined
		if err := s.invQuery(tx).Where("i.id = ?", id).Take(&j).Error; err != nil {
			return err
		}
		it := invItem(j, true)
		out = &it
		return nil
	})
	return out, err
}

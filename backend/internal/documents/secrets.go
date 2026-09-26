package documents

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"kupol/internal/achievements"
	"kupol/internal/audit"
	"kupol/internal/xp"
)

// Скрытые коды (пасхалки, спецификация §4/§6, шаг 5.5). Код ведёт на существующий документ — целиком
// (обычная видимость документа при этом обходится: код открывает его аккаунту, даже если тот недостижим
// по уровню или статусу) или, необязательно, с якорем на блок внутри него (интерфейс докручивает к
// месту, видимость самого блока — по обычным правилам, отдельного секретного per-block доступа нет).
// Заводит и удаляет коды только Директорат; погашает — любой вошедший, один раз на аккаунт.

// ErrCodeAlreadyRedeemed — этот аккаунт уже погасил этот код раньше.
var ErrCodeAlreadyRedeemed = errors.New("documents: код уже использован")

const (
	maxSecretCodeLen = 64
	maxSecretCodeXP  = 500
)

// SecretCode — заведённый код (таблица secret_codes).
type SecretCode struct {
	ID         int64 `gorm:"primaryKey"`
	Code       string
	DocumentID int64
	BlockID    *string
	RewardXP   int
	Note       string
	CreatedAt  time.Time `gorm:"autoCreateTime:false"`
}

func (SecretCode) TableName() string { return "secret_codes" }

// SecretCodeOut — код в ответе Директорату (для страницы управления).
type SecretCodeOut struct {
	ID            int64  `json:"id"`
	Code          string `json:"code"`
	DocumentRef   string `json:"document_ref"`
	DocumentTitle string `json:"document_title"`
	BlockID       string `json:"block_id,omitempty"`
	RewardXP      int    `json:"reward_xp"`
	Note          string `json:"note"`
}

// SecretCodeInput — новый код.
type SecretCodeInput struct {
	Code        string `json:"code"`
	DocumentRef string `json:"document_ref"`
	BlockID     string `json:"block_id,omitempty"`
	RewardXP    int    `json:"reward_xp"`
	Note        string `json:"note,omitempty"`
}

func (a Actor) canManageSecrets() bool { return a.Directorate }

// SecretCodeList — все заведённые коды. Только Директорат.
func (s *Service) SecretCodeList(ctx context.Context, a Actor) ([]SecretCodeOut, error) {
	if !a.canManageSecrets() {
		return nil, ErrForbidden
	}
	var rows []struct {
		SecretCode
		DocumentCode  *string
		DocumentTitle string
	}
	err := s.db.WithContext(ctx).Table("secret_codes c").
		Select("c.*, d.code AS document_code, d.title AS document_title").
		Joins("JOIN documents d ON d.id = c.document_id").
		Order("c.created_at DESC, c.id DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]SecretCodeOut, len(rows))
	for i, r := range rows {
		out[i] = secretCodeOut(r.SecretCode, deref(r.DocumentCode), r.DocumentTitle)
	}
	return out, nil
}

func secretCodeOut(c SecretCode, documentRef, documentTitle string) SecretCodeOut {
	out := SecretCodeOut{
		ID: c.ID, Code: c.Code, DocumentRef: documentRef, DocumentTitle: documentTitle,
		RewardXP: c.RewardXP, Note: c.Note,
	}
	if c.BlockID != nil {
		out.BlockID = *c.BlockID
	}
	return out
}

// SecretCodeCreate заводит код. Только Директорат.
func (s *Service) SecretCodeCreate(ctx context.Context, a Actor, in SecretCodeInput) (*SecretCodeOut, error) {
	if !a.canManageSecrets() {
		return nil, ErrForbidden
	}
	var p Problems
	code := strings.TrimSpace(in.Code)
	p.text("code", code, 1, maxSecretCodeLen)
	if in.RewardXP < 0 || in.RewardXP > maxSecretCodeXP {
		p.Add("reward_xp", "награда — от 0 до %d XP", maxSecretCodeXP)
	}
	p.text("note", in.Note, 0, 500)

	d, err := s.byRef(ctx, in.DocumentRef)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			p.Add("document_ref", "%q — не шифр существующего документа", in.DocumentRef)
		} else {
			return nil, err
		}
	}

	var blockID *string
	if bid := strings.TrimSpace(in.BlockID); bid != "" && d != nil {
		found := false
		for _, b := range d.Blocks {
			if b.ID == bid {
				found = true
				break
			}
		}
		if !found {
			p.Add("block_id", "в документе %s нет блока %q", in.DocumentRef, bid)
		} else {
			blockID = &bid
		}
	}

	if len(p.list) > 0 {
		return nil, &ValidationError{Problems: p.List()}
	}

	now := s.now()
	c := &SecretCode{Code: code, DocumentID: d.ID, BlockID: blockID, RewardXP: in.RewardXP, Note: strings.TrimSpace(in.Note), CreatedAt: now}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(c).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return oneProblem("code", "такой код уже существует")
			}
			return err
		}
		uid := a.UserID
		return audit.Record(tx, now, audit.SecretCodeCreated, audit.Event{
			ActorID: &uid, DocumentID: &d.ID, Details: audit.Details("code", c.Code, "reward_xp", c.RewardXP),
		})
	})
	if err != nil {
		return nil, err
	}
	out := secretCodeOut(*c, deref(d.Code), d.Title)
	return &out, nil
}

// SecretCodeDelete удаляет код. Только Директорат.
func (s *Service) SecretCodeDelete(ctx context.Context, a Actor, id int64) error {
	if !a.canManageSecrets() {
		return ErrForbidden
	}
	now := s.now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c SecretCode
		if err := tx.Take(&c, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if err := tx.Delete(&SecretCode{}, id).Error; err != nil {
			return err
		}
		uid := a.UserID
		return audit.Record(tx, now, audit.SecretCodeDeleted, audit.Event{
			ActorID: &uid, DocumentID: &c.DocumentID, Details: audit.Details("code", c.Code),
		})
	})
}

// RedeemResult — итог погашения кода: куда идти и сколько дали XP.
type RedeemResult struct {
	DocumentRef string `json:"document_ref"`
	Slug        string `json:"slug"`
	BlockID     string `json:"block_id,omitempty"`
	AwardedXP   int    `json:"awarded_xp"`
	XP          int    `json:"xp"`
	Level       int    `json:"level"`
	// NewAchievements — грамоты, выданные этим погашением (шаг 5.6): secret_finder.
	NewAchievements []achievements.Kind `json:"new_achievements,omitempty"`
}

// RedeemCode погашает код для вошедшего пользователя: открывает ему документ (см. hasDocumentUnlock в
// read.go) и один раз начисляет XP. Неверный код и код на несуществующий документ неотличимы (ErrNotFound):
// перебор кодов не должен ничего раскрывать. Повторное погашение своим же аккаунтом — ErrCodeAlreadyRedeemed.
func (s *Service) RedeemCode(ctx context.Context, v Viewer, raw string) (*RedeemResult, error) {
	if v.UserID == 0 {
		return nil, ErrForbidden
	}
	code := strings.TrimSpace(raw)
	if code == "" {
		return nil, oneProblem("code", "введите код")
	}

	var res *RedeemResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row struct {
			SecretCode
			DocCode string
			DocSlug string
		}
		err := tx.Table("secret_codes c").Select("c.*, d.code AS doc_code, d.slug AS doc_slug").
			Joins("JOIN documents d ON d.id = c.document_id").
			Where("c.code = ?", code).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		now := s.now()
		insert := tx.Exec(`INSERT INTO secret_code_redemptions (user_id, code_id, redeemed_at) VALUES (?, ?, ?)
			ON CONFLICT DO NOTHING`, v.UserID, row.ID, now)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			return ErrCodeAlreadyRedeemed
		}

		xr, err := xp.Award(ctx, tx, v.UserID, xp.SourceSecretCode, row.RewardXP, xp.SecretCodeDailyCap, now)
		if err != nil {
			return err
		}
		if err := audit.Record(tx, now, audit.SecretCodeRedeemed, audit.Event{
			ActorID: &v.UserID, DocumentID: &row.DocumentID, Details: audit.Details("code", row.Code, "awarded_xp", xr.Awarded),
		}); err != nil {
			return err
		}

		newAch, err := achievements.Grant(ctx, tx, v.UserID, achievements.SecretFinder, now)
		if err != nil {
			return err
		}

		res = &RedeemResult{DocumentRef: row.DocCode, Slug: row.DocSlug, AwardedXP: xr.Awarded, XP: xr.XP, Level: xr.Level, NewAchievements: newAch}
		if row.BlockID != nil {
			res.BlockID = *row.BlockID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// hasDocumentUnlock — погасил ли пользователь код, открывающий документ целиком (не блок-якорь).
func (s *Service) hasDocumentUnlock(ctx context.Context, userID, docID int64) (bool, error) {
	if userID == 0 {
		return false, nil
	}
	var n int64
	err := s.db.WithContext(ctx).Raw(`
		SELECT count(*) FROM secret_code_redemptions r
		JOIN secret_codes c ON c.id = r.code_id
		WHERE r.user_id = ? AND c.document_id = ? AND c.block_id IS NULL`, userID, docID).Scan(&n).Error
	return n > 0, err
}

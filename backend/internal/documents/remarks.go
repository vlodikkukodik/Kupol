package documents

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"kupol/internal/xp"
)

// «Пометки на полях» — комментарии читателей под документом (шаг 5.2, спецификация §8): ветки с ответами, с уровня 1
// (Гражданин не пишет, только читает), публикуются сразу без очереди на проверку. Видимость треда не зависит от
// уровня — если читатель видит документ, он видит все его пометки целиком; закрытых пометок не бывает.

const (
	minRemarkLen       = 1
	maxRemarkLen       = 2000
	maxReportedRemarks = 200
)

// ErrRemarkNotFound — нет такой пометки (или она под другим документом).
var ErrRemarkNotFound = errors.New("documents: пометка не найдена")

type remarkRow struct {
	ID          int64 `gorm:"primaryKey"`
	DocumentID  int64
	ParentID    *int64
	AuthorID    int64
	Text        string
	CreatedAt   time.Time
	ReportCount int
}

func (remarkRow) TableName() string { return "remarks" }

// RemarkOut — пометка в ответе читателю.
type RemarkOut struct {
	ID        int64     `json:"id"`
	ParentID  *int64    `json:"parent_id,omitempty"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	// CanDelete — вправе ли читатель удалить эту пометку (автор или модератор).
	CanDelete bool `json:"can_delete"`
	// ReportCount, DocumentCode, DocumentSlug — только в списке жалоб модератору (ReportedRemarks); в обычном
	// треде под документом были бы лишними (документ и так один, число жалоб читателю знать незачем).
	ReportCount  int    `json:"report_count,omitempty"`
	DocumentCode string `json:"document_code,omitempty"`
	DocumentSlug string `json:"document_slug,omitempty"`
}

// RemarkInput — новая пометка или ответ (ParentID — id пометки, на которую отвечают).
type RemarkInput struct {
	ParentID *int64
	Text     string
}

// resolveVisibleDocument находит документ по шифру и проверяет допуск читателя v — те же правила, что и Get()
// (неопубликованное видит только Директорат; выше допуска — ErrNotFound или AccessDeniedError по флагу документа).
func (s *Service) resolveVisibleDocument(ctx context.Context, v Viewer, ref string) (*Document, error) {
	c, err := ParseCode(ref)
	if err != nil {
		return nil, ErrNotFound
	}
	var d Document
	err = s.db.WithContext(ctx).Where("slug = ?", c.Slug).Take(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if d.Status != string(StatusPublished) && !v.SeesUnpublished() {
		return nil, ErrNotFound
	}
	if d.Level > v.Level() {
		if DirectLink(d.DirectLink) == DirectLinkForbidden {
			return nil, &AccessDeniedError{RequiredLevel: d.Level}
		}
		return nil, ErrNotFound
	}
	return &d, nil
}

// ListRemarks — весь тред под документом, от старых к новым (ветки собирает интерфейс по ParentID).
func (s *Service) ListRemarks(ctx context.Context, v Viewer, ref string) ([]RemarkOut, error) {
	d, err := s.resolveVisibleDocument(ctx, v, ref)
	if err != nil {
		return nil, err
	}
	type row struct {
		ID        int64
		ParentID  *int64
		AuthorID  int64
		Text      string
		CreatedAt time.Time
		Login     string
	}
	var rows []row
	err = s.db.WithContext(ctx).Table("remarks r").
		Select("r.id, r.parent_id, r.author_id, r.text, r.created_at, u.login AS login").
		Joins("JOIN users u ON u.id = r.author_id").
		Where("r.document_id = ?", d.ID).
		Order("r.created_at, r.id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]RemarkOut, len(rows))
	for i, r := range rows {
		out[i] = RemarkOut{
			ID: r.ID, ParentID: r.ParentID, Author: r.Login, Text: r.Text, CreatedAt: r.CreatedAt.UTC(),
			CanDelete: r.AuthorID == v.UserID || v.ModerateComments,
		}
	}
	return out, nil
}

// CreateRemark публикует пометку сразу (без очереди) и начисляет XP (до xp.CommentDailyCap пометок в день).
// Уровень 1 — это и есть «вошёл»: Гражданин (уровень 0, без входа) писать не может.
func (s *Service) CreateRemark(ctx context.Context, v Viewer, ref string, in RemarkInput) (*RemarkOut, error) {
	if v.UserID == 0 {
		return nil, ErrForbidden
	}
	d, err := s.resolveVisibleDocument(ctx, v, ref)
	if err != nil {
		return nil, err
	}
	if d.Status != string(StatusPublished) {
		return nil, ErrForbidden
	}
	text := strings.TrimSpace(in.Text)
	if n := utf8.RuneCountInString(text); n < minRemarkLen || n > maxRemarkLen {
		return nil, oneProblem("text", "Пометка: от %d до %d знаков", minRemarkLen, maxRemarkLen)
	}
	if word, bad := firstBannedWord(text); bad {
		return nil, oneProblem("text", "В тексте есть запрещённое слово: «%s»", word)
	}
	if in.ParentID != nil {
		var parent remarkRow
		err := s.db.WithContext(ctx).Where("id = ? AND document_id = ?", *in.ParentID, d.ID).Take(&parent).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRemarkNotFound
		}
		if err != nil {
			return nil, err
		}
	}

	now := s.now()
	row := remarkRow{DocumentID: d.ID, ParentID: in.ParentID, AuthorID: v.UserID, Text: text, CreatedAt: now}
	var login string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if _, err := xp.Award(ctx, tx, v.UserID, xp.SourceComment, xp.CommentXP, xp.CommentDailyCap, now); err != nil {
			return err
		}
		return tx.Raw("SELECT login FROM users WHERE id = ?", v.UserID).Scan(&login).Error
	})
	if err != nil {
		return nil, err
	}
	return &RemarkOut{ID: row.ID, ParentID: row.ParentID, Author: login, Text: row.Text, CreatedAt: row.CreatedAt.UTC(), CanDelete: true}, nil
}

// ReportRemark — «Жалоба»: одна на пометку от одного читателя (повторная ничего не меняет).
func (s *Service) ReportRemark(ctx context.Context, v Viewer, id int64) error {
	if v.UserID == 0 {
		return ErrForbidden
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var exists int64
		if err := tx.Model(&remarkRow{}).Where("id = ?", id).Count(&exists).Error; err != nil {
			return err
		}
		if exists == 0 {
			return ErrRemarkNotFound
		}
		res := tx.Exec("INSERT INTO remark_reports (remark_id, user_id, created_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING", id, v.UserID, s.now())
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // уже жаловался
		}
		return tx.Exec("UPDATE remarks SET report_count = report_count + 1 WHERE id = ?", id).Error
	})
}

// DeleteRemark — автор удаляет свою пометку в любое время; модератор (или Директорат) — чужую.
func (s *Service) DeleteRemark(ctx context.Context, v Viewer, id int64) error {
	if v.UserID == 0 {
		return ErrForbidden
	}
	var row remarkRow
	if err := s.db.WithContext(ctx).Take(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRemarkNotFound
		}
		return err
	}
	if row.AuthorID != v.UserID && !v.ModerateComments {
		return ErrForbidden
	}
	return s.db.WithContext(ctx).Delete(&remarkRow{}, id).Error
}

// ReportedRemarks — очередь жалоб для модератора: по числу жалоб, затем по давности.
func (s *Service) ReportedRemarks(ctx context.Context, v Viewer) ([]RemarkOut, error) {
	if !v.ModerateComments {
		return nil, ErrForbidden
	}
	type row struct {
		ID          int64
		ParentID    *int64
		Text        string
		CreatedAt   time.Time
		ReportCount int
		Login       string
		DocCode     *string
		DocSlug     *string
	}
	var rows []row
	err := s.db.WithContext(ctx).Table("remarks r").
		Select("r.id, r.parent_id, r.text, r.created_at, r.report_count, u.login AS login, d.code AS doc_code, d.slug AS doc_slug").
		Joins("JOIN users u ON u.id = r.author_id").
		Joins("JOIN documents d ON d.id = r.document_id").
		Where("r.report_count > 0").
		Order("r.report_count DESC, r.created_at").Limit(maxReportedRemarks).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]RemarkOut, len(rows))
	for i, r := range rows {
		out[i] = RemarkOut{
			ID: r.ID, ParentID: r.ParentID, Author: r.Login, Text: r.Text, CreatedAt: r.CreatedAt.UTC(),
			CanDelete: true, ReportCount: r.ReportCount, DocumentCode: deref(r.DocCode), DocumentSlug: deref(r.DocSlug),
		}
	}
	return out, nil
}

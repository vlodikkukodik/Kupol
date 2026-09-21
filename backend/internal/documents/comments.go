package documents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// Комментарии рецензента и ход рецензии (team panel, шаг 3.5).
//
// Комментарий привязан к блоку по его идентификатору (или к документу целиком) и к редакции, на которую написан. Писать
// комментарии вправе тот, кто выносит вердикт, пока документ на проверке; отметить «исправлено» — рецензент и автор документа.
// Комментарии и ход рецензии остаются в истории документа после вердикта: автору есть что исправлять, а рецензенту —
// что перепроверить при повторной отправке.

// ErrCommentNotFound — нет такого комментария у этого документа.
var ErrCommentNotFound = errors.New("documents: комментарий не найден")

// Comment — комментарий рецензента (таблица review_comments).
type Comment struct {
	ID         int64 `gorm:"primaryKey"`
	DocumentID int64
	BlockID    *string
	Revision   int
	AuthorID   *int64
	Body       string
	ResolvedAt *time.Time
	ResolvedBy *int64
	CreatedAt  time.Time `gorm:"autoCreateTime:false"`
}

func (Comment) TableName() string { return "review_comments" }

// CommentOut — комментарий в ответе.
type CommentOut struct {
	ID         int64      `json:"id"`
	BlockID    *string    `json:"block_id,omitempty"`
	Revision   int        `json:"revision"`
	Author     *string    `json:"author,omitempty"`
	Body       string     `json:"body"`
	Resolved   bool       `json:"resolved"`
	ResolvedBy *string    `json:"resolved_by,omitempty"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	// CanResolve — вправе ли человек отметить комментарий исправленным или вернуть в открытые.
	CanResolve bool `json:"can_resolve"`
	// CanDelete — вправе ли человек удалить комментарий (автор комментария и Директорат).
	CanDelete bool `json:"can_delete"`
}

// ReviewEventOut — событие хода рецензии в ответе.
type ReviewEventOut struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	KindName  string    `json:"kind_name"`
	Revision  int       `json:"revision"`
	Actor     *string   `json:"actor,omitempty"`
	Comment   string    `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ReviewInfo — вся рецензия документа: ход (старые сверху) и комментарии.
type ReviewInfo struct {
	Events   []ReviewEventOut `json:"events"`
	Comments []CommentOut     `json:"comments"`
	// Open — сколько комментариев ещё не отмечено исправленными.
	Open int `json:"open"`
}

// canResolve — вправе ли человек отмечать комментарии к документу исправленными.
func (a Actor) canResolve(d *Document) bool {
	return a.Directorate || a.CanReview || (a.owns(d) && a.CanWrite)
}

type commentRow struct {
	Comment
	AuthorLogin   *string
	ResolvedLogin *string
}

func (s *Service) commentRows(db *gorm.DB, docID int64, commentID int64) ([]commentRow, error) {
	q := db.Table("review_comments c").
		Select("c.*, a.login::text AS author_login, r.login::text AS resolved_login").
		Joins("LEFT JOIN users a ON a.id = c.author_id").
		Joins("LEFT JOIN users r ON r.id = c.resolved_by").
		Where("c.document_id = ?", docID).Order("c.id")
	if commentID > 0 {
		q = q.Where("c.id = ?", commentID)
	}
	var rows []commentRow
	return rows, q.Scan(&rows).Error
}

func commentOut(a Actor, d *Document, r commentRow) CommentOut {
	out := CommentOut{
		ID: r.ID, BlockID: r.BlockID, Revision: r.Revision, Author: r.AuthorLogin, Body: r.Body,
		Resolved: r.ResolvedAt != nil, ResolvedBy: r.ResolvedLogin, CreatedAt: r.CreatedAt.UTC(),
		CanResolve: a.canResolve(d),
		CanDelete:  a.Directorate || (r.AuthorID != nil && *r.AuthorID == a.UserID),
	}
	if r.ResolvedAt != nil {
		t := r.ResolvedAt.UTC()
		out.ResolvedAt = &t
	}
	return out
}

// TeamReview возвращает ход рецензии и комментарии. Видит тот, кто видит документ в team panel.
func (s *Service) TeamReview(ctx context.Context, a Actor, id int64) (*ReviewInfo, error) {
	db := s.db.WithContext(ctx)
	d, err := s.viewable(ctx, db, a, id)
	if err != nil {
		return nil, err
	}
	info := &ReviewInfo{Events: []ReviewEventOut{}, Comments: []CommentOut{}}

	var events []struct {
		ReviewEvent
		ActorLogin *string
	}
	err = db.Table("review_events e").
		Select("e.*, u.login::text AS actor_login").
		Joins("LEFT JOIN users u ON u.id = e.actor_id").
		Where("e.document_id = ?", id).Order("e.id").Scan(&events).Error
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		info.Events = append(info.Events, ReviewEventOut{
			ID: e.ID, Kind: e.Kind, KindName: ReviewKind(e.Kind).NameIn(a.Lang), Revision: e.Revision, Actor: e.ActorLogin, Comment: e.Comment, CreatedAt: e.CreatedAt.UTC(),
		})
	}

	rows, err := s.commentRows(db, id, 0)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		c := commentOut(a, d, r)
		if !c.Resolved {
			info.Open++
		}
		info.Comments = append(info.Comments, c)
	}
	return info, nil
}

// checkBody приводит текст комментария в порядок и проверяет длину.
func checkBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	switch {
	case body == "":
		return "", oneProblem("body", "Напишите комментарий")
	case utf8.RuneCountInString(body) > MaxReviewText:
		return "", oneProblem("body", "слишком длинный комментарий (не больше %d знаков)", MaxReviewText)
	}
	return body, nil
}

// AddComment добавляет комментарий к блоку (blockID) или к документу целиком (blockID пуст). Пока документ на проверке —
// тому, кто выносит вердикт (не по своему документу).
func (s *Service) AddComment(ctx context.Context, a Actor, id int64, blockID *string, body string) (*CommentOut, error) {
	body, err := checkBody(body)
	if err != nil {
		return nil, err
	}
	var out *CommentOut
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		d, err := s.lockedDoc(tx, a, id, false)
		if err != nil {
			return err
		}
		if err := a.authorize(d, ActionReview); err != nil {
			return err
		}
		if blockID != nil {
			if *blockID == "" {
				blockID = nil
			} else if !hasBlock(d, *blockID) {
				return oneProblem("block_id", "в документе нет блока %q", *blockID)
			}
		}
		uid := a.UserID
		c := &Comment{DocumentID: d.ID, BlockID: blockID, Revision: d.Revision, AuthorID: &uid, Body: body, CreatedAt: s.now()}
		if err := tx.Create(c).Error; err != nil {
			return err
		}
		rows, err := s.commentRows(tx, d.ID, c.ID)
		if err != nil || len(rows) != 1 {
			return fmt.Errorf("комментарий не найден после записи: %w", err)
		}
		o := commentOut(a, d, rows[0])
		out = &o
		return nil
	})
	return out, err
}

func hasBlock(d *Document, id string) bool {
	for _, b := range d.Blocks {
		if b.ID == id {
			return true
		}
	}
	return false
}

// SetCommentResolved отмечает комментарий исправленным (resolved) или возвращает в открытые.
func (s *Service) SetCommentResolved(ctx context.Context, a Actor, id, commentID int64, resolved bool) (*CommentOut, error) {
	var out *CommentOut
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		d, err := s.lockedDoc(tx, a, id, false)
		if err != nil {
			return err
		}
		if !a.canResolve(d) {
			return ErrForbidden
		}
		rows, err := s.commentRows(tx, d.ID, commentID)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return ErrCommentNotFound
		}
		update := map[string]any{"resolved_at": nil, "resolved_by": nil}
		if resolved {
			update = map[string]any{"resolved_at": s.now(), "resolved_by": a.UserID}
		}
		if err := tx.Model(&Comment{}).Where("id = ?", commentID).Updates(update).Error; err != nil {
			return err
		}
		rows, err = s.commentRows(tx, d.ID, commentID)
		if err != nil {
			return err
		}
		o := commentOut(a, d, rows[0])
		out = &o
		return nil
	})
	return out, err
}

// DeleteComment удаляет комментарий: свой — автор комментария, любой — Директорат.
func (s *Service) DeleteComment(ctx context.Context, a Actor, id, commentID int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		d, err := s.lockedDoc(tx, a, id, false)
		if err != nil {
			return err
		}
		rows, err := s.commentRows(tx, d.ID, commentID)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return ErrCommentNotFound
		}
		if !commentOut(a, d, rows[0]).CanDelete {
			return ErrForbidden
		}
		return tx.Delete(&Comment{}, commentID).Error
	})
}

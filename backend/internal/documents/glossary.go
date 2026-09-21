package documents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"kupol/internal/audit"
)

// Глоссарий канона (team panel, шаг 3.6): справочник терминов вселенной — написание, значение, другие написания.
// Читают все члены команды, ведёт тот, у кого право manage_glossary (Редактор, Архивариус) и Директорат.

const (
	maxGlossaryTerm       = 100
	maxGlossaryDefinition = 2000
	maxGlossaryAliases    = 10
)

var (
	// ErrTermNotFound — нет такого термина.
	ErrTermNotFound = errors.New("documents: термин не найден")
	// ErrTermTaken — такой термин уже есть.
	ErrTermTaken = errors.New("documents: такой термин уже есть в глоссарии")
)

// GlossaryTerm — запись глоссария (таблица glossary_terms).
type GlossaryTerm struct {
	ID         int64 `gorm:"primaryKey"`
	Term       string
	Definition string
	Aliases    JSONText `gorm:"type:jsonb"` // JSON-массив строк
	AuthorID   *int64
	CreatedAt  time.Time `gorm:"autoCreateTime:false"`
	UpdatedAt  time.Time `gorm:"autoCreateTime:false;autoUpdateTime:false"`
}

func (GlossaryTerm) TableName() string { return "glossary_terms" }

// TermOut — термин в ответе.
type TermOut struct {
	ID         int64     `json:"id"`
	Term       string    `json:"term"`
	Definition string    `json:"definition"`
	Aliases    []string  `json:"aliases"`
	Author     *string   `json:"author,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
	// CanEdit — вправе ли человек менять и удалять термины.
	CanEdit bool `json:"can_edit"`
}

// TermInput — что нужно, чтобы завести или изменить термин.
type TermInput struct {
	Term       string   `json:"term"`
	Definition string   `json:"definition"`
	Aliases    []string `json:"aliases"`
}

func (a Actor) canManageGlossary() bool { return a.Directorate || a.CanManageGlossary }

// checkTerm проверяет термин и возвращает его в каноническом виде: пробелы по краям срезаны, пустые и повторные написания убраны.
func checkTerm(in TermInput) (TermInput, []Problem) {
	var p Problems
	out := TermInput{Term: strings.TrimSpace(in.Term), Definition: strings.TrimSpace(in.Definition), Aliases: []string{}}
	p.text("term", out.Term, 1, maxGlossaryTerm)
	p.text("definition", out.Definition, 1, maxGlossaryDefinition)
	seen := map[string]bool{strings.ToLower(out.Term): true}
	for i, a := range in.Aliases {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if utf8.RuneCountInString(a) > maxGlossaryTerm {
			p.Add("aliases", "написание №%d слишком длинное (%d знаков, не больше %d)", i+1, utf8.RuneCountInString(a), maxGlossaryTerm)
			continue
		}
		if key := strings.ToLower(a); !seen[key] {
			seen[key] = true
			out.Aliases = append(out.Aliases, a)
		}
	}
	if len(out.Aliases) > maxGlossaryAliases {
		p.Add("aliases", "слишком много написаний (%d, не больше %d)", len(out.Aliases), maxGlossaryAliases)
	}
	return out, p.List()
}

type termRow struct {
	GlossaryTerm
	AuthorLogin *string
}

func (s *Service) termOut(a Actor, r termRow) (TermOut, error) {
	aliases := []string{}
	if len(r.Aliases) > 0 {
		if err := json.Unmarshal(r.Aliases, &aliases); err != nil {
			return TermOut{}, fmt.Errorf("термин %d: %w", r.ID, err)
		}
	}
	return TermOut{ID: r.ID, Term: r.Term, Definition: r.Definition, Aliases: aliases, Author: r.AuthorLogin, UpdatedAt: r.UpdatedAt.UTC(), CanEdit: a.canManageGlossary()}, nil
}

func (s *Service) termRows(db *gorm.DB, id int64, q string) ([]termRow, error) {
	tx := db.Table("glossary_terms g").Select("g.*, u.login::text AS author_login").Joins("LEFT JOIN users u ON u.id = g.author_id").
		Order("g.term COLLATE kupol_natural, g.id")
	if id > 0 {
		tx = tx.Where("g.id = ?", id)
	}
	if q != "" {
		like := "%" + likeEscaper.Replace(q) + "%"
		tx = tx.Where(`(g.term ILIKE ? ESCAPE '\' OR g.definition ILIKE ? ESCAPE '\' OR EXISTS (SELECT 1 FROM jsonb_array_elements_text(g.aliases) AS al WHERE al ILIKE ? ESCAPE '\'))`, like, like, like)
	}
	var rows []termRow
	return rows, tx.Scan(&rows).Error
}

// GlossaryList возвращает термины по алфавиту; q — часть термина, написания или определения. Видят все члены команды.
func (s *Service) GlossaryList(ctx context.Context, a Actor, q string) ([]TermOut, error) {
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) > 100 {
		return nil, queryError("q", "слишком длинный запрос")
	}
	rows, err := s.termRows(s.db.WithContext(ctx), 0, q)
	if err != nil {
		return nil, err
	}
	out := make([]TermOut, len(rows))
	for i, r := range rows {
		if out[i], err = s.termOut(a, r); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) termOne(ctx context.Context, a Actor, id int64) (*TermOut, error) {
	rows, err := s.termRows(s.db.WithContext(ctx), id, "")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrTermNotFound
	}
	o, err := s.termOut(a, rows[0])
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// GlossaryCreate заводит термин. Нужно право вести глоссарий.
func (s *Service) GlossaryCreate(ctx context.Context, a Actor, in TermInput) (*TermOut, error) {
	if !a.canManageGlossary() {
		return nil, ErrForbidden
	}
	in, problems := checkTerm(in)
	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}
	now := s.now()
	uid := a.UserID
	aliases, err := json.Marshal(in.Aliases)
	if err != nil {
		return nil, err
	}
	t := &GlossaryTerm{Term: in.Term, Definition: in.Definition, Aliases: aliases, AuthorID: &uid, CreatedAt: now, UpdatedAt: now}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(t).Error; err != nil {
			return err
		}
		return audit.Record(tx, now, audit.GlossaryCreated, audit.Event{ActorID: &uid, Details: audit.Details("term_id", t.ID, "term", t.Term)})
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, ErrTermTaken
	}
	if err != nil {
		return nil, err
	}
	return s.termOne(ctx, a, t.ID)
}

// GlossaryUpdate заменяет термин, определение и написания.
func (s *Service) GlossaryUpdate(ctx context.Context, a Actor, id int64, in TermInput) (*TermOut, error) {
	if !a.canManageGlossary() {
		return nil, ErrForbidden
	}
	in, problems := checkTerm(in)
	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}
	aliases, err := json.Marshal(in.Aliases)
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var t GlossaryTerm
		if err := tx.Take(&t, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTermNotFound
		} else if err != nil {
			return err
		}
		now := s.now()
		update := map[string]any{"term": in.Term, "definition": in.Definition, "aliases": string(aliases), "updated_at": now}
		if err := tx.Model(&GlossaryTerm{}).Where("id = ?", id).Updates(update).Error; err != nil {
			return err
		}
		uid := a.UserID
		return audit.Record(tx, now, audit.GlossaryUpdated, audit.Event{ActorID: &uid, Details: audit.Details("term_id", id, "term", in.Term, "was", t.Term)})
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, ErrTermTaken
	}
	if err != nil {
		return nil, err
	}
	return s.termOne(ctx, a, id)
}

// GlossaryDelete удаляет термин.
func (s *Service) GlossaryDelete(ctx context.Context, a Actor, id int64) error {
	if !a.canManageGlossary() {
		return ErrForbidden
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var t GlossaryTerm
		if err := tx.Take(&t, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTermNotFound
		} else if err != nil {
			return err
		}
		if err := tx.Delete(&GlossaryTerm{}, id).Error; err != nil {
			return err
		}
		uid := a.UserID
		return audit.Record(tx, s.now(), audit.GlossaryDeleted, audit.Event{ActorID: &uid, Details: audit.Details("term_id", id, "term", t.Term)})
	})
}

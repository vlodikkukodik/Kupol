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

// Шаблоны документов и наборы блоков (team panel, шаг 3.6).
//
// Шаблон документа — заготовка нового документа: тип, заготовка названия, допуск, гриф, режим прямой ссылки и блоки.
// Набор блоков — несколько блоков, которые вставляют в готовый документ. Содержимое проверяется так же строго, как блоки
// документа (blocks.go): шаблон, который нельзя вставить, сохранить нельзя. Серверного «применить» нет: клиент подставляет
// шаблон в обычное создание документа или вставляет набор в редактор — а значит, права и проверки те же, что у любого документа.
// Читают все члены команды; ведёт тот, у кого право manage_templates (Редактор, Архивариус) и Директорат.

// TemplateKind — вид шаблона.
type TemplateKind string

const (
	TemplateDocument TemplateKind = "document" // заготовка нового документа
	TemplateBlockset TemplateKind = "blockset" // набор блоков для вставки
)

// Valid — известный вид шаблона.
func (k TemplateKind) Valid() bool { return k == TemplateDocument || k == TemplateBlockset }

const (
	maxTemplateName        = 100
	maxTemplateDescription = 500
	maxBlocksetBlocks      = 100
)

var (
	// ErrTemplateNotFound — нет такого шаблона.
	ErrTemplateNotFound = errors.New("documents: шаблон не найден")
	// ErrTemplateNameTaken — шаблон с таким названием уже есть.
	ErrTemplateNameTaken = errors.New("documents: шаблон с таким названием уже есть")
)

// TemplateContent — содержимое шаблона. У набора блоков заполнены только блоки.
type TemplateContent struct {
	Title      string       `json:"title,omitempty"`
	Level      *int         `json:"level,omitempty"`
	DirectLink string       `json:"direct_link,omitempty"`
	Grif       string       `json:"grif,omitempty"`
	Blocks     []InputBlock `json:"blocks"`
}

// Template — запись шаблона (таблица templates).
type Template struct {
	ID          int64 `gorm:"primaryKey"`
	Kind        string
	Name        string
	Description string
	DocType     *string
	Content     JSONText `gorm:"type:jsonb"`
	AuthorID    *int64
	CreatedAt   time.Time `gorm:"autoCreateTime:false"`
	UpdatedAt   time.Time `gorm:"autoCreateTime:false;autoUpdateTime:false"`
}

func (Template) TableName() string { return "templates" }

// TemplateItem — шаблон в списке.
type TemplateItem struct {
	ID          int64     `json:"id"`
	Kind        string    `json:"kind" tstype:"'document' | 'blockset'"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DocType     *string   `json:"doc_type,omitempty"`
	DocTypeName string    `json:"doc_type_name,omitempty"`
	Blocks      int       `json:"blocks"`
	Author      *string   `json:"author,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
	// CanEdit — вправе ли человек менять и удалять шаблоны.
	CanEdit bool `json:"can_edit"`
}

// TemplateFull — шаблон вместе с содержимым.
type TemplateFull struct {
	TemplateItem `tstype:",extends"`
	Content      TemplateContent `json:"content"`
}

// TemplateInput — что нужно, чтобы завести шаблон.
type TemplateInput struct {
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	DocType     string          `json:"doc_type"`
	Content     TemplateContent `json:"content"`
}

// canManageTemplates — вправе ли человек вести шаблоны.
func (a Actor) canManageTemplates() bool { return a.Directorate || a.CanManageTemplates }

// checkTemplate проверяет вид, название, описание и содержимое; возвращает канонический вид содержимого.
func checkTemplate(kind TemplateKind, name, description, docType string, c TemplateContent, wantContent bool) (TemplateContent, []Problem) {
	var p Problems
	if utf8.RuneCountInString(strings.TrimSpace(name)) < 1 {
		p.Add("name", "не может быть пустым")
	} else {
		p.text("name", strings.TrimSpace(name), 1, maxTemplateName)
	}
	p.text("description", description, 0, maxTemplateDescription)

	out := TemplateContent{}
	if !wantContent {
		return out, p.List()
	}
	max := maxBlocks
	switch kind {
	case TemplateDocument:
		if !Type(docType).Valid() {
			p.Add("doc_type", "недопустимый тип %q; допустимы: %s", docType, typeList())
		}
		p.text("content.title", c.Title, 0, 300)
		p.text("content.grif", c.Grif, 0, 100)
		if c.Level != nil && (*c.Level < 0 || *c.Level > MaxLevel) {
			p.Add("content.level", "уровень должен быть от 0 до %d", MaxLevel)
		}
		if c.DirectLink != "" && !DirectLink(c.DirectLink).Valid() {
			p.Add("content.direct_link", "недопустимое значение %q; допустимы: not_found, forbidden", c.DirectLink)
		}
	case TemplateBlockset:
		if docType != "" {
			p.Add("doc_type", "у набора блоков типа нет")
		}
		if c.Title != "" || c.Level != nil || c.DirectLink != "" || c.Grif != "" {
			p.Add("content", "у набора блоков есть только блоки: название, допуск и гриф относятся к документу")
		}
		max = maxBlocksetBlocks
	}
	if len(c.Blocks) == 0 {
		p.Add("content.blocks", "нужен хотя бы один блок")
	} else if len(c.Blocks) > max {
		p.Add("content.blocks", "слишком много блоков (%d, не больше %d)", len(c.Blocks), max)
	} else {
		blocks := normalizeBlocksAt(c.Blocks, &p, "content.")
		out.Blocks = make([]InputBlock, len(blocks))
		for i, b := range blocks {
			out.Blocks[i] = InputBlock{ID: b.ID, Type: b.Type, Level: b.Level, Data: b.Data}
		}
	}
	if kind == TemplateDocument {
		out.Title, out.Level, out.DirectLink, out.Grif = c.Title, c.Level, c.DirectLink, c.Grif
	}
	return out, p.List()
}

type templateRow struct {
	Template
	AuthorLogin *string
}

func (s *Service) templateItem(a Actor, r templateRow, blocks int) TemplateItem {
	it := TemplateItem{
		ID: r.ID, Kind: r.Kind, Name: r.Name, Description: r.Description, DocType: r.DocType, Blocks: blocks,
		Author: r.AuthorLogin, UpdatedAt: r.UpdatedAt.UTC(), CanEdit: a.canManageTemplates(),
	}
	if r.DocType != nil {
		it.DocTypeName = Type(*r.DocType).NameIn(a.Lang)
	}
	return it
}

func (s *Service) templateRows(db *gorm.DB, id int64, kind string) ([]templateRow, error) {
	q := db.Table("templates t").Select("t.*, u.login::text AS author_login").Joins("LEFT JOIN users u ON u.id = t.author_id").
		Order("t.name COLLATE kupol_natural, t.id")
	if id > 0 {
		q = q.Where("t.id = ?", id)
	}
	if kind != "" {
		q = q.Where("t.kind = ?", kind)
	}
	var rows []templateRow
	return rows, q.Scan(&rows).Error
}

func decodeTemplate(raw JSONText) (TemplateContent, error) {
	var c TemplateContent
	if err := json.Unmarshal(raw, &c); err != nil {
		return TemplateContent{}, err
	}
	if c.Blocks == nil {
		c.Blocks = []InputBlock{}
	}
	return c, nil
}

// TemplateList возвращает шаблоны (kind пуст — все) по алфавиту. Видят все члены команды.
func (s *Service) TemplateList(ctx context.Context, a Actor, kind string) ([]TemplateItem, error) {
	if kind != "" && !TemplateKind(kind).Valid() {
		return nil, &QueryError{Field: "kind", Message: "document или blockset"}
	}
	rows, err := s.templateRows(s.db.WithContext(ctx), 0, kind)
	if err != nil {
		return nil, err
	}
	out := make([]TemplateItem, len(rows))
	for i, r := range rows {
		c, err := decodeTemplate(r.Content)
		if err != nil {
			return nil, fmt.Errorf("шаблон %d: %w", r.ID, err)
		}
		out[i] = s.templateItem(a, r, len(c.Blocks))
	}
	return out, nil
}

// TemplateGet возвращает шаблон целиком.
func (s *Service) TemplateGet(ctx context.Context, a Actor, id int64) (*TemplateFull, error) {
	rows, err := s.templateRows(s.db.WithContext(ctx), id, "")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrTemplateNotFound
	}
	c, err := decodeTemplate(rows[0].Content)
	if err != nil {
		return nil, fmt.Errorf("шаблон %d: %w", id, err)
	}
	return &TemplateFull{TemplateItem: s.templateItem(a, rows[0], len(c.Blocks)), Content: c}, nil
}

// TemplateCreate заводит шаблон. Нужно право вести шаблоны.
func (s *Service) TemplateCreate(ctx context.Context, a Actor, in TemplateInput) (*TemplateFull, error) {
	if !a.canManageTemplates() {
		return nil, ErrForbidden
	}
	kind := TemplateKind(in.Kind)
	if !kind.Valid() {
		return nil, oneProblem("kind", "document или blockset")
	}
	content, problems := checkTemplate(kind, in.Name, in.Description, in.DocType, in.Content, true)
	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	now := s.now()
	uid := a.UserID
	t := &Template{Kind: in.Kind, Name: strings.TrimSpace(in.Name), Description: in.Description, Content: raw, AuthorID: &uid, CreatedAt: now, UpdatedAt: now}
	if kind == TemplateDocument {
		t.DocType = &in.DocType
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(t).Error; err != nil {
			return err
		}
		return audit.Record(tx, now, audit.TemplateCreated, audit.Event{ActorID: &uid, Details: audit.Details("template_id", t.ID, "kind", in.Kind, "name", t.Name)})
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, ErrTemplateNameTaken
	}
	if err != nil {
		return nil, err
	}
	return s.TemplateGet(ctx, a, t.ID)
}

// TemplateUpdate меняет название и описание, а если передано содержимое (content != nil) — и его. Вид и тип неизменны.
func (s *Service) TemplateUpdate(ctx context.Context, a Actor, id int64, name, description string, content *TemplateContent) (*TemplateFull, error) {
	if !a.canManageTemplates() {
		return nil, ErrForbidden
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var t Template
		if err := tx.Take(&t, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTemplateNotFound
		} else if err != nil {
			return err
		}
		docType := ""
		if t.DocType != nil {
			docType = *t.DocType
		}
		var c TemplateContent
		if content != nil {
			c = *content
		}
		canon, problems := checkTemplate(TemplateKind(t.Kind), name, description, docType, c, content != nil)
		if len(problems) > 0 {
			return &ValidationError{Problems: problems}
		}
		update := map[string]any{"name": strings.TrimSpace(name), "description": description, "updated_at": s.now()}
		if content != nil {
			raw, err := json.Marshal(canon)
			if err != nil {
				return err
			}
			update["content"] = string(raw)
		}
		if err := tx.Model(&Template{}).Where("id = ?", id).Updates(update).Error; err != nil {
			return err
		}
		uid := a.UserID
		return audit.Record(tx, s.now(), audit.TemplateUpdated, audit.Event{ActorID: &uid, Details: audit.Details("template_id", id, "kind", t.Kind, "name", strings.TrimSpace(name), "content_replaced", content != nil)})
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, ErrTemplateNameTaken
	}
	if err != nil {
		return nil, err
	}
	return s.TemplateGet(ctx, a, id)
}

// TemplateDelete удаляет шаблон навсегда (документы, созданные по нему, не меняются).
func (s *Service) TemplateDelete(ctx context.Context, a Actor, id int64) error {
	if !a.canManageTemplates() {
		return ErrForbidden
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var t Template
		if err := tx.Take(&t, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTemplateNotFound
		} else if err != nil {
			return err
		}
		if err := tx.Delete(&Template{}, id).Error; err != nil {
			return err
		}
		uid := a.UserID
		return audit.Record(tx, s.now(), audit.TemplateDeleted, audit.Event{ActorID: &uid, Details: audit.Details("template_id", id, "kind", t.Kind, "name", t.Name)})
	})
}

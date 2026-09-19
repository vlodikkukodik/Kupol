package documents

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"gorm.io/gorm"
)

// VersionKind — вид снимка. От него зависит, хранится ли снимок всегда.
type VersionKind string

const (
	VersionCreate   VersionKind = "create"   // документ создан
	VersionAutosave VersionKind = "autosave" // автосохранение редактора (не применяется к документу)
	VersionSave     VersionKind = "save"     // сохранение черновика или документа на проверке
	VersionEdit     VersionKind = "edit"     // правка опубликованного или архивного «на месте»
	VersionStatus   VersionKind = "status"   // смена статуса
	VersionImport   VersionKind = "import"   // загрузка из файла командой kupol doc import
	VersionRollback VersionKind = "rollback" // откат к прежней версии
)

var versionKindNames = map[VersionKind]string{
	VersionCreate: "Создан", VersionAutosave: "Автосохранение", VersionSave: "Сохранён", VersionEdit: "Правка опубликованного",
	VersionStatus: "Смена статуса", VersionImport: "Загрузка из файла", VersionRollback: "Откат",
}

// Name — название вида для интерфейса.
func (k VersionKind) Name() string { return versionKindNames[k] }

// Permanent — хранится ли снимок всегда. Скользящие (последние MaxRollingVersions на документ) — автосохранения
// и обычные сохранения черновиков; всё остальное — история изменений документа, её не стирают.
func (k VersionKind) Permanent() bool { return k != VersionAutosave && k != VersionSave }

// MaxRollingVersions — сколько скользящих снимков хранится на документ.
const MaxRollingVersions = 30

// Version — снимок документа (таблица document_versions).
type Version struct {
	ID         int64 `gorm:"primaryKey"`
	DocumentID int64
	Revision   int
	Kind       string
	Status     string
	Permanent  bool
	AuthorID   *int64
	Note       string
	Content    JSONText  `gorm:"type:jsonb"`
	CreatedAt  time.Time `gorm:"autoCreateTime:false"`
}

func (Version) TableName() string { return "document_versions" }

// recordVersion записывает снимок в текущей транзакции и подрезает скользящие. content — что записать.
func (s *Service) recordVersion(tx *gorm.DB, d *Document, kind VersionKind, authorID *int64, note string, c Content) (*Version, error) {
	raw, err := c.canonical()
	if err != nil {
		return nil, err
	}
	v := &Version{
		DocumentID: d.ID, Revision: d.Revision, Kind: string(kind), Status: d.Status, Permanent: kind.Permanent(),
		AuthorID: authorID, Note: note, Content: raw, CreatedAt: s.now(),
	}
	if err := tx.Create(v).Error; err != nil {
		return nil, err
	}
	if !v.Permanent {
		// оставить последние MaxRollingVersions скользящих, остальные удалить
		if err := tx.Exec(`DELETE FROM document_versions
			WHERE document_id = ? AND NOT permanent
			  AND id NOT IN (SELECT id FROM document_versions WHERE document_id = ? AND NOT permanent ORDER BY id DESC LIMIT ?)`,
			d.ID, d.ID, MaxRollingVersions).Error; err != nil {
			return nil, err
		}
	}
	return v, nil
}

// VersionItem — строка истории.
type VersionItem struct {
	ID        int64     `json:"id"`
	Revision  int       `json:"revision"`
	Kind      string    `json:"kind"`
	KindName  string    `json:"kind_name"`
	Status    string    `json:"status"`
	Permanent bool      `json:"permanent"`
	Author    *string   `json:"author,omitempty"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// VersionsPage — страница истории (новые сверху).
type VersionsPage struct {
	Items   []VersionItem `json:"items"`
	Total   int64         `json:"total"`
	Page    int           `json:"page"`
	PerPage int           `json:"per_page"`
	Pages   int           `json:"pages"`
}

type versionRow struct {
	Version
	AuthorLogin *string
}

func (r versionRow) item() VersionItem {
	return VersionItem{
		ID: r.ID, Revision: r.Revision, Kind: r.Kind, KindName: VersionKind(r.Kind).Name(), Status: r.Status,
		Permanent: r.Permanent, Author: r.AuthorLogin, Note: r.Note, CreatedAt: r.CreatedAt.UTC(),
	}
}

// Versions возвращает историю документа. Видеть её вправе тот, кто вправе видеть сам документ.
func (s *Service) Versions(ctx context.Context, a Actor, docID int64, page, perPage int) (*VersionsPage, error) {
	if _, err := s.viewable(ctx, s.db, a, docID); err != nil {
		return nil, err
	}
	if page < 0 || perPage < 0 || perPage > 100 {
		return nil, &QueryError{Field: "per_page", Message: "размер страницы — от 1 до 100, страница — не меньше 1"}
	}
	if page == 0 {
		page = 1
	}
	if perPage == 0 {
		perPage = 50
	}
	db := s.db.WithContext(ctx)
	var total int64
	if err := db.Model(&Version{}).Where("document_id = ?", docID).Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []versionRow
	err := db.Table("document_versions").
		Select("document_versions.id, document_versions.document_id, document_versions.revision, document_versions.kind, document_versions.status, "+
			"document_versions.permanent, document_versions.author_id, document_versions.note, document_versions.created_at, users.login AS author_login").
		Joins("LEFT JOIN users ON users.id = document_versions.author_id").
		Where("document_versions.document_id = ?", docID).
		Order("document_versions.id DESC").Limit(perPage).Offset((page - 1) * perPage).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := &VersionsPage{Items: make([]VersionItem, len(rows)), Total: total, Page: page, PerPage: perPage}
	for i, r := range rows {
		out.Items[i] = r.item()
	}
	out.Pages = int((total + int64(perPage) - 1) / int64(perPage))
	return out, nil
}

// VersionFull — снимок целиком.
type VersionFull struct {
	VersionItem `tstype:",extends"`
	Content     Content `json:"content"`
}

// version читает снимок документа (без проверки прав: её делает вызывающий).
func (s *Service) version(db *gorm.DB, docID, versionID int64) (*versionRow, error) {
	var r versionRow
	err := db.Table("document_versions").
		Select("document_versions.*, users.login AS author_login").
		Joins("LEFT JOIN users ON users.id = document_versions.author_id").
		Where("document_versions.document_id = ? AND document_versions.id = ?", docID, versionID).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &r, err
}

// GetVersion возвращает снимок с содержимым.
func (s *Service) GetVersion(ctx context.Context, a Actor, docID, versionID int64) (*VersionFull, error) {
	if _, err := s.viewable(ctx, s.db, a, docID); err != nil {
		return nil, err
	}
	r, err := s.version(s.db.WithContext(ctx), docID, versionID)
	if err != nil {
		return nil, err
	}
	c, err := decodeContent(r.Content, false)
	if err != nil {
		return nil, fmt.Errorf("снимок %d повреждён: %w", versionID, err)
	}
	return &VersionFull{VersionItem: r.item(), Content: c}, nil
}

// ---------------------------------------------------------------- разница

// FieldChange — изменённое поле документа (название, допуск, дата, свойства).
type FieldChange struct {
	Field  string `json:"field"`
	Label  string `json:"label"`
	Before any    `json:"before" tstype:"unknown"`
	After  any    `json:"after" tstype:"unknown"`
}

// BlockChange — изменение блока. Change: added, removed, changed, moved.
type BlockChange struct {
	ID     string      `json:"id"`
	Type   string      `json:"type"`
	Change string      `json:"change"`
	Before *InputBlock `json:"before,omitempty"`
	After  *InputBlock `json:"after,omitempty"`
}

// Diff — разница двух содержимых по блокам и полям.
type Diff struct {
	Fields    []FieldChange `json:"fields"`
	Blocks    []BlockChange `json:"blocks"`
	Unchanged int           `json:"unchanged_blocks"`
	Same      bool          `json:"same"`
}

func valOf(v any) any {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		return rv.Elem().Interface()
	}
	return v
}

func fieldList(c Content) map[string]any {
	m := map[string]any{
		"title": c.Title, "grif": c.Grif, "direct_link": c.DirectLink,
	}
	if c.Level != nil {
		m["level"] = *c.Level
	} else {
		m["level"] = 0
	}
	if c.Composed != nil {
		m["composed"] = fmt.Sprintf("%d-%02d-%02d", c.Composed.Year, derefInt(c.Composed.Month), derefInt(c.Composed.Day))
	}
	if p := c.Props; p != nil {
		m["danger_class"] = valOf(p.DangerClass)
		m["deviation_points"] = valOf(p.DeviationPoints)
		m["department"] = p.Department
		m["category"] = p.Category
		m["containment_status"] = p.ContainmentStatus
		m["discovery_place"] = p.DiscoveryPlace
	}
	return m
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

var fieldLabels = []struct{ key, label string }{
	{"title", "Название"}, {"level", "Допуск"}, {"direct_link", "Прямая ссылка"}, {"grif", "Гриф"},
	{"composed", "Дата составления"}, {"danger_class", "Класс опасности"}, {"deviation_points", "Пункты отклонения"},
	{"department", "Отдел"}, {"category", "Категория"}, {"containment_status", "Статус содержания"}, {"discovery_place", "Место обнаружения"},
}

func blockEqual(a, b InputBlock) bool {
	if a.Type != b.Type || derefInt(a.Level) != derefInt(b.Level) {
		return false
	}
	return equalJSON(a.Data, b.Data)
}

// DiffContent сравнивает два содержимого. Блоки сопоставляются по идентификатору; блок, у которого изменилось
// только положение, помечается moved (порядок общих блоков считается наибольшей общей подпоследовательностью).
func DiffContent(from, to Content) Diff {
	d := Diff{Fields: []FieldChange{}, Blocks: []BlockChange{}}
	fa, fb := fieldList(from), fieldList(to)
	for _, f := range fieldLabels {
		before, after := fa[f.key], fb[f.key]
		if !reflect.DeepEqual(before, after) {
			d.Fields = append(d.Fields, FieldChange{Field: f.key, Label: f.label, Before: before, After: after})
		}
	}

	byID := func(bs []InputBlock) map[string]int {
		m := make(map[string]int, len(bs))
		for i, b := range bs {
			m[b.ID] = i
		}
		return m
	}
	ai, bi := byID(from.Blocks), byID(to.Blocks)

	// общие блоки в порядке «до» и «после»; всё, что вне наибольшей общей подпоследовательности, — переставлено
	var common []string
	for _, b := range from.Blocks {
		if _, ok := bi[b.ID]; ok {
			common = append(common, b.ID)
		}
	}
	var commonAfter []string
	for _, b := range to.Blocks {
		if _, ok := ai[b.ID]; ok {
			commonAfter = append(commonAfter, b.ID)
		}
	}
	stay := lcs(common, commonAfter)

	for _, b := range to.Blocks {
		i, existed := ai[b.ID]
		after := b
		switch {
		case !existed:
			d.Blocks = append(d.Blocks, BlockChange{ID: b.ID, Type: b.Type, Change: "added", After: &after})
		case !blockEqual(from.Blocks[i], b):
			before := from.Blocks[i]
			d.Blocks = append(d.Blocks, BlockChange{ID: b.ID, Type: b.Type, Change: "changed", Before: &before, After: &after})
		case !stay[b.ID]:
			d.Blocks = append(d.Blocks, BlockChange{ID: b.ID, Type: b.Type, Change: "moved"})
		default:
			d.Unchanged++
		}
	}
	for _, b := range from.Blocks {
		if _, ok := bi[b.ID]; !ok {
			before := b
			d.Blocks = append(d.Blocks, BlockChange{ID: b.ID, Type: b.Type, Change: "removed", Before: &before})
		}
	}
	d.Same = len(d.Fields) == 0 && len(d.Blocks) == 0
	return d
}

// lcs возвращает множество элементов наибольшей общей подпоследовательности двух списков уникальных значений.
func lcs(a, b []string) map[string]bool {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	out := map[string]bool{}
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case a[i] == b[j]:
			out[a[i]] = true
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			i++
		default:
			j++
		}
	}
	return out
}

// DiffVersions сравнивает снимок with (по умолчанию — предыдущий по времени) с againstID; against == 0 — с текущим
// содержимым документа.
func (s *Service) DiffVersions(ctx context.Context, a Actor, docID, versionID, againstID int64) (*Diff, error) {
	doc, err := s.viewable(ctx, s.db, a, docID)
	if err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	from, err := s.version(db, docID, versionID)
	if err != nil {
		return nil, err
	}
	fromC, err := decodeContent(from.Content, false)
	if err != nil {
		return nil, err
	}
	var toC Content
	if againstID == 0 {
		toC = contentFromDocument(doc)
	} else {
		other, err := s.version(db, docID, againstID)
		if err != nil {
			return nil, err
		}
		if toC, err = decodeContent(other.Content, false); err != nil {
			return nil, err
		}
	}
	d := DiffContent(fromC, toC)
	return &d, nil
}

// latestVersionContent — содержимое последнего снимка документа (автосохранение без изменений не пишется).
// Пусто, если снимков нет.
func (s *Service) latestVersionContent(tx *gorm.DB, docID int64) (JSONText, error) {
	var raws []JSONText
	err := tx.Raw("SELECT content FROM document_versions WHERE document_id = ? ORDER BY id DESC LIMIT 1", docID).Scan(&raws).Error
	if err != nil || len(raws) == 0 {
		return nil, err
	}
	return raws[0], nil
}

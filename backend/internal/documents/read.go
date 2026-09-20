package documents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// DirectorateViewer — читатель для административных команд: видит всё, включая неопубликованное.
var DirectorateViewer = Viewer{Directorate: true}

// visible ограничивает запрос документами, которые читатель вправе видеть в списках:
// опубликованные и не выше его допуска; Директорат видит и неопубликованные (но не больше уровня 7).
// Это единственное место, где определяется видимость документа в списках.
func visible(tx *gorm.DB, v Viewer) *gorm.DB {
	tx = tx.Where("level <= ?", v.Level())
	if !v.SeesUnpublished() {
		tx = tx.Where("status = ?", string(StatusPublished))
	}
	return tx
}

// Item — строка каталога и ленты.
type Item struct {
	Code              string     `json:"code"`
	Slug              string     `json:"slug"`
	Type              string     `json:"type"`
	TypeName          string     `json:"type_name"`
	Title             string     `json:"title"`
	Level             int        `json:"level"`
	Composed          Composed   `json:"composed"`
	DangerClass       *int       `json:"danger_class,omitempty"`
	DeviationPoints   *int       `json:"deviation_points,omitempty"`
	Department        *string    `json:"department,omitempty"`
	Category          *string    `json:"category,omitempty"`
	CategoryName      string     `json:"category_name,omitempty"`
	ContainmentStatus *string    `json:"containment_status,omitempty"`
	ContainmentName   string     `json:"containment_name,omitempty"`
	Status            string     `json:"status,omitempty"` // только тем, кто видит неопубликованное
	PublishedAt       *time.Time `json:"published_at,omitempty"`
}

func itemFrom(d *Document, v Viewer) Item {
	it := Item{
		Code: deref(d.Code), Slug: deref(d.Slug), Type: d.Type, TypeName: Type(d.Type).Name(), Title: d.Title, Level: d.Level,
		Composed:    Composed{Year: d.ComposedYear, Month: d.ComposedMonth, Day: d.ComposedDay},
		DangerClass: d.DangerClass, DeviationPoints: d.DeviationPoints, Department: d.Department,
		Category: d.Category, ContainmentStatus: d.ContainmentStatus,
	}
	if d.Category != nil {
		it.CategoryName = Category(*d.Category).Name()
	}
	if d.ContainmentStatus != nil {
		it.ContainmentName = Containment(*d.ContainmentStatus).Name()
	}
	// Статус и реальная дата публикации — служебные сведения (спецификация §6): читателям не показываются.
	if v.SeesUnpublished() {
		it.Status = d.Status
		it.PublishedAt = d.PublishedAt
	}
	return it
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// QueryError — некорректный параметр запроса каталога.
type QueryError struct {
	Field   string
	Message string
}

func (e *QueryError) Error() string { return e.Field + ": " + e.Message }

// ListQuery — параметры каталога.
type ListQuery struct {
	Type        string
	Class       *int
	YearFrom    *int
	YearTo      *int
	Department  string
	Category    string
	Containment string
	Status      string // только для видящих неопубликованное
	Sort        string // code (по умолчанию), title, year, class, deviation, published
	Desc        bool
	Page        int
	PerPage     int
}

const (
	defaultPerPage = 50
	maxPerPage     = 100
	maxFeed        = 50
)

// sortSQL — допустимая сортировка; %s подставляется ASC или DESC. Значения не приходят от клиента напрямую.
var sortSQL = map[string]string{
	// При равенстве порядок всегда определяется шифром (естественная сортировка), а не служебным id.
	"code":      "code COLLATE kupol_natural %[1]s, id",
	"title":     "title %[1]s, code COLLATE kupol_natural, id",
	"year":      "composed_year %[1]s, composed_month %[1]s NULLS LAST, composed_day %[1]s NULLS LAST, code COLLATE kupol_natural, id",
	"class":     "danger_class %[1]s NULLS LAST, code COLLATE kupol_natural, id",
	"deviation": "deviation_points %[1]s NULLS LAST, code COLLATE kupol_natural, id",
	"published": "published_at %[1]s NULLS LAST, code COLLATE kupol_natural, id",
}

func (q *ListQuery) normalize(v Viewer) error {
	if q.Type != "" && !Type(q.Type).Valid() {
		return &QueryError{"type", fmt.Sprintf("неизвестный тип %q", q.Type)}
	}
	if q.Class != nil && (*q.Class < 1 || *q.Class > 5) {
		return &QueryError{"class", "класс опасности — от 1 до 5"}
	}
	if q.YearFrom != nil && (*q.YearFrom < 1900 || *q.YearFrom > 2099) {
		return &QueryError{"year_from", "год — от 1900 до 2099"}
	}
	if q.YearTo != nil && (*q.YearTo < 1900 || *q.YearTo > 2099) {
		return &QueryError{"year_to", "год — от 1900 до 2099"}
	}
	if q.YearFrom != nil && q.YearTo != nil && *q.YearFrom > *q.YearTo {
		return &QueryError{"year_from", "начало периода позже его конца"}
	}
	if q.Department != "" {
		c, err := ParseCode(q.Department)
		if err != nil || c.Type != TypeUnit {
			return &QueryError{"department", "ожидается шифр отдела или филиала (ОТД-2, ОБ-14)"}
		}
		q.Department = c.Canonical
	}
	if q.Category != "" && !Category(q.Category).Valid() {
		return &QueryError{"category", fmt.Sprintf("неизвестная категория %q", q.Category)}
	}
	if q.Containment != "" && !Containment(q.Containment).Valid() {
		return &QueryError{"containment", fmt.Sprintf("неизвестный статус содержания %q", q.Containment)}
	}
	if q.Status != "" {
		if !Status(q.Status).Valid() {
			return &QueryError{"status", fmt.Sprintf("неизвестный статус %q", q.Status)}
		}
		if !v.SeesUnpublished() {
			return &QueryError{"status", "фильтр по статусу недоступен"}
		}
	}
	if q.Sort == "" {
		q.Sort = "code"
	}
	if _, ok := sortSQL[q.Sort]; !ok {
		return &QueryError{"sort", fmt.Sprintf("неизвестная сортировка %q; допустимы: code, title, year, class, deviation, published", q.Sort)}
	}
	if q.Page < 0 {
		return &QueryError{"page", "номер страницы не может быть отрицательным"}
	}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PerPage < 0 || q.PerPage > maxPerPage {
		return &QueryError{"per_page", fmt.Sprintf("размер страницы — от 1 до %d", maxPerPage)}
	}
	if q.PerPage == 0 {
		q.PerPage = defaultPerPage
	}
	return nil
}

// ListResult — страница каталога.
type ListResult struct {
	Items   []Item `json:"items"`
	Total   int64  `json:"total"`
	Page    int    `json:"page"`
	PerPage int    `json:"per_page"`
	Pages   int    `json:"pages"`
}

// listColumns — поля для списков: без блоков (они тяжёлые и в каталоге не нужны).
const listColumns = "id, code, slug, type, title, status, level, composed_year, composed_month, composed_day, " +
	"danger_class, deviation_points, department, category, containment_status, published_at"

// List возвращает страницу каталога: только то, что читатель вправе видеть.
func (s *Service) List(ctx context.Context, v Viewer, q ListQuery) (*ListResult, error) {
	if err := q.normalize(v); err != nil {
		return nil, err
	}
	tx := visible(s.db.WithContext(ctx).Model(&Document{}).Where("code IS NOT NULL"), v)
	if q.Type != "" {
		tx = tx.Where("type = ?", q.Type)
	}
	if q.Class != nil {
		tx = tx.Where("danger_class = ?", *q.Class)
	}
	if q.YearFrom != nil {
		tx = tx.Where("composed_year >= ?", *q.YearFrom)
	}
	if q.YearTo != nil {
		tx = tx.Where("composed_year <= ?", *q.YearTo)
	}
	if q.Department != "" {
		tx = tx.Where("department = ?", q.Department)
	}
	if q.Category != "" {
		tx = tx.Where("category = ?", q.Category)
	}
	if q.Containment != "" {
		tx = tx.Where("containment_status = ?", q.Containment)
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, err
	}
	dir := "ASC"
	if q.Desc {
		dir = "DESC"
	}
	var docs []Document
	err := tx.Select(listColumns).
		Order(fmt.Sprintf(sortSQL[q.Sort], dir)).
		Limit(q.PerPage).Offset((q.Page - 1) * q.PerPage).
		Find(&docs).Error
	if err != nil {
		return nil, err
	}
	res := &ListResult{Items: make([]Item, len(docs)), Total: total, Page: q.Page, PerPage: q.PerPage}
	for i := range docs {
		res.Items[i] = itemFrom(&docs[i], v)
	}
	res.Pages = int((total + int64(q.PerPage) - 1) / int64(q.PerPage))
	return res, nil
}

// Recent — лента «Поступило в ЦАК»: последние опубликованные документы, доступные читателю.
func (s *Service) Recent(ctx context.Context, v Viewer, limit int) ([]Item, error) {
	if limit < 1 || limit > maxFeed {
		return nil, &QueryError{"limit", fmt.Sprintf("от 1 до %d", maxFeed)}
	}
	var docs []Document
	err := s.db.WithContext(ctx).Model(&Document{}).
		Where("code IS NOT NULL AND status = ? AND level <= ? AND published_at IS NOT NULL", string(StatusPublished), v.Level()).
		Select(listColumns).Order("published_at DESC, id DESC").Limit(limit).Find(&docs).Error
	if err != nil {
		return nil, err
	}
	out := make([]Item, len(docs))
	for i := range docs {
		out[i] = itemFrom(&docs[i], v)
	}
	return out, nil
}

// Summary — сводка каталога для фильтров и «папок»: сколько документов каждого типа, отдела и класса.
type Summary struct {
	Total       int64             `json:"total"`
	Types       []TypeCount       `json:"types"`
	Departments []DepartmentCount `json:"departments"`
	Classes     []ClassCount      `json:"classes"`
}

type TypeCount struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type DepartmentCount struct {
	Code  string `json:"code"`
	Count int64  `json:"count"`
}

type ClassCount struct {
	Class int   `json:"class"`
	Count int64 `json:"count"`
}

// Summary считает только документы, видимые читателю.
func (s *Service) Summary(ctx context.Context, v Viewer) (*Summary, error) {
	base := func() *gorm.DB {
		return visible(s.db.WithContext(ctx).Model(&Document{}).Where("code IS NOT NULL"), v)
	}
	out := &Summary{Types: []TypeCount{}, Departments: []DepartmentCount{}, Classes: []ClassCount{}}

	var typeRows []struct {
		Type  string
		Count int64
	}
	if err := base().Select("type, count(*) AS count").Group("type").Scan(&typeRows).Error; err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, r := range typeRows {
		counts[r.Type] = r.Count
		out.Total += r.Count
	}
	for _, t := range Types { // в порядке каталога; типы без документов не показываем
		if n := counts[string(t)]; n > 0 {
			out.Types = append(out.Types, TypeCount{Type: string(t), Name: t.Name(), Count: n})
		}
	}

	var deps []DepartmentCount
	if err := base().Where("department IS NOT NULL").Select("department AS code, count(*) AS count").
		Group("department").Order("department COLLATE kupol_natural").Scan(&deps).Error; err != nil {
		return nil, err
	}
	if deps != nil {
		out.Departments = deps
	}

	var classes []ClassCount
	if err := base().Where("danger_class IS NOT NULL").Select("danger_class AS class, count(*) AS count").
		Group("danger_class").Order("danger_class").Scan(&classes).Error; err != nil {
		return nil, err
	}
	if classes != nil {
		out.Classes = classes
	}
	return out, nil
}

// OutDocument — документ в ответе читателю.
type OutDocument struct {
	Code              string     `json:"code"`
	Slug              string     `json:"slug"`
	Type              string     `json:"type"`
	TypeName          string     `json:"type_name"`
	Title             string     `json:"title"`
	Grif              string     `json:"grif"`
	Level             int        `json:"level"`
	Composed          Composed   `json:"composed"`
	DangerClass       *int       `json:"danger_class,omitempty"`
	DeviationPoints   *int       `json:"deviation_points,omitempty"`
	Department        *string    `json:"department,omitempty"`
	Category          *string    `json:"category,omitempty"`
	CategoryName      string     `json:"category_name,omitempty"`
	ContainmentStatus *string    `json:"containment_status,omitempty"`
	ContainmentName   string     `json:"containment_name,omitempty"`
	DiscoveryPlace    *string    `json:"discovery_place,omitempty"`
	Author            *string    `json:"author,omitempty"`
	Status            string     `json:"status,omitempty"` // только тем, кто видит неопубликованное
	Blocks            []OutBlock `json:"blocks"`
	// MentionedIn — документы, ссылающиеся на этот, из числа доступных читателю («Упоминается в»). В предпросмотре не заполняется.
	MentionedIn []Mention `json:"mentioned_in,omitempty"`
	// CopyNumber — номер экземпляра читателя, как у нумерованных копий секретных документов: «0042» из номера аккаунта, у Гражданина — «б/н».
	CopyNumber string `json:"copy_number"`
	// ReadCount — сколько зарегистрированных читателей ознакомилось с документом («лист ознакомления»; без имён — история чтения закрыта).
	ReadCount int `json:"read_count"`
}

// copyNumber — номер экземпляра читателя.
func copyNumber(v Viewer) string {
	if v.UserID == 0 {
		return "б/н"
	}
	return fmt.Sprintf("%04d", v.UserID)
}

// docRow — документ вместе с ником автора.
type docRow struct {
	Document
	AuthorLogin *string
}

// Get открывает документ читателю. Порядок решений (важно для безопасности):
//  1. Не шифр или нет такого документа — ErrNotFound.
//  2. Неопубликованное видит только Директорат; всем остальным — ErrNotFound независимо от настроек.
//  3. Документ выше допуска: по флагу документа либо ErrNotFound (404), либо AccessDeniedError
//     («Доступ запрещён» и нужный уровень).
//  4. Иначе блоки собираются заново по допуску читателя; закрытое в ответ не попадает.
func (s *Service) Get(ctx context.Context, v Viewer, ref string) (*OutDocument, error) {
	c, err := ParseCode(ref)
	if err != nil {
		return nil, ErrNotFound
	}
	var row docRow
	err = s.db.WithContext(ctx).Table("documents").
		Select("documents.*, users.login AS author_login").
		Joins("LEFT JOIN users ON users.id = documents.author_id").
		Where("documents.slug = ?", c.Slug).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d := &row.Document

	if d.Status != string(StatusPublished) && !v.SeesUnpublished() {
		return nil, ErrNotFound
	}
	if d.Level > v.Level() {
		if DirectLink(d.DirectLink) == DirectLinkForbidden {
			return nil, &AccessDeniedError{RequiredLevel: d.Level}
		}
		return nil, ErrNotFound
	}

	resolve, err := s.linkResolver(ctx, v, d.Blocks)
	if err != nil {
		return nil, err
	}
	blocks, err := RenderBlocks(d.Blocks, d.Level, v.Level(), resolve)
	if err != nil {
		return nil, fmt.Errorf("документ %s: %w", c.Canonical, err)
	}

	out := outDocument(d, row.AuthorLogin, blocks, v)
	if out.MentionedIn, err = s.mentions(ctx, v, c.Canonical); err != nil {
		return nil, err
	}

	if v.UserID != 0 && d.Status == string(StatusPublished) {
		s.recordRead(ctx, v.UserID, d.ID)
	}
	// Лист ознакомления считается после записи чтения: сам читатель уже в счёте.
	var reads int64
	if err := s.db.WithContext(ctx).Raw("SELECT count(*) FROM document_reads WHERE document_id = ?", d.ID).Scan(&reads).Error; err != nil {
		return nil, err
	}
	out.ReadCount = int(reads)
	return out, nil
}

// outDocument собирает ответ читателю из записи документа и уже отфильтрованных блоков. Общий для чтения и предпросмотра:
// что попадает в ответ, решается в одном месте.
func outDocument(d *Document, authorLogin *string, blocks []OutBlock, v Viewer) *OutDocument {
	out := &OutDocument{
		Code: deref(d.Code), Slug: deref(d.Slug), Type: d.Type, TypeName: Type(d.Type).Name(), Title: d.Title,
		Grif: d.Grif, Level: d.Level,
		Composed:    Composed{Year: d.ComposedYear, Month: d.ComposedMonth, Day: d.ComposedDay},
		DangerClass: d.DangerClass, DeviationPoints: d.DeviationPoints, Department: d.Department,
		Category: d.Category, ContainmentStatus: d.ContainmentStatus, DiscoveryPlace: d.DiscoveryPlace,
		Author: authorLogin, Blocks: blocks, CopyNumber: copyNumber(v),
	}
	if d.Category != nil {
		out.CategoryName = Category(*d.Category).Name()
	}
	if d.ContainmentStatus != nil {
		out.ContainmentName = Containment(*d.ContainmentStatus).Name()
	}
	if v.SeesUnpublished() {
		out.Status = d.Status
	}
	return out
}

// linkResolver разом находит цели всех ссылок документа. Цель видна по тем же правилам, что и в каталоге;
// невидимая и несуществующая неотличимы (nil).
func (s *Service) linkResolver(ctx context.Context, v Viewer, blocks []Block) (LinkResolver, error) {
	codes, err := LinkCodes(blocks)
	if err != nil {
		return nil, err
	}
	if len(codes) == 0 {
		return func(string) *LinkTarget { return nil }, nil
	}
	var rows []Document
	err = visible(s.db.WithContext(ctx).Model(&Document{}), v).
		Where("code IN ?", codes).Select("code, slug, title, type").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	targets := make(map[string]*LinkTarget, len(rows))
	for i := range rows {
		r := &rows[i]
		targets[deref(r.Code)] = &LinkTarget{Code: deref(r.Code), Slug: deref(r.Slug), Title: r.Title, Type: Type(r.Type)}
	}
	return func(code string) *LinkTarget { return targets[code] }, nil
}

// recordRead запоминает, что пользователь прочитал документ. Сбой записи чтение не ломает.
func (s *Service) recordRead(ctx context.Context, userID, docID int64) {
	now := s.now()
	err := s.db.WithContext(ctx).Exec(`
		INSERT INTO document_reads (user_id, document_id, first_read_at, last_read_at, read_count)
		VALUES (?, ?, ?, ?, 1)
		ON CONFLICT (user_id, document_id)
		DO UPDATE SET last_read_at = EXCLUDED.last_read_at, read_count = document_reads.read_count + 1`,
		userID, docID, now, now).Error
	if err != nil && ctx.Err() == nil {
		s.log.Error("не удалось записать факт чтения", "user_id", userID, "document_id", docID, "err", err)
	}
}

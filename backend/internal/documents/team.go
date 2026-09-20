package documents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"kupol/internal/audit"
)

// Actor — кто работает с документами в team panel. Права приходят готовыми (их считает сервис аккаунтов),
// этот пакет о ролях не знает.
type Actor struct {
	UserID           int64
	Login            string
	Directorate      bool
	CanWrite         bool // создавать документы и править свои черновики
	CanReview        bool // проверять документы (Редактор)
	CanEditPublished bool // править опубликованное на месте
}

func (a Actor) owns(d *Document) bool { return d.AuthorID != nil && *d.AuthorID == a.UserID }

// CanView — видит ли человек документ в team panel.
//   - Директорат видит всё.
//   - Автор видит свои документы в любом статусе.
//   - Редактор (право проверять или править опубликованное) видит всё, что вышло из черновика:
//     чужой черновик — личное дело автора, пока тот не отправил его на проверку.
func (a Actor) CanView(d *Document) bool {
	if a.Directorate || a.owns(d) {
		return true
	}
	return (a.CanReview || a.CanEditPublished) && d.Status != string(StatusDraft)
}

// CanEdit — вправе ли человек менять документ.
//   - Директорат — любой.
//   - Черновик — его автор (нужно право писать).
//   - На проверке — автор и тот, кто проверяет.
//   - Опубликованный и архивный — только тот, кто вправе править опубликованное «на месте».
func (a Actor) CanEdit(d *Document) bool {
	if a.Directorate {
		return true
	}
	switch Status(d.Status) {
	case StatusDraft:
		return a.owns(d) && a.CanWrite
	case StatusReview:
		return (a.owns(d) && a.CanWrite) || a.CanReview
	case StatusPublished, StatusArchived:
		return a.CanEditPublished
	}
	return false
}

// CanBreakLocks — вправе ли человек снять чужой замок.
func (a Actor) CanBreakLocks() bool { return a.Directorate || a.CanReview || a.CanEditPublished }

// ConflictError — документ изменился после того, как редактор его открыл.
type ConflictError struct {
	CurrentRevision int
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("documents: документ изменён, текущая редакция %d", e.CurrentRevision)
}

// ErrCodeTaken — шифр уже занят другим документом.
var ErrCodeTaken = errors.New("documents: шифр уже занят")

// viewable находит документ и проверяет, что человек вправе его видеть (иначе ErrNotFound: существование не раскрывается).
func (s *Service) viewable(ctx context.Context, db *gorm.DB, a Actor, id int64) (*Document, error) {
	var d Document
	err := db.WithContext(ctx).Take(&d, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !a.CanView(&d) {
		return nil, ErrNotFound
	}
	return &d, nil
}

// lockedDoc находит документ и блокирует его строку до конца транзакции. needEdit — нужно ли право менять.
func (s *Service) lockedDoc(tx *gorm.DB, a Actor, id int64, needEdit bool) (*Document, error) {
	var d Document
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&d, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !a.CanView(&d) {
		return nil, ErrNotFound
	}
	if needEdit && !a.CanEdit(&d) {
		return nil, ErrForbidden
	}
	return &d, nil
}

// ---------------------------------------------------------------- список

// TeamListQuery — параметры списка документов в team panel.
type TeamListQuery struct {
	Status  string
	Type    string
	Query   string // часть названия или шифра
	Mine    bool   // только мои документы
	Page    int
	PerPage int
}

// TeamItem — строка списка.
type TeamItem struct {
	ID        int64     `json:"id"`
	Code      *string   `json:"code,omitempty"`
	Type      string    `json:"type"`
	TypeName  string    `json:"type_name"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Level     int       `json:"level"`
	Revision  int       `json:"revision"`
	Author    *string   `json:"author,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	CanEdit   bool      `json:"can_edit"`
	Lock      *LockInfo `json:"lock,omitempty"`
}

// TeamListResult — страница списка.
type TeamListResult struct {
	Items   []TeamItem `json:"items"`
	Total   int64      `json:"total"`
	Page    int        `json:"page"`
	PerPage int        `json:"per_page"`
	Pages   int        `json:"pages"`
}

// visibleTo ограничивает выборку документами, которые человек вправе видеть (то же, что Actor.CanView).
func (a Actor) visibleTo(tx *gorm.DB) *gorm.DB {
	if a.Directorate {
		return tx
	}
	if a.CanReview || a.CanEditPublished {
		return tx.Where("(documents.author_id = ? OR documents.status <> 'draft')", a.UserID)
	}
	return tx.Where("documents.author_id = ?", a.UserID)
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

type teamRow struct {
	Document
	AuthorLogin *string
}

// TeamList возвращает документы, которые человек вправе видеть, — новые правки сверху.
func (s *Service) TeamList(ctx context.Context, a Actor, q TeamListQuery) (*TeamListResult, error) {
	if q.Status != "" && !Status(q.Status).Valid() {
		return nil, &QueryError{Field: "status", Message: fmt.Sprintf("неизвестный статус %q", q.Status)}
	}
	if q.Type != "" && !Type(q.Type).Valid() {
		return nil, &QueryError{Field: "type", Message: fmt.Sprintf("неизвестный тип %q", q.Type)}
	}
	q.Query = strings.TrimSpace(q.Query)
	if len([]rune(q.Query)) > 100 {
		return nil, &QueryError{Field: "q", Message: "слишком длинный запрос"}
	}
	if q.Page < 0 {
		return nil, &QueryError{Field: "page", Message: "номер страницы не может быть отрицательным"}
	}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PerPage < 0 || q.PerPage > maxPerPage {
		return nil, &QueryError{Field: "per_page", Message: fmt.Sprintf("размер страницы — от 1 до %d", maxPerPage)}
	}
	if q.PerPage == 0 {
		q.PerPage = defaultPerPage
	}

	db := s.db.WithContext(ctx)
	base := a.visibleTo(db.Model(&Document{}))
	if q.Status != "" {
		base = base.Where("documents.status = ?", q.Status)
	}
	if q.Type != "" {
		base = base.Where("documents.type = ?", q.Type)
	}
	if q.Mine {
		base = base.Where("documents.author_id = ?", a.UserID)
	}
	if q.Query != "" {
		like := "%" + likeEscaper.Replace(q.Query) + "%"
		base = base.Where(`(documents.title ILIKE ? ESCAPE '\' OR documents.code ILIKE ? ESCAPE '\')`, like, like)
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []teamRow
	err := base.Select("documents.id, documents.code, documents.slug, documents.type, documents.title, documents.status, documents.level, " +
		"documents.revision, documents.author_id, documents.updated_at, users.login AS author_login").
		Joins("LEFT JOIN users ON users.id = documents.author_id").
		Order("documents.updated_at DESC, documents.id DESC").Limit(q.PerPage).Offset((q.Page - 1) * q.PerPage).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	locks, err := s.activeLocks(db, ids, a)
	if err != nil {
		return nil, err
	}
	out := &TeamListResult{Items: make([]TeamItem, len(rows)), Total: total, Page: q.Page, PerPage: q.PerPage}
	for i := range rows {
		r := &rows[i]
		out.Items[i] = TeamItem{
			ID: r.ID, Code: r.Code, Type: r.Type, TypeName: Type(r.Type).Name(), Title: r.Title, Status: r.Status, Level: r.Level,
			Revision: r.Revision, Author: r.AuthorLogin, UpdatedAt: r.UpdatedAt.UTC(), CanEdit: a.CanEdit(&r.Document), Lock: locks[r.ID],
		}
	}
	out.Pages = int((total + int64(q.PerPage) - 1) / int64(q.PerPage))
	return out, nil
}

// activeLocks — действующие замки указанных документов.
func (s *Service) activeLocks(db *gorm.DB, ids []int64, a Actor) (map[int64]*LockInfo, error) {
	out := map[int64]*LockInfo{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []lockRow
	err := db.Table("document_locks").
		Select("document_locks.*, users.login AS holder_login").
		Joins("JOIN users ON users.id = document_locks.user_id").
		Where("document_locks.document_id IN ? AND document_locks.expires_at > ?", ids, s.now()).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.DocumentID] = &LockInfo{Holder: r.HolderLogin, Mine: r.UserID == a.UserID, AcquiredAt: r.AcquiredAt.UTC(), ExpiresAt: r.ExpiresAt.UTC()}
	}
	return out, nil
}

// ---------------------------------------------------------------- документ

// DraftInfo — несохранённые правки: последнее автосохранение поверх текущей редакции.
type DraftInfo struct {
	VersionID int64     `json:"version_id"`
	Author    *string   `json:"author,omitempty"`
	SavedAt   time.Time `json:"saved_at"`
	Content   Content   `json:"content"`
}

// TeamDocument — документ целиком для team panel: без фильтрации по допуску читателя (роль команды — доверенная).
type TeamDocument struct {
	ID          int64      `json:"id"`
	Code        *string    `json:"code,omitempty"`
	Slug        *string    `json:"slug,omitempty"`
	Type        string     `json:"type"`
	TypeName    string     `json:"type_name"`
	Status      string     `json:"status"`
	Revision    int        `json:"revision"`
	Author      *string    `json:"author,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	Content     Content    `json:"content"`
	CanEdit     bool       `json:"can_edit"`
	// CanBreakLock — вправе ли человек снять чужой замок («Редактирует: …»), если тот ушёл и не снял.
	CanBreakLock bool       `json:"can_break_lock"`
	Lock         *LockInfo  `json:"lock,omitempty"`
	Draft        *DraftInfo `json:"draft,omitempty"`
	// Workflow — что человек может сделать с документом: отправить на проверку, вынести вердикт, убрать в архив…
	Workflow Workflow `json:"workflow"`
}

func (s *Service) teamDocument(db *gorm.DB, a Actor, d *Document) (*TeamDocument, error) {
	out := &TeamDocument{
		ID: d.ID, Code: d.Code, Slug: d.Slug, Type: d.Type, TypeName: Type(d.Type).Name(), Status: d.Status, Revision: d.Revision,
		CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(), Content: contentFromDocument(d), CanEdit: a.CanEdit(d),
		CanBreakLock: a.CanEdit(d) && a.CanBreakLocks(), Workflow: a.Workflow(d),
	}
	if d.PublishedAt != nil {
		t := d.PublishedAt.UTC()
		out.PublishedAt = &t
	}
	if d.AuthorID != nil {
		var login *string
		if err := db.Raw("SELECT login::text FROM users WHERE id = ?", *d.AuthorID).Scan(&login).Error; err != nil {
			return nil, err
		}
		out.Author = login
	}
	lock, err := s.activeLock(db, d.ID, a)
	if err != nil {
		return nil, err
	}
	out.Lock = lock

	// Несохранённые правки предлагаются, только если сделаны поверх текущей редакции и отличаются от неё.
	var rows []versionRow
	err = db.Table("document_versions").
		Select("document_versions.*, users.login AS author_login").
		Joins("LEFT JOIN users ON users.id = document_versions.author_id").
		Where("document_versions.document_id = ? AND document_versions.kind = ? AND document_versions.revision = ?", d.ID, string(VersionAutosave), d.Revision).
		Order("document_versions.id DESC").Limit(1).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 1 {
		live, _ := out.Content.canonical()
		if !equalJSON(rows[0].Content, live) {
			if c, err := decodeContent(rows[0].Content, false); err == nil {
				out.Draft = &DraftInfo{VersionID: rows[0].ID, Author: rows[0].AuthorLogin, SavedAt: rows[0].CreatedAt.UTC(), Content: c}
			}
		}
	}
	return out, nil
}

// TeamGet возвращает документ целиком. Тому, кто не вправе его видеть, — ErrNotFound.
func (s *Service) TeamGet(ctx context.Context, a Actor, id int64) (*TeamDocument, error) {
	db := s.db.WithContext(ctx)
	d, err := s.viewable(ctx, db, a, id)
	if err != nil {
		return nil, err
	}
	return s.teamDocument(db, a, d)
}

// CreateInput — что нужно, чтобы завести документ.
type CreateInput struct {
	Type    string
	Code    string // обязателен у всех типов, кроме объекта (его номер О-№ присваивается при публикации)
	Content Content
}

// TeamCreate заводит черновик. Автор — тот, кто создал.
func (s *Service) TeamCreate(ctx context.Context, a Actor, in CreateInput) (*TeamDocument, error) {
	if !a.CanWrite && !a.Directorate {
		return nil, ErrForbidden
	}
	var probs Problems
	input := in.Content.input(in.Code, in.Type, string(StatusDraft))
	pr := input.prepare("$", &probs)
	if probs.Any() {
		return nil, &ValidationError{Problems: probs.List()}
	}
	d := pr.Doc
	now := s.now()
	uid := a.UserID
	d.AuthorID, d.CreatedAt, d.UpdatedAt, d.Revision = &uid, now, now, 1

	var out *TeamDocument
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		if _, err := s.recordVersion(tx, &d, VersionCreate, &uid, "", contentFromDocument(&d)); err != nil {
			return err
		}
		var err error
		out, err = s.teamDocument(tx, a, &d)
		return err
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, ErrCodeTaken
	}
	if err != nil {
		return nil, err
	}
	s.log.Info("документ создан", "document_id", d.ID, "type", d.Type, "author", a.Login)
	return out, nil
}

// SaveResult — итог сохранения.
type SaveResult struct {
	Document *TeamDocument `json:"document" tstype:",required"`
	Changed  bool          `json:"changed"` // false — содержимое не изменилось, новая редакция не создавалась
}

// saveContent проверяет содержимое и, если оно отличается от текущего, применяет его к документу: редакция растёт,
// пишется снимок. Вызывается внутри транзакции, строка документа заблокирована.
func (s *Service) saveContent(tx *gorm.DB, a Actor, d *Document, c Content, kind VersionKind, note string) (bool, error) {
	code := ""
	if d.Code != nil {
		code = *d.Code
	}
	var probs Problems
	input := c.input(code, d.Type, d.Status)
	pr := input.prepare("$", &probs)
	if probs.Any() {
		return false, &ValidationError{Problems: probs.List()}
	}
	before, err := contentFromDocument(d).canonical()
	if err != nil {
		return false, err
	}
	after, err := contentFromDocument(&pr.Doc).canonical()
	if err != nil {
		return false, err
	}
	if equalJSON(before, after) {
		return false, nil
	}
	applyContent(d, pr.Doc)
	d.Revision++
	d.UpdatedAt = s.now()
	if err := tx.Save(d).Error; err != nil {
		return false, err
	}
	uid := a.UserID
	if _, err := s.recordVersion(tx, d, kind, &uid, note, contentFromDocument(d)); err != nil {
		return false, err
	}
	return true, nil
}

// editKind — вид снимка при сохранении: правка опубликованного хранится всегда, обычное сохранение черновика — скользящее.
func editKind(d *Document) VersionKind {
	if d.Status == string(StatusPublished) || d.Status == string(StatusArchived) {
		return VersionEdit
	}
	return VersionSave
}

// TeamSave сохраняет содержимое документа. Нужны право править документ и замок (берётся сам, если свободен).
// baseRevision — редакция, с которой начал редактор: если документ успели изменить, — ConflictError.
func (s *Service) TeamSave(ctx context.Context, a Actor, id int64, baseRevision int, c Content) (*SaveResult, error) {
	var res *SaveResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		d, err := s.lockedDoc(tx, a, id, true)
		if err != nil {
			return err
		}
		if _, err := s.acquireLock(tx, id, a); err != nil {
			return err
		}
		if baseRevision != d.Revision {
			return &ConflictError{CurrentRevision: d.Revision}
		}
		kind := editKind(d)
		changed, err := s.saveContent(tx, a, d, c, kind, "")
		if err != nil {
			return err
		}
		if changed && kind == VersionEdit {
			uid := a.UserID
			if err := audit.Record(tx, s.now(), audit.PublishedEdited, audit.Event{ActorID: &uid, DocumentID: &id, Details: audit.Details("revision", d.Revision, "status", d.Status)}); err != nil {
				return err
			}
		}
		td, err := s.teamDocument(tx, a, d)
		res = &SaveResult{Document: td, Changed: changed}
		return err
	})
	if err != nil {
		return nil, err
	}
	if res.Changed {
		s.log.Info("документ сохранён", "document_id", id, "revision", res.Document.Revision, "by", a.Login, "status", res.Document.Status)
	}
	return res, nil
}

// AutosaveResult — итог автосохранения.
type AutosaveResult struct {
	Saved     bool      `json:"saved"` // false — содержимое не отличается от последнего снимка, ничего не записано
	VersionID int64     `json:"version_id,omitempty"`
	SavedAt   time.Time `json:"saved_at"`
	Lock      *LockInfo `json:"lock" tstype:",required"`
}

// TeamAutosave записывает несохранённые правки редактора. Документ они не меняют: читатели их не видят.
// Содержимое не проверяется на полноту (человек ещё пишет) — только на разбор; строгая проверка — при сохранении.
func (s *Service) TeamAutosave(ctx context.Context, a Actor, id int64, raw []byte) (*AutosaveResult, error) {
	if len(raw) > MaxInputBytes {
		return nil, &ValidationError{Problems: []Problem{{Path: "$", Message: fmt.Sprintf("содержимое слишком большое (%d байт, не больше %d)", len(raw), MaxInputBytes)}}}
	}
	c, err := decodeContent(raw, false)
	if err != nil {
		return nil, &ValidationError{Problems: []Problem{{Path: "$", Message: describeJSONError(err)}}}
	}
	var res *AutosaveResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		d, err := s.lockedDoc(tx, a, id, true)
		if err != nil {
			return err
		}
		lock, err := s.acquireLock(tx, id, a)
		if err != nil {
			return err
		}
		res = &AutosaveResult{SavedAt: s.now().UTC(), Lock: lock}
		canon, err := c.canonical()
		if err != nil {
			return err
		}
		if last, err := s.latestVersionContent(tx, id); err != nil {
			return err
		} else if last != nil && equalJSON(last, canon) {
			return nil
		}
		uid := a.UserID
		v, err := s.recordVersion(tx, d, VersionAutosave, &uid, "", c)
		if err != nil {
			return err
		}
		res.Saved, res.VersionID = true, v.ID
		return nil
	})
	return res, err
}

// TeamRestore откатывает документ к содержимому снимка. Это обычное сохранение: проверяется как любое другое,
// создаёт новую редакцию и снимок «откат» (история не переписывается). Автосохранение с неполным содержимым может не
// пройти проверку — тогда в ответе список замечаний.
func (s *Service) TeamRestore(ctx context.Context, a Actor, id, versionID int64) (*SaveResult, error) {
	var res *SaveResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		d, err := s.lockedDoc(tx, a, id, true)
		if err != nil {
			return err
		}
		if _, err := s.acquireLock(tx, id, a); err != nil {
			return err
		}
		v, err := s.version(tx, id, versionID)
		if err != nil {
			return err
		}
		c, err := decodeContent(v.Content, false)
		if err != nil {
			return fmt.Errorf("снимок %d повреждён: %w", versionID, err)
		}
		note := fmt.Sprintf("Откат к версии %d (редакция %d, %s)", v.ID, v.Revision, VersionKind(v.Kind).Name())
		changed, err := s.saveContent(tx, a, d, c, VersionRollback, note)
		if err != nil {
			return err
		}
		if changed {
			uid := a.UserID
			if err := audit.Record(tx, s.now(), audit.DocumentRolledBack, audit.Event{ActorID: &uid, DocumentID: &id, Details: audit.Details("version_id", v.ID, "version_revision", v.Revision, "revision", d.Revision)}); err != nil {
				return err
			}
		}
		td, err := s.teamDocument(tx, a, d)
		res = &SaveResult{Document: td, Changed: changed}
		return err
	})
	if err != nil {
		return nil, err
	}
	if res.Changed {
		s.log.Info("документ откачен", "document_id", id, "to_version", versionID, "by", a.Login)
	}
	return res, err
}

package documents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrNotFound — документа нет или читатель не вправе знать о его существовании.
var ErrNotFound = errors.New("documents: документ не найден")

// AccessDeniedError — документ существует, но закрыт от читателя, а автор выбрал режим
// «Доступ запрещён» для прямой ссылки (direct_link: forbidden).
type AccessDeniedError struct {
	RequiredLevel int
}

func (e *AccessDeniedError) Error() string {
	return fmt.Sprintf("documents: нужен допуск не ниже уровня %d", e.RequiredLevel)
}

// Service — документы архива: загрузка, чтение с фильтрацией по допуску, каталог.
type Service struct {
	db  *gorm.DB
	log *slog.Logger
	now func() time.Time
}

// NewService создаёт сервис. now == nil — настоящее время.
func NewService(db *gorm.DB, log *slog.Logger, now func() time.Time) *Service {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{db: db, log: log, now: now}
}

// objectNumberLock — ключ рекомендательной блокировки: загрузки, присваивающие номера О-№, идут по очереди.
const objectNumberLock = 0x4b5550_4f4c_01 // «KUPOL» + 01

// ImportOptions — параметры загрузки.
type ImportOptions struct {
	// AuthorLogin — логин автора (в подвале документа); пусто — не менять / не указывать.
	AuthorLogin string
	// DryRun — проверить и показать, что будет сделано, ничего не сохраняя.
	DryRun bool
}

// ImportItem — итог по одному документу файла.
type ImportItem struct {
	Code         string   `json:"code"`
	Slug         string   `json:"slug"`
	Type         string   `json:"type"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`
	Created      bool     `json:"created"`       // false — обновлён существующий
	AssignedCode bool     `json:"assigned_code"` // номер О-№ присвоен автоматически
	Revision     int      `json:"revision"`
	Warnings     []string `json:"warnings,omitempty"`
}

// ImportReport — итог загрузки файла.
type ImportReport struct {
	Items  []ImportItem `json:"items"`
	DryRun bool         `json:"dry_run"`
}

var errDryRun = errors.New("dry run")

// Import загружает документы из файла (один документ или пакет). Всё или ничего: при любой ошибке
// не сохраняется ни один документ. Документ с уже существующим шифром обновляется на месте
// (номер редакции растёт), новый — создаётся. Объекту без шифра при status: published
// автоматически присваивается следующий номер О-№.
func (s *Service) Import(ctx context.Context, raw []byte, opt ImportOptions) (*ImportReport, error) {
	inputs, batch, err := ParseInputs(raw)
	if err != nil {
		return nil, err
	}

	var probs Problems
	prepared := make([]*prepared, len(inputs))
	for i := range inputs {
		path := "$"
		if batch {
			path = fmt.Sprintf("documents[%d]", i)
		}
		prepared[i] = inputs[i].prepare(path, &probs)
	}
	// один и тот же шифр дважды в файле
	seen := map[string]int{}
	for i, pr := range prepared {
		if pr == nil || pr.Code == nil {
			continue
		}
		if prev, dup := seen[pr.Code.Canonical]; dup {
			path := "code"
			if batch {
				path = fmt.Sprintf("documents[%d].code", i)
			}
			probs.Add(path, "шифр %s уже встречался в этом файле (документ %d)", pr.Code.Canonical, prev)
		}
		seen[pr.Code.Canonical] = i
	}
	if probs.Any() {
		return nil, &ValidationError{Problems: probs.List()}
	}

	report := &ImportReport{DryRun: opt.DryRun}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Присвоение номеров О-№ и обновления идут по очереди: два одновременных импорта не возьмут один номер.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(objectNumberLock)).Error; err != nil {
			return err
		}
		var authorID *int64
		if opt.AuthorLogin != "" {
			var id int64
			res := tx.Raw("SELECT id FROM users WHERE login = ?", opt.AuthorLogin).Scan(&id)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return fmt.Errorf("автор %q не найден: сначала он должен зарегистрироваться", opt.AuthorLogin)
			}
			authorID = &id
		}

		for _, pr := range prepared {
			item, err := s.upsert(tx, pr, authorID)
			if err != nil {
				return err
			}
			report.Items = append(report.Items, *item)
		}
		if err := s.addWarnings(tx, prepared, report); err != nil {
			return err
		}
		if opt.DryRun {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return nil, err
	}
	if !opt.DryRun {
		for _, it := range report.Items {
			s.log.Info("документ загружен", "code", it.Code, "created", it.Created, "revision", it.Revision, "status", it.Status)
		}
	}
	return report, nil
}

// upsert создаёт или обновляет документ.
func (s *Service) upsert(tx *gorm.DB, pr *prepared, authorID *int64) (*ImportItem, error) {
	now := s.now()
	d := pr.Doc

	var existing Document
	found := false
	if pr.Code != nil {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", pr.Code.Canonical).Take(&existing).Error
		switch {
		case err == nil:
			found = true
		case errors.Is(err, gorm.ErrRecordNotFound):
		default:
			return nil, err
		}
	}

	item := &ImportItem{Type: d.Type, Title: d.Title, Status: d.Status}
	if found {
		d.ID = existing.ID
		d.CreatedAt = existing.CreatedAt
		d.PublishedAt = existing.PublishedAt
		d.AuthorID = existing.AuthorID
		d.Revision = existing.Revision + 1
	} else {
		d.CreatedAt = now
		item.Created = true
	}
	if authorID != nil {
		d.AuthorID = authorID
	}
	d.UpdatedAt = now
	if d.Status == string(StatusPublished) && d.PublishedAt == nil {
		d.PublishedAt = &now
	}

	if d.Code == nil { // объект без шифра: номер присваивается при публикации
		n, err := nextObjectNumber(tx)
		if err != nil {
			return nil, err
		}
		c := ObjectCode(n)
		d.Code, d.Slug, d.ObjectNumber = &c.Canonical, &c.Slug, c.ObjectNumber
		item.AssignedCode = true
	}

	var err error
	if found {
		err = tx.Save(&d).Error
	} else {
		err = tx.Create(&d).Error
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, fmt.Errorf("шифр %s уже занят другим документом", *d.Code)
	}
	if err != nil {
		return nil, err
	}
	if err := reindexDocument(tx, &d); err != nil {
		return nil, err
	}
	// загрузка из файла — тоже правка: в истории остаётся снимок с автором загрузки
	if _, err := s.recordVersion(tx, &d, VersionImport, authorID, "", contentFromDocument(&d)); err != nil {
		return nil, err
	}
	item.Code, item.Slug, item.Revision = *d.Code, *d.Slug, d.Revision
	return item, nil
}

// nextObjectNumber — следующий свободный номер О-№ (после наибольшего существующего; О-0 не занимается автоматически).
func nextObjectNumber(tx *gorm.DB) (int, error) {
	var n int
	if err := tx.Raw("SELECT COALESCE(MAX(object_number), 0) + 1 FROM documents WHERE object_number IS NOT NULL").Scan(&n).Error; err != nil {
		return 0, err
	}
	if n > MaxObjectNumber {
		return 0, fmt.Errorf("исчерпаны номера объектов (О-%d)", MaxObjectNumber)
	}
	return n, nil
}

// addWarnings добавляет предупреждения о ссылках и отделах, которых (пока) нет в архиве.
// Это не ошибки: документы одного пакета могут ссылаться друг на друга в любом порядке, а цель
// ссылки может появиться позже.
func (s *Service) addWarnings(tx *gorm.DB, prepared []*prepared, report *ImportReport) error {
	wanted := map[string]bool{}
	for _, pr := range prepared {
		for _, c := range pr.LinkCodes {
			wanted[c] = true
		}
		if pr.DepartmentCode != "" {
			wanted[pr.DepartmentCode] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	codes := make([]string, 0, len(wanted))
	for c := range wanted {
		codes = append(codes, c)
	}
	var have []string
	if err := tx.Model(&Document{}).Where("code IN ?", codes).Pluck("code", &have).Error; err != nil {
		return err
	}
	exists := map[string]bool{}
	for _, c := range have {
		exists[c] = true
	}
	for i, pr := range prepared {
		it := &report.Items[i]
		for _, c := range pr.LinkCodes {
			if !exists[c] {
				it.Warnings = append(it.Warnings, fmt.Sprintf("ссылка на %s: такого документа пока нет в архиве", c))
			}
		}
		if pr.DepartmentCode != "" && !exists[pr.DepartmentCode] {
			it.Warnings = append(it.Warnings, fmt.Sprintf("отдел %s: такого документа пока нет в архиве", pr.DepartmentCode))
		}
	}
	return nil
}

// Export возвращает документ целиком (без фильтрации по допуску) в формате загрузки:
// его можно править и загрузить обратно.
func (s *Service) Export(ctx context.Context, ref string) ([]byte, error) {
	d, err := s.byRef(ctx, ref)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(inputFromDocument(d), "", "  ")
}

func inputFromDocument(d *Document) Input {
	level := d.Level
	in := Input{
		Type: d.Type, Title: d.Title, Status: d.Status, Level: &level, DirectLink: d.DirectLink, Grif: d.Grif,
		Composed: &Composed{Year: d.ComposedYear, Month: d.ComposedMonth, Day: d.ComposedDay},
	}
	if d.Code != nil {
		in.Code = *d.Code
	}
	if Type(d.Type) == TypeObject {
		p := &Props{DangerClass: d.DangerClass, DeviationPoints: d.DeviationPoints}
		if d.Department != nil {
			p.Department = *d.Department
		}
		if d.Category != nil {
			p.Category = *d.Category
		}
		if d.ContainmentStatus != nil {
			p.ContainmentStatus = *d.ContainmentStatus
		}
		if d.DiscoveryPlace != nil {
			p.DiscoveryPlace = *d.DiscoveryPlace
		}
		in.Props = p
	}
	in.Blocks = make([]InputBlock, len(d.Blocks))
	for i, b := range d.Blocks {
		in.Blocks[i] = InputBlock{ID: b.ID, Type: b.Type, Level: b.Level, Data: b.Data}
	}
	return in
}

// byRef находит документ по шифру в любой раскладке (для административных операций: без учёта допуска).
func (s *Service) byRef(ctx context.Context, ref string) (*Document, error) {
	c, err := ParseCode(ref)
	if err != nil {
		return nil, ErrNotFound
	}
	var d Document
	err = s.db.WithContext(ctx).Where("slug = ?", c.Slug).Take(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &d, err
}

// SetStatus меняет статус документа. При первой публикации фиксируется дата поступления в ЦАК.
func (s *Service) SetStatus(ctx context.Context, ref string, status Status) (*Document, error) {
	if !status.Valid() {
		return nil, fmt.Errorf("недопустимый статус %q: draft, review, published, archived", status)
	}
	var out *Document
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		c, err := ParseCode(ref)
		if err != nil {
			return ErrNotFound
		}
		var d Document
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("slug = ?", c.Slug).Take(&d).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if d.Status == string(status) {
			out = &d // уже в этом статусе: ничего не меняется и в историю ничего не пишется
			return nil
		}
		now := s.now()
		note := fmt.Sprintf("%s → %s", d.Status, status)
		d.Status = string(status)
		d.UpdatedAt = now
		if status == StatusPublished && d.PublishedAt == nil {
			d.PublishedAt = &now
		}
		if err := tx.Save(&d).Error; err != nil {
			return err
		}
		if _, err := s.recordVersion(tx, &d, VersionStatus, nil, note, contentFromDocument(&d)); err != nil {
			return err
		}
		out = &d
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.log.Info("статус документа изменён", "code", *out.Code, "status", out.Status)
	return out, nil
}

// Delete удаляет документ навсегда (вместе с историей чтения).
func (s *Service) Delete(ctx context.Context, ref string) error {
	c, err := ParseCode(ref)
	if err != nil {
		return ErrNotFound
	}
	res := s.db.WithContext(ctx).Where("slug = ?", c.Slug).Delete(&Document{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	s.log.Info("документ удалён", "code", c.Canonical)
	return nil
}

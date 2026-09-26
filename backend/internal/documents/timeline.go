package documents

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"kupol/internal/audit"
)

// Хронология «О КУПОЛЕ» (этап 4): события вселенной 1974 — наши дни. Ведут Редактор и Архивариус (право manage_timeline) и Директорат.
//
// У события свой уровень допуска: читатель видит в шкале только события не выше своего допуска (закрытого в ответе нет вовсе).
// Ссылка на документ показывается, только если читатель вправе открыть этот документ: иначе по ссылке можно было бы узнать о нём.

const (
	maxTimelineTitle = 200
	maxTimelineBody  = 2000
	minTimelineYear  = 1900
	maxTimelineYear  = 2099
)

// ErrEventNotFound — нет такого события хронологии.
var ErrEventNotFound = errors.New("documents: событие хронологии не найдено")

// TimelineEvent — запись хронологии (таблица timeline_events).
type TimelineEvent struct {
	ID           int64 `gorm:"primaryKey"`
	Year         int
	Month        *int
	Day          *int
	Title        string
	Body         string
	Level        int
	DocumentCode *string
	AuthorID     *int64
	CreatedAt    time.Time `gorm:"autoCreateTime:false"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime:false"`
}

func (TimelineEvent) TableName() string { return "timeline_events" }

// TimelineDoc — документ, к которому относится событие (только доступный читателю).
type TimelineDoc struct {
	Code  string `json:"code"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// TimelineItem — событие в шкале для читателя.
type TimelineItem struct {
	ID       int64        `json:"id"`
	Date     Composed     `json:"date"`
	Title    string       `json:"title"`
	Body     string       `json:"body,omitempty"`
	Level    int          `json:"level"`
	Document *TimelineDoc `json:"document,omitempty"`
}

// Timeline — шкала для читателя: события не выше его допуска по порядку дат.
func (s *Service) Timeline(ctx context.Context, v Viewer) ([]TimelineItem, error) {
	var events []TimelineEvent
	err := s.db.WithContext(ctx).Where("level <= ?", v.Level()).
		Order("year, month NULLS FIRST, day NULLS FIRST, id").Find(&events).Error
	if err != nil {
		return nil, err
	}
	return s.timelineItems(ctx, v, events)
}

func (s *Service) timelineItems(ctx context.Context, v Viewer, events []TimelineEvent) ([]TimelineItem, error) {
	var codes []string
	for _, e := range events {
		if e.DocumentCode != nil {
			codes = append(codes, *e.DocumentCode)
		}
	}
	docs := map[string]*TimelineDoc{}
	if len(codes) > 0 {
		var rows []Document
		err := visible(s.db.WithContext(ctx).Model(&Document{}), v).Where("code IN ?", codes).Select("code, slug, title").Find(&rows).Error
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			docs[deref(r.Code)] = &TimelineDoc{Code: deref(r.Code), Slug: deref(r.Slug), Title: r.Title}
		}
	}
	out := make([]TimelineItem, len(events))
	for i, e := range events {
		out[i] = TimelineItem{ID: e.ID, Date: Composed{Year: e.Year, Month: e.Month, Day: e.Day}, Title: e.Title, Body: e.Body, Level: e.Level}
		if e.DocumentCode != nil {
			out[i].Document = docs[*e.DocumentCode]
		}
	}
	return out, nil
}

// ——— team panel ———

// TimelineEventOut — событие для редактора: со всеми полями, включая шифр документа, как его ввели.
type TimelineEventOut struct {
	ID           int64     `json:"id"`
	Year         int       `json:"year"`
	Month        *int      `json:"month,omitempty"`
	Day          *int      `json:"day,omitempty"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	Level        int       `json:"level"`
	DocumentCode string    `json:"document_code,omitempty"`
	Author       *string   `json:"author,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
	// CanEdit — вправе ли человек менять и удалять события.
	CanEdit bool `json:"can_edit"`
}

// TimelineInput — что нужно, чтобы завести или изменить событие.
type TimelineInput struct {
	Year         int    `json:"year"`
	Month        *int   `json:"month,omitempty"`
	Day          *int   `json:"day,omitempty"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	Level        int    `json:"level"`
	DocumentCode string `json:"document_code"`
}

func (a Actor) canManageTimeline() bool { return a.Directorate || a.CanManageTimeline }

// checkEvent проверяет событие и возвращает его в каноническом виде.
func checkEvent(in TimelineInput) (TimelineInput, []Problem) {
	var p Problems
	out := TimelineInput{Year: in.Year, Month: in.Month, Day: in.Day, Title: strings.TrimSpace(in.Title), Body: strings.TrimSpace(in.Body), Level: in.Level}
	if in.Year < minTimelineYear || in.Year > maxTimelineYear {
		p.Add("year", "год — от %d до %d", minTimelineYear, maxTimelineYear)
	}
	switch {
	case in.Month != nil && (*in.Month < 1 || *in.Month > 12):
		p.Add("month", "месяц — от 1 до 12")
	case in.Day != nil && in.Month == nil:
		p.Add("day", "день без месяца указать нельзя")
	case in.Day != nil && !validDay(in.Year, *in.Month, *in.Day):
		p.Add("day", "в этом месяце нет такого дня")
	}
	p.text("title", out.Title, 1, maxTimelineTitle)
	if out.Body != "" {
		p.text("body", out.Body, 1, maxTimelineBody)
	}
	if in.Level < 0 || in.Level > MaxLevel {
		p.Add("level", "допуск — от 0 до %d", MaxLevel)
	}
	if code := strings.TrimSpace(in.DocumentCode); code != "" {
		c, err := ParseCode(code)
		if err != nil {
			p.Add("document_code", "%q — не шифр документа (примеры: О-041, ПРИКАЗ-1978-12)", code)
		} else {
			out.DocumentCode = c.Canonical
		}
	}
	return out, p.List()
}

func (s *Service) timelineOut(a Actor, e TimelineEvent, author *string) TimelineEventOut {
	return TimelineEventOut{
		ID: e.ID, Year: e.Year, Month: e.Month, Day: e.Day, Title: e.Title, Body: e.Body, Level: e.Level,
		DocumentCode: deref(e.DocumentCode), Author: author, UpdatedAt: e.UpdatedAt.UTC(), CanEdit: a.canManageTimeline(),
	}
}

type timelineRow struct {
	TimelineEvent
	AuthorLogin *string
}

func (s *Service) timelineRows(ctx context.Context, id int64) ([]timelineRow, error) {
	tx := s.db.WithContext(ctx).Table("timeline_events t").Select("t.*, u.login::text AS author_login").
		Joins("LEFT JOIN users u ON u.id = t.author_id").Order("t.year, t.month NULLS FIRST, t.day NULLS FIRST, t.id")
	if id > 0 {
		tx = tx.Where("t.id = ?", id)
	}
	var rows []timelineRow
	return rows, tx.Scan(&rows).Error
}

// TimelineList — все события для редактирования, в том числе закрытые уровнем: читают члены команды (Гражданину и посторонним — нет,
// это проверяет HTTP-слой правом team_panel).
func (s *Service) TimelineList(ctx context.Context, a Actor) ([]TimelineEventOut, error) {
	rows, err := s.timelineRows(ctx, 0)
	if err != nil {
		return nil, err
	}
	out := make([]TimelineEventOut, len(rows))
	for i, r := range rows {
		out[i] = s.timelineOut(a, r.TimelineEvent, r.AuthorLogin)
	}
	return out, nil
}

func (s *Service) timelineOne(ctx context.Context, a Actor, id int64) (*TimelineEventOut, error) {
	rows, err := s.timelineRows(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrEventNotFound
	}
	o := s.timelineOut(a, rows[0].TimelineEvent, rows[0].AuthorLogin)
	return &o, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// TimelineCreate добавляет событие. Нужно право вести хронологию.
func (s *Service) TimelineCreate(ctx context.Context, a Actor, in TimelineInput) (*TimelineEventOut, error) {
	if !a.canManageTimeline() {
		return nil, ErrForbidden
	}
	in, problems := checkEvent(in)
	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}
	now := s.now()
	uid := a.UserID
	e := &TimelineEvent{Year: in.Year, Month: in.Month, Day: in.Day, Title: in.Title, Body: in.Body, Level: in.Level, DocumentCode: nilIfEmpty(in.DocumentCode), AuthorID: &uid, CreatedAt: now, UpdatedAt: now}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(e).Error; err != nil {
			return err
		}
		return audit.Record(tx, now, audit.TimelineCreated, audit.Event{ActorID: &uid, Details: audit.Details("event_id", e.ID, "title", e.Title, "year", e.Year)})
	})
	if err != nil {
		return nil, err
	}
	return s.timelineOne(ctx, a, e.ID)
}

// TimelineUpdate заменяет событие целиком.
func (s *Service) TimelineUpdate(ctx context.Context, a Actor, id int64, in TimelineInput) (*TimelineEventOut, error) {
	if !a.canManageTimeline() {
		return nil, ErrForbidden
	}
	in, problems := checkEvent(in)
	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var e TimelineEvent
		if err := tx.Take(&e, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrEventNotFound
		} else if err != nil {
			return err
		}
		now := s.now()
		upd := map[string]any{"year": in.Year, "month": in.Month, "day": in.Day, "title": in.Title, "body": in.Body, "level": in.Level, "document_code": nilIfEmpty(in.DocumentCode), "updated_at": now}
		if err := tx.Model(&TimelineEvent{}).Where("id = ?", id).Updates(upd).Error; err != nil {
			return err
		}
		uid := a.UserID
		return audit.Record(tx, now, audit.TimelineUpdated, audit.Event{ActorID: &uid, Details: audit.Details("event_id", id, "title", in.Title, "was", e.Title)})
	})
	if err != nil {
		return nil, err
	}
	return s.timelineOne(ctx, a, id)
}

// TimelineDelete удаляет событие.
func (s *Service) TimelineDelete(ctx context.Context, a Actor, id int64) error {
	if !a.canManageTimeline() {
		return ErrForbidden
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var e TimelineEvent
		if err := tx.Take(&e, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrEventNotFound
		} else if err != nil {
			return err
		}
		if err := tx.Delete(&TimelineEvent{}, id).Error; err != nil {
			return err
		}
		uid := a.UserID
		return audit.Record(tx, s.now(), audit.TimelineDeleted, audit.Event{ActorID: &uid, Details: audit.Details("event_id", id, "title", e.Title)})
	})
}

// Package suggestions — «Предложения» читателей (шаг 5.4, спецификация §8): одна форма (идея или замечание),
// очередь Редакторов в панели команды, статусы получено → рассмотрено → принято/отклонено. Принятое предложение
// приносит автору xp.SuggestionXP (не больше xp.SuggestionDailyCap в день).
//
// «Записка» автору — статус и пояснение Редактора в его собственном списке предложений; доставка отдельными
// записками внутренней почты появится вместе с шагом 5.7.
package suggestions

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"kupol/internal/inbox"
	"kupol/internal/xp"
)

const (
	minTextLen      = 1
	maxTextLen      = 2000
	maxCommentLen   = 2000
	defaultPerPage  = 50
	maxPerPage      = 100
	maxSuggestionsN = 200 // сколько максимум предложений отдаётся автору в «моих»
)

// Status — состояние предложения в очереди Редакторов.
type Status string

const (
	StatusReceived Status = "received" // получено: ждёт разбора
	StatusReviewed Status = "reviewed" // рассмотрено: Редактор заглянул, решения ещё нет
	StatusAccepted Status = "accepted" // принято: +100 XP автору
	StatusRejected Status = "rejected" // отклонено
)

// transitions — допустимые переводы: только вперёд по цепочке получено → рассмотрено → принято/отклонено;
// «принято» и «отклонено» окончательны (иначе XP за принятие можно было бы получить повторно).
var transitions = map[Status][]Status{
	StatusReceived: {StatusReviewed, StatusAccepted, StatusRejected},
	StatusReviewed: {StatusAccepted, StatusRejected},
	StatusAccepted: {},
	StatusRejected: {},
}

var knownStatuses = map[Status]bool{
	StatusReceived: true, StatusReviewed: true, StatusAccepted: true, StatusRejected: true,
}

// Valid — известен ли статус (для проверки параметров запроса и тел).
func (s Status) Valid() bool { return knownStatuses[s] }

// allowed — допустим ли перевод из from в to.
func allowed(from, to Status) bool {
	for _, next := range transitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// Ошибки перехода. Тексты — внутренние (ошибки читателю отдаёт httpapi на языке запроса).
var (
	ErrNotFound     = errors.New("suggestions: предложение не найдено")
	ErrForbidden    = errors.New("suggestions: недостаточно прав")
	ErrSelfReview   = errors.New("suggestions: своё предложение рассматривает другой Редактор")
	ErrInvalidState = errors.New("suggestions: такой перевод статуса недопустим")
)

// ValidationError — форма не прошла проверку. Сообщения русские, они же ключи перевода (их отдаёт httpapi).
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for k, v := range e.Fields {
		parts = append(parts, k+": "+v)
	}
	return "suggestions: " + strings.Join(parts, "; ")
}

func fieldError(field, msg string) *ValidationError {
	return &ValidationError{Fields: map[string]string{field: msg}}
}

type row struct {
	ID        int64 `gorm:"primaryKey"`
	AuthorID  int64
	Text      string
	Status    string
	Comment   string
	HandledBy *int64
	CreatedAt time.Time
	HandledAt *time.Time
}

func (row) TableName() string { return "suggestions" }

// Out — предложение в ответе: автору (список «мои предложения») и команде (очередь).
type Out struct {
	ID     int64  `json:"id"`
	Author string `json:"author,omitempty"` // логин автора; в очереди команды — всегда, у автора своего списка — тоже
	Text   string `json:"text"`
	Status Status `json:"status"`
	// Comment — пояснение Редактора при переводе статуса: это и есть «записка» автору.
	Comment   string     `json:"comment,omitempty"`
	HandledBy string     `json:"handled_by,omitempty"` // кто перевёл в нынешний статус (пусто, пока не рассматривали)
	CreatedAt time.Time  `json:"created_at"`
	HandledAt *time.Time `json:"handled_at,omitempty"`
}

// Query — параметры очереди команды (GET /api/team/suggestions).
type Query struct {
	Status  Status
	Page    int
	PerPage int
}

// List — страница очереди.
type List struct {
	Items   []Out `json:"items"`
	Total   int64 `json:"total"`
	Page    int   `json:"page"`
	PerPage int   `json:"per_page"`
	Pages   int   `json:"pages"`
}

// Service — предложения читателей.
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

// joined — строка предложения с логинами: автора и того, кто вёл предложение.
type joined struct {
	ID        int64
	AuthorID  int64
	Text      string
	Status    string
	Comment   string
	HandledBy *int64
	CreatedAt time.Time
	HandledAt *time.Time
	Author    string
	Handler   string
}

func toOut(r joined) Out {
	return Out{
		ID: r.ID, Author: r.Author, Text: r.Text, Status: Status(r.Status), Comment: r.Comment,
		HandledBy: r.Handler, CreatedAt: r.CreatedAt.UTC(), HandledAt: r.HandledAt,
	}
}

// joinOut — запрос предложения с логинами. LEFT JOIN на ведущего: у нерассмотренного его ещё нет,
// а после сдачи дела в архив логина нет, но само предложение остаётся.
func joinOut(db *gorm.DB) *gorm.DB {
	return db.Table("suggestions s").
		Select("s.id, s.author_id, s.text, s.status, s.comment, s.handled_by, s.created_at, s.handled_at," +
			" au.login AS author, hu.login AS handler").
		Joins("JOIN users au ON au.id = s.author_id").
		Joins("LEFT JOIN users hu ON hu.id = s.handled_by")
}

// Create принимает предложение вошедшего читателя (одна форма: идея или замечание) — статус «получено».
// XP не начисляется: оценку предложения держит Редактор (спецификация §5: XP при принятии).
func (s *Service) Create(ctx context.Context, authorID int64, text string) (*Out, error) {
	if authorID == 0 {
		return nil, ErrForbidden
	}
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n < minTextLen || n > maxTextLen {
		return nil, fieldError("text", "Предложение: от 1 до 2000 знаков")
	}
	row := row{AuthorID: authorID, Text: text, Status: string(StatusReceived), CreatedAt: s.now()}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return s.byID(ctx, row.ID)
}

// Mine — собственные предложения автора, новые сверху (до maxSuggestionsN).
func (s *Service) Mine(ctx context.Context, authorID int64) ([]Out, error) {
	if authorID == 0 {
		return nil, ErrForbidden
	}
	var rows []joined
	err := joinOut(s.db.WithContext(ctx)).
		Where("s.author_id = ?", authorID).
		Order("s.created_at DESC, s.id DESC").
		Limit(maxSuggestionsN).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]Out, len(rows))
	for i, r := range rows {
		out[i] = toOut(r)
	}
	return out, nil
}

// byID собирает предложение с логинами; нет его — ErrNotFound.
func (s *Service) byID(ctx context.Context, id int64) (*Out, error) {
	var r joined
	if err := joinOut(s.db.WithContext(ctx)).Where("s.id = ?", id).Take(&r).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	out := toOut(r)
	return &out, nil
}

// Queue — очередь для Редакторов: сначала нерассмотренные (старейшие), затем решённые; фильтр по статусу.
func (s *Service) Queue(ctx context.Context, q Query) (*List, error) {
	if q.Status != "" && !q.Status.Valid() {
		return nil, fieldError("status", "Неизвестный статус предложения")
	}
	if q.Page < 0 {
		return nil, fieldError("page", "номер страницы не может быть отрицательным")
	}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PerPage == 0 {
		q.PerPage = defaultPerPage
	}
	if q.PerPage < 0 || q.PerPage > maxPerPage {
		return nil, fieldError("per_page", "размер страницы — от 1 до 100")
	}

	filter := func(db *gorm.DB) *gorm.DB {
		if q.Status != "" {
			return db.Where("s.status = ?", string(q.Status))
		}
		return db
	}
	var total int64
	if err := filter(s.db.WithContext(ctx).Table("suggestions s")).Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []joined
	err := filter(joinOut(s.db.WithContext(ctx))).
		Order("CASE WHEN s.status IN ('accepted', 'rejected') THEN 1 ELSE 0 END, s.created_at, s.id").
		Limit(q.PerPage).Offset((q.Page - 1) * q.PerPage).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]Out, len(rows))
	for i, r := range rows {
		items[i] = toOut(r)
	}
	pages := int((total + int64(q.PerPage) - 1) / int64(q.PerPage))
	return &List{Items: items, Total: total, Page: q.Page, PerPage: q.PerPage, Pages: pages}, nil
}

// SetStatus переводит предложение в новый статус (вызывает Редактор или Директорат — право проверяет httpapi).
// comment — пояснение автору («записка»); пустое пояснение не затирает уже написанное. Переход в «принято»
// начисляет автору XP в той же транзакции; возврат из «принято»/«отклонено» невозможен, поэтому XP не повторяется.
func (s *Service) SetStatus(ctx context.Context, handlerID, id int64, status Status, comment string) (*Out, error) {
	if handlerID == 0 {
		return nil, ErrForbidden
	}
	if !status.Valid() {
		return nil, fieldError("status", "Неизвестный статус предложения")
	}
	comment = strings.TrimSpace(comment)
	if n := utf8.RuneCountInString(comment); n > maxCommentLen {
		return nil, fieldError("comment", "Пояснение: до 2000 знаков")
	}

	now := s.now()
	var out *Out
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var r row
		// SELECT … FOR UPDATE: два Редакторов не должны перевести предложение дважды (двойной XP).
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&r).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if r.AuthorID == handlerID {
			return ErrSelfReview
		}
		if !allowed(Status(r.Status), status) {
			return ErrInvalidState
		}
		updates := map[string]any{
			"status": string(status), "handled_by": handlerID, "handled_at": now,
		}
		if comment != "" {
			updates["comment"] = comment
		}
		if err := tx.Model(&row{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		if status == StatusAccepted {
			if _, err := xp.Award(ctx, tx, r.AuthorID, xp.SourceSuggestion, xp.SuggestionXP, xp.SuggestionDailyCap, now); err != nil {
				return err
			}
		}
		verdicts := map[Status]string{StatusReviewed: "рассмотрено", StatusAccepted: "принято", StatusRejected: "отклонено"}
		if err := inbox.Send(ctx, tx, r.AuthorID, inbox.KindSuggestion, map[string]any{"status": verdicts[status], "comment": comment}, now); err != nil {
			return err
		}
		var j joined
		if err := joinOut(tx).Where("s.id = ?", id).Take(&j).Error; err != nil {
			return err
		}
		got := toOut(j)
		out = &got
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.log.Info("предложение переведено в статус", "id", out.ID, "status", out.Status)
	return out, nil
}

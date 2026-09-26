package documents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"kupol/internal/audit"
	"kupol/internal/i18n"
)

// Ход документа (team panel, шаг 3.5): черновик → на проверке → опубликован → архив.
//
//	Автор       отправляет черновик на проверку (submit) и может забрать его обратно, пока вердикта нет (withdraw);
//	Редактор    выносит вердикт по чужому документу (review): принять (approve, то есть опубликовать), вернуть на
//	            доработку (return) или отклонить (reject) — причина обязательна;
//	Редактор    убирает опубликованное в архив и возвращает из архива (archive, unarchive);
//	Директорат  может всё, в том числе проверять собственные документы.
//
// «Принять» и «опубликовать» — одно действие: состояния «принято, но не опубликовано» нет. Свой документ Редактор
// не проверяет — нужна вторая пара глаз. Отправка и публикация проходят линтер канона (lint.go): ошибки не пропускают.

// Действия хода документа (для прав).
const (
	ActionSubmit    = "submit"
	ActionWithdraw  = "withdraw"
	ActionReview    = "review"
	ActionArchive   = "archive"
	ActionUnarchive = "unarchive"
)

// ReviewKind — вид события хода рецензии (таблица review_events).
type ReviewKind string

const (
	ReviewSubmit    ReviewKind = "submit"
	ReviewWithdraw  ReviewKind = "withdraw"
	ReviewApprove   ReviewKind = "approve"
	ReviewReturn    ReviewKind = "return"
	ReviewReject    ReviewKind = "reject"
	ReviewArchive   ReviewKind = "archive"
	ReviewUnarchive ReviewKind = "unarchive"
)

var reviewKindNames = map[ReviewKind]string{
	ReviewSubmit: "Отправлен на проверку", ReviewWithdraw: "Забран с проверки", ReviewApprove: "Принят и опубликован",
	ReviewReturn: "Возвращён на доработку", ReviewReject: "Отклонён", ReviewArchive: "Убран в архив", ReviewUnarchive: "Возвращён из архива",
}

// Name — название события для интерфейса.
func (k ReviewKind) Name() string { return reviewKindNames[k] }

// NameIn — название на языке l.
func (k ReviewKind) NameIn(l i18n.Lang) string { return l.Translate(reviewKindNames[k]) }

// MaxReviewText — предел причины и комментария (знаков).
const MaxReviewText = 2000

var (
	// ErrSelfReview — Редактор пытается проверить собственный документ.
	ErrSelfReview = errors.New("documents: свой документ проверяет другой Редактор")
)

// StateError — действие не подходит документу в его нынешнем статусе (например, вердикт по черновику).
type StateError struct {
	Status string
	Action string
}

func (e *StateError) Error() string {
	return fmt.Sprintf("documents: действие %q не подходит документу в статусе %q", e.Action, e.Status)
}

// Workflow — что человек может сделать с документом прямо сейчас. Права считает сервер, интерфейс их только показывает.
type Workflow struct {
	Submit    bool `json:"submit"`
	Withdraw  bool `json:"withdraw"`
	Review    bool `json:"review"`
	Comment   bool `json:"comment"`
	Archive   bool `json:"archive"`
	Unarchive bool `json:"unarchive"`
}

// roleAllows — вправе ли человек по роли (без учёта статуса) совершить действие над этим документом.
func (a Actor) roleAllows(d *Document, action string) bool {
	switch action {
	case ActionSubmit, ActionWithdraw:
		return a.Directorate || (a.owns(d) && a.CanWrite)
	case ActionReview:
		return a.Directorate || (a.CanReview && !a.owns(d))
	case ActionArchive, ActionUnarchive:
		return a.Directorate || a.CanEditPublished
	}
	return false
}

// statusAllows — подходит ли действие статусу документа.
func statusAllows(status Status, action string) bool {
	switch action {
	case ActionSubmit:
		return status == StatusDraft
	case ActionWithdraw, ActionReview:
		return status == StatusReview
	case ActionArchive:
		return status == StatusPublished
	case ActionUnarchive:
		return status == StatusArchived
	}
	return false
}

// authorize: nil — можно; ErrSelfReview — Редактор проверяет своё; ErrForbidden — роли не хватает; *StateError — не тот статус.
func (a Actor) authorize(d *Document, action string) error {
	if !a.roleAllows(d, action) {
		if action == ActionReview && a.CanReview && a.owns(d) {
			return ErrSelfReview
		}
		return ErrForbidden
	}
	if !statusAllows(Status(d.Status), action) {
		return &StateError{Status: d.Status, Action: action}
	}
	return nil
}

// Workflow собирает права человека на документ.
func (a Actor) Workflow(d *Document) Workflow {
	can := func(action string) bool { return a.authorize(d, action) == nil }
	return Workflow{
		Submit: can(ActionSubmit), Withdraw: can(ActionWithdraw), Review: can(ActionReview),
		// комментировать вправе тот же, кто выносит вердикт, пока документ на проверке
		Comment: can(ActionReview), Archive: can(ActionArchive), Unarchive: can(ActionUnarchive),
	}
}

// ReviewEvent — запись хода рецензии (таблица review_events).
type ReviewEvent struct {
	ID         int64 `gorm:"primaryKey"`
	DocumentID int64
	Kind       string
	Revision   int
	ActorID    *int64
	Comment    string
	CreatedAt  time.Time `gorm:"autoCreateTime:false"`
}

func (ReviewEvent) TableName() string { return "review_events" }

// transition — описание одного перехода документа.
type transition struct {
	action       string // право (Action*)
	kind         ReviewKind
	to           Status
	audit        audit.Action
	baseRevision *int // редакция, на которую опирался человек; nil — не проверяется
	comment      string
	needReason   bool // возврат и отклонение без причины бессмысленны
	lint         bool // проверить канон: ошибки не пропускают
	publish      bool // присвоить номер О-№ (если нужен) и дату публикации
}

// move выполняет переход в одной транзакции: проверка прав и состояния, запись статуса, снимок истории, событие
// рецензии, событие журнала. Замок снимается: после перехода документ никто не «правит».
func (s *Service) move(ctx context.Context, a Actor, id int64, t transition) (*TeamDocument, error) {
	comment := strings.TrimSpace(t.comment)
	if utf8.RuneCountInString(comment) > MaxReviewText {
		return nil, oneProblem("comment", "слишком длинный текст (не больше %d знаков)", MaxReviewText)
	}

	var out *TeamDocument
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		d, err := s.lockedDoc(tx, a, id, false)
		if err != nil {
			return err
		}
		if err := a.authorize(d, t.action); err != nil {
			return err
		}
		if t.baseRevision != nil && *t.baseRevision != d.Revision {
			return &ConflictError{CurrentRevision: d.Revision}
		}
		if t.needReason && comment == "" {
			return oneProblem("comment", "Укажите причину: автору нужно знать, что исправить")
		}
		lock, err := s.activeLock(tx, d.ID, a)
		if err != nil {
			return err
		}
		if lock != nil && !lock.Mine {
			return &LockedError{Holder: lock.Holder, ExpiresAt: lock.ExpiresAt}
		}
		if t.lint {
			rep, err := lintDocument(tx, d)
			if err != nil {
				return err
			}
			if rep.HasErrors() {
				return &LintFailedError{Report: rep}
			}
		}

		now := s.now()
		from := d.Status
		if t.publish {
			if d.Code == nil { // объект без шифра: номер О-№ присваивается при публикации, по порядку
				if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('kupol.object_number'))").Error; err != nil {
					return err
				}
				n, err := nextObjectNumber(tx)
				if err != nil {
					return err
				}
				c := ObjectCode(n)
				d.Code, d.Slug, d.ObjectNumber = &c.Canonical, &c.Slug, c.ObjectNumber
			}
			if d.PublishedAt == nil {
				d.PublishedAt = &now
			}
		}
		d.Status = string(t.to)
		d.UpdatedAt = now
		if err := tx.Save(d).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrCodeTaken
			}
			return err
		}
		if t.publish { // шифр мог быть присвоен только что: он входит в строку названия
			if err := reindexDocument(tx, d); err != nil {
				return err
			}
		}

		uid := a.UserID
		note := fmt.Sprintf("%s → %s", from, t.to)
		if comment != "" {
			note += ": " + comment
		}
		if _, err := s.recordVersion(tx, d, VersionStatus, &uid, note, contentFromDocument(d)); err != nil {
			return err
		}
		if err := tx.Create(&ReviewEvent{DocumentID: d.ID, Kind: string(t.kind), Revision: d.Revision, ActorID: &uid, Comment: comment, CreatedAt: now}).Error; err != nil {
			return err
		}
		details := audit.Details("from", from, "to", string(t.to), "revision", d.Revision, "code", deref(d.Code))
		if err := audit.Record(tx, now, t.audit, audit.Event{ActorID: &uid, DocumentID: &d.ID, Details: details}); err != nil {
			return err
		}
		if err := tx.Where("document_id = ?", d.ID).Delete(&Lock{}).Error; err != nil {
			return err
		}
		out, err = s.teamDocument(tx, a, d)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.log.Info("документ переведён", "document_id", id, "action", t.kind, "by", a.Login)
	return out, nil
}

// Submit отправляет черновик на проверку. baseRevision — редакция, которую видел автор: отправляется то, что он видел.
// Не пройдёт, если линтер нашёл ошибки (*LintFailedError).
func (s *Service) Submit(ctx context.Context, a Actor, id int64, baseRevision int) (*TeamDocument, error) {
	return s.move(ctx, a, id, transition{
		action: ActionSubmit, kind: ReviewSubmit, to: StatusReview, audit: audit.DocumentSubmitted, baseRevision: &baseRevision, lint: true,
	})
}

// Withdraw забирает документ с проверки обратно в черновик (пока вердикта нет).
func (s *Service) Withdraw(ctx context.Context, a Actor, id int64, comment string) (*TeamDocument, error) {
	return s.move(ctx, a, id, transition{action: ActionWithdraw, kind: ReviewWithdraw, to: StatusDraft, audit: audit.DocumentWithdrawn, comment: comment})
}

// Verdict — вердикт рецензента.
type Verdict string

const (
	VerdictApprove Verdict = "approve" // принять и опубликовать
	VerdictReturn  Verdict = "return"  // вернуть на доработку (причина обязательна)
	VerdictReject  Verdict = "reject"  // отклонить, документ уходит в архив (причина обязательна)
)

// Valid — известный вердикт.
func (v Verdict) Valid() bool { return v == VerdictApprove || v == VerdictReturn || v == VerdictReject }

// Decide выносит вердикт по документу на проверке. baseRevision — редакция, которую читал рецензент: если автор успел
// поправить текст, вердикт по старому не выносится (ConflictError). «Принять» публикует документ и присваивает номер О-№.
func (s *Service) Decide(ctx context.Context, a Actor, id int64, v Verdict, comment string, baseRevision int) (*TeamDocument, error) {
	t := transition{action: ActionReview, comment: comment, baseRevision: &baseRevision}
	switch v {
	case VerdictApprove:
		t.kind, t.to, t.audit, t.lint, t.publish = ReviewApprove, StatusPublished, audit.DocumentPublished, true, true
	case VerdictReturn:
		t.kind, t.to, t.audit, t.needReason = ReviewReturn, StatusDraft, audit.DocumentReturned, true
	case VerdictReject:
		t.kind, t.to, t.audit, t.needReason = ReviewReject, StatusArchived, audit.DocumentRejected, true
	default:
		return nil, oneProblem("verdict", "вердикт: approve, return или reject")
	}
	return s.move(ctx, a, id, t)
}

// Archive убирает опубликованный документ в архив; Unarchive возвращает его в опубликованные.
func (s *Service) Archive(ctx context.Context, a Actor, id int64, comment string) (*TeamDocument, error) {
	return s.move(ctx, a, id, transition{action: ActionArchive, kind: ReviewArchive, to: StatusArchived, audit: audit.DocumentArchived, comment: comment})
}

func (s *Service) Unarchive(ctx context.Context, a Actor, id int64, comment string) (*TeamDocument, error) {
	return s.move(ctx, a, id, transition{action: ActionUnarchive, kind: ReviewUnarchive, to: StatusPublished, audit: audit.DocumentUnarchived, comment: comment})
}

// Пакет audit — журнал событий: кто, когда и что сделал с ролями, замками, откатами и паролями.
//
// Запись делается в той же транзакции, что и само действие: не бывает роли, выданной без записи в журнале,
// и записи о том, что не случилось. Подробности — только то, что нужно для разбора (роль, версия, прежний владелец
// замка); паролей, кодов и содержимого документов в журнале нет.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Action — машинное имя события.
type Action string

const (
	RoleGranted        Action = "role.granted"              // выдана роль команды
	RoleRevoked        Action = "role.revoked"              // снята роль команды
	LockBroken         Action = "lock.broken"               // снят чужой замок документа
	DocumentRolledBack Action = "document.rolled_back"      // документ откачен к прежней версии
	PublishedEdited    Action = "document.published_edited" // правка опубликованного или архивного «на месте»
	PasswordChanged    Action = "account.password_changed"  // пароль сменён владельцем
	AccessRestored     Action = "account.access_restored"   // доступ восстановлен по резервному коду
	PasswordReset      Action = "account.password_reset"    // пароль сброшен автором командой на сервере

	DocumentSubmitted  Action = "document.submitted"  // документ отправлен на проверку
	DocumentWithdrawn  Action = "document.withdrawn"  // автор забрал документ с проверки
	DocumentPublished  Action = "document.published"  // документ опубликован (вердикт «принять»)
	DocumentReturned   Action = "document.returned"   // документ возвращён на доработку
	DocumentRejected   Action = "document.rejected"   // документ отклонён и убран в архив
	DocumentArchived   Action = "document.archived"   // опубликованный документ убран в архив
	DocumentUnarchived Action = "document.unarchived" // документ возвращён из архива в опубликованные
	DocumentDeleted    Action = "document.deleted"    // документ удалён безвозвратно, его шифр освобождён

	TemplateCreated Action = "template.created" // заведён шаблон документа или набор блоков
	TemplateUpdated Action = "template.updated" // шаблон изменён
	TemplateDeleted Action = "template.deleted" // шаблон удалён

	GlossaryCreated Action = "glossary.created" // заведён термин глоссария
	GlossaryUpdated Action = "glossary.updated" // термин изменён
	GlossaryDeleted Action = "glossary.deleted" // термин удалён

	SiteUpdated Action = "site.updated" // изменены настройки сайта (контакты автора)

	SecretCodeCreated  Action = "secret_code.created"  // заведён скрытый код (пасхалка)
	SecretCodeDeleted  Action = "secret_code.deleted"  // скрытый код удалён
	SecretCodeRedeemed Action = "secret_code.redeemed" // код погашен читателем

	InboxNoteSent Action = "inbox.note_sent" // Директорат отправил записку (одному или всем)

	InvitationSent     Action = "invitation.sent"     // Совет пригласил читателя на следующий уровень
	InvitationAccepted Action = "invitation.accepted" // читатель принял приглашение (уровень поднят)
	SanctionIssued     Action = "sanction.issued"     // наложено наказание (предупреждение, блокировка комментариев, бан)
	SanctionRevoked    Action = "sanction.revoked"    // наказание снято

	PetitionDecided Action = "petition.decided" // решение по ходатайству о допуске (одобрено или отклонено)

	TimelineCreated Action = "timeline.created" // добавлено событие хронологии
	TimelineUpdated Action = "timeline.updated" // событие хронологии изменено
	TimelineDeleted Action = "timeline.deleted" // событие хронологии удалено

	TOTPEnabled      Action = "account.totp_enabled"       // включён код из приложения
	TOTPDisabled     Action = "account.totp_disabled"      // код из приложения выключен владельцем
	TOTPReset        Action = "account.totp_reset"         // код из приложения снят автором командой на сервере
	TOTPCodesRenewed Action = "account.totp_codes_renewed" // выданы новые коды на случай потери телефона
	TOTPRecoveryUsed Action = "account.totp_recovery_used" // вход по одноразовому коду вместо кода из приложения

	LevelPromoted Action = "account.level_promoted" // уровень поднялся автоматически по XP (1→2 или 2→3)

	EmailConfirmed Action = "account.email_confirmed" // почта подтверждена по ссылке из письма
)

var titles = map[Action]string{
	RoleGranted:        "Выдана роль",
	RoleRevoked:        "Снята роль",
	LockBroken:         "Снят чужой замок",
	DocumentRolledBack: "Откат документа",
	PublishedEdited:    "Правка опубликованного",
	PasswordChanged:    "Смена пароля",
	AccessRestored:     "Восстановление доступа",
	PasswordReset:      "Сброс пароля",

	DocumentSubmitted:  "Документ отправлен на проверку",
	DocumentWithdrawn:  "Документ забран с проверки",
	DocumentPublished:  "Документ опубликован",
	DocumentReturned:   "Документ возвращён на доработку",
	DocumentRejected:   "Документ отклонён",
	DocumentArchived:   "Документ убран в архив",
	DocumentUnarchived: "Документ возвращён из архива",
	DocumentDeleted:    "Документ удалён",

	TemplateCreated: "Заведён шаблон",
	TemplateUpdated: "Шаблон изменён",
	TemplateDeleted: "Шаблон удалён",

	GlossaryCreated: "Заведён термин глоссария",
	GlossaryUpdated: "Термин глоссария изменён",
	GlossaryDeleted: "Термин глоссария удалён",

	SiteUpdated: "Изменены настройки сайта",

	SecretCodeCreated:  "Заведён скрытый код",
	SecretCodeDeleted:  "Скрытый код удалён",
	SecretCodeRedeemed: "Скрытый код погашен",

	InboxNoteSent: "Отправлена записка Директората",

	InvitationSent:     "Отправлено приглашение Совета",
	InvitationAccepted: "Приглашение Совета принято",
	SanctionIssued:     "Наложено наказание",
	SanctionRevoked:    "Наказание снято",

	PetitionDecided: "Решение по ходатайству о допуске",

	TimelineCreated: "Добавлено событие хронологии",
	TimelineUpdated: "Изменено событие хронологии",
	TimelineDeleted: "Удалено событие хронологии",

	TOTPEnabled:      "Включён код из приложения",
	TOTPDisabled:     "Код из приложения выключен",
	TOTPReset:        "Код из приложения снят администратором",
	TOTPCodesRenewed: "Выданы новые одноразовые коды",
	TOTPRecoveryUsed: "Вход по одноразовому коду",

	LevelPromoted: "Уровень повышен по XP",

	EmailConfirmed: "Почта подтверждена",
}

// Title — название события для людей; для неизвестного — само машинное имя.
func (a Action) Title() string {
	if t, ok := titles[a]; ok {
		return t
	}
	return string(a)
}

// Event — запись журнала (таблица audit_events).
type Event struct {
	ID           int64 `gorm:"primaryKey"`
	At           time.Time
	Action       string
	ActorID      *int64
	TargetUserID *int64
	DocumentID   *int64
	// Details — JSON-объект с подробностями; собирается через Details().
	Details string `gorm:"type:jsonb"`
}

func (Event) TableName() string { return "audit_events" }

// Details собирает подробности события из пар «ключ, значение».
func Details(pairs ...any) string {
	if len(pairs)%2 != 0 {
		panic("audit.Details: нечётное число аргументов")
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			panic(fmt.Sprintf("audit.Details: ключ %v не строка", pairs[i]))
		}
		m[key] = pairs[i+1]
	}
	raw, err := json.Marshal(m)
	if err != nil {
		panic("audit.Details: " + err.Error())
	}
	return string(raw)
}

// Record записывает событие в переданной транзакции (или соединении). at — момент из часов сервиса.
func Record(db *gorm.DB, at time.Time, action Action, e Event) error {
	if action == "" {
		return errors.New("audit: пустое событие")
	}
	e.At = at.UTC()
	e.Action = string(action)
	if e.Details == "" {
		e.Details = "{}"
	}
	return db.Create(&e).Error
}

// Row — строка журнала для показа: с логинами вместо номеров (у удалённых аккаунтов логина нет).
type Row struct {
	ID         int64     `json:"id"`
	At         time.Time `json:"at"`
	Action     string    `json:"action"`
	Title      string    `json:"title"`
	Actor      *string   `json:"actor,omitempty"`
	Target     *string   `json:"target,omitempty"`
	DocumentID *int64    `json:"document_id,omitempty"`
	Details    string    `json:"details"`
}

// Query — отбор записей журнала.
type Query struct {
	Action     Action
	DocumentID int64
	Limit      int
}

const defaultLimit, maxLimit = 50, 500

type rawRow struct {
	Event
	ActorLogin  *string
	TargetLogin *string
}

// List возвращает записи журнала, новые сверху.
func List(ctx context.Context, db *gorm.DB, q Query) ([]Row, error) {
	limit := q.Limit
	switch {
	case limit == 0:
		limit = defaultLimit
	case limit < 0 || limit > maxLimit:
		return nil, fmt.Errorf("audit: размер выборки — от 1 до %d", maxLimit)
	}
	tx := db.WithContext(ctx).Table("audit_events e").
		Select("e.id, e.at, e.action, e.actor_id, e.target_user_id, e.document_id, e.details::text AS details, a.login AS actor_login, t.login AS target_login").
		Joins("LEFT JOIN users a ON a.id = e.actor_id").
		Joins("LEFT JOIN users t ON t.id = e.target_user_id").
		Order("e.at DESC, e.id DESC").Limit(limit)
	if q.Action != "" {
		tx = tx.Where("e.action = ?", string(q.Action))
	}
	if q.DocumentID != 0 {
		tx = tx.Where("e.document_id = ?", q.DocumentID)
	}
	var raws []rawRow
	if err := tx.Scan(&raws).Error; err != nil {
		return nil, err
	}
	rows := make([]Row, len(raws))
	for i, r := range raws {
		rows[i] = Row{
			ID: r.ID, At: r.At.UTC(), Action: r.Event.Action, Title: Action(r.Event.Action).Title(),
			Actor: r.ActorLogin, Target: r.TargetLogin, DocumentID: r.DocumentID, Details: r.Details,
		}
	}
	return rows, nil
}

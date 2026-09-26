// Package inbox — внутренняя почта (шаг 5.7): записки читателю в интерфейсе. Независимый пакет (как xp и
// achievements): импортируют его accounts, documents, suggestions и achievements, он их — нет.
//
// В базе — вид записки и параметры; текст собирается при чтении на языке читателя. Исключение — записка
// Директората: её текст пишет человек, он хранится как есть (как содержимое документов) и не переводится.
package inbox

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"kupol/internal/i18n"
)

// Kind — вид записки.
type Kind string

const (
	KindNote             Kind = "note"              // записка Директората
	KindLevelUp          Kind = "level_up"          // повышение уровня по XP
	KindAchievement      Kind = "achievement"       // новая грамота
	KindSuggestion       Kind = "suggestion"        // решение по предложению
	KindRemarkReply      Kind = "remark_reply"      // ответ на вашу пометку на полях
	KindPetition         Kind = "petition"          // решение по ходатайству о допуске
	KindInvitation       Kind = "invitation"        // приглашение Совета на следующий уровень
	KindInvitationAnswer Kind = "invitation_answer" // ответ читателя на приглашение (приглашавшему)
	KindSanction         Kind = "sanction"          // предупреждение или блокировка комментариев
)

const (
	MaxTitle = 200
	MaxBody  = 4000
	PerPage  = 20
)

type row struct {
	ID        int64 `gorm:"primaryKey"`
	UserID    int64
	Kind      string
	Params    []byte `gorm:"type:jsonb"`
	CreatedAt time.Time
	ReadAt    *time.Time
}

func (row) TableName() string { return "inbox_messages" }

// Send кладёт записку в ящик. params — то, из чего собирается текст (см. render).
func Send(ctx context.Context, tx *gorm.DB, userID int64, kind Kind, params map[string]any, now time.Time) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if params == nil {
		raw = []byte("{}")
	}
	return tx.WithContext(ctx).Exec(
		`INSERT INTO inbox_messages (user_id, kind, params, created_at) VALUES (?, ?, ?::jsonb, ?)`,
		userID, string(kind), string(raw), now).Error
}

// Note — записка Директората: заголовок и текст проверяются.
type Note struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Validate возвращает путь поля и сообщение (по-русски, ключ перевода) или пусто.
func (n *Note) Validate() (field, msg string) {
	n.Title, n.Body = strings.TrimSpace(n.Title), strings.TrimSpace(n.Body)
	switch {
	case n.Title == "" || utf8.RuneCountInString(n.Title) > MaxTitle:
		return "title", "Заголовок: от 1 до 200 знаков"
	case n.Body == "" || utf8.RuneCountInString(n.Body) > MaxBody:
		return "body", "Текст: от 1 до 4000 знаков"
	}
	return "", ""
}

// SendNote отправляет записку одному пользователю (login) или всем (login == ""); возвращает число получателей.
func SendNote(ctx context.Context, tx *gorm.DB, login string, n Note, now time.Time) (int64, error) {
	raw, err := json.Marshal(map[string]any{"title": n.Title, "body": n.Body})
	if err != nil {
		return 0, err
	}
	q := `INSERT INTO inbox_messages (user_id, kind, params, created_at)
	      SELECT id, 'note', ?::jsonb, ? FROM users`
	args := []any{string(raw), now}
	if login != "" {
		q += ` WHERE login = ?`
		args = append(args, login)
	}
	res := tx.WithContext(ctx).Exec(q, args...)
	return res.RowsAffected, res.Error
}

// Item — записка в ответе: текст уже на языке читателя.
type Item struct {
	ID        int64     `json:"id"`
	Kind      Kind      `json:"kind"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Link      string    `json:"link,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Read      bool      `json:"read"`
}

// Page — страница ящика.
type Page struct {
	Items  []Item `json:"items"`
	Total  int64  `json:"total"`
	Unread int64  `json:"unread"`
	Page   int    `json:"page"`
	Pages  int    `json:"pages"`
}

// List — ящик пользователя, сначала новые.
func List(ctx context.Context, db *gorm.DB, userID int64, page int, lang i18n.Lang) (*Page, error) {
	if page < 1 {
		page = 1
	}
	out := &Page{Items: []Item{}, Page: page}
	base := db.WithContext(ctx).Model(&row{}).Where("user_id = ?", userID)
	if err := base.Count(&out.Total).Error; err != nil {
		return nil, err
	}
	if err := db.WithContext(ctx).Model(&row{}).Where("user_id = ? AND read_at IS NULL", userID).Count(&out.Unread).Error; err != nil {
		return nil, err
	}
	out.Pages = int((out.Total + PerPage - 1) / PerPage)
	var rows []row
	err := db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC, id DESC").
		Limit(PerPage).Offset((page - 1) * PerPage).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out.Items = append(out.Items, render(r, lang))
	}
	return out, nil
}

// Unread — сколько непрочитанных.
func Unread(ctx context.Context, db *gorm.DB, userID int64) (int64, error) {
	var n int64
	err := db.WithContext(ctx).Model(&row{}).Where("user_id = ? AND read_at IS NULL", userID).Count(&n).Error
	return n, err
}

// MarkRead помечает записку прочитанной; false — такой записки у пользователя нет.
func MarkRead(ctx context.Context, db *gorm.DB, userID, id int64, now time.Time) (bool, error) {
	var exists int64
	if err := db.WithContext(ctx).Model(&row{}).Where("id = ? AND user_id = ?", id, userID).Count(&exists).Error; err != nil {
		return false, err
	}
	if exists == 0 {
		return false, nil
	}
	return true, db.WithContext(ctx).Model(&row{}).Where("id = ? AND user_id = ? AND read_at IS NULL", id, userID).Update("read_at", now).Error
}

// MarkAllRead помечает прочитанными все записки пользователя.
func MarkAllRead(ctx context.Context, db *gorm.DB, userID int64, now time.Time) error {
	return db.WithContext(ctx).Model(&row{}).Where("user_id = ? AND read_at IS NULL", userID).Update("read_at", now).Error
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func num(m map[string]any, k string) int {
	f, _ := m[k].(float64)
	return int(f)
}

// render собирает текст записки на языке l.
func render(r row, l i18n.Lang) Item {
	var p map[string]any
	_ = json.Unmarshal(r.Params, &p)
	it := Item{ID: r.ID, Kind: Kind(r.Kind), CreatedAt: r.CreatedAt.UTC(), Read: r.ReadAt != nil}
	switch it.Kind {
	case KindNote:
		it.Title, it.Body = str(p, "title"), str(p, "body")
	case KindLevelUp:
		it.Title = l.T("Допуск повышен")
		it.Body = l.T("Ваш допуск повышен до уровня %d. Поздравляем!", num(p, "level"))
		it.Link = "/file"
	case KindAchievement:
		it.Title = l.T("Новая грамота")
		it.Body = l.T("Вам выдана грамота: «%s»", l.Translate(str(p, "name")))
		it.Link = "/file"
	case KindSuggestion:
		it.Title = l.T("Решение по вашему предложению")
		body := l.T("Ваше предложение: %s.", l.Translate(str(p, "status")))
		if c := str(p, "comment"); c != "" {
			body += " " + c
		}
		it.Body = body
		it.Link = "/suggestions"
	case KindRemarkReply:
		it.Title = l.T("Ответ на вашу пометку")
		it.Body = l.T("На вашу пометку на полях ответили в деле %s.", str(p, "code"))
		it.Link = "/doc/" + str(p, "slug")
	case KindPetition:
		it.Title = l.T("Решение по вашему ходатайству")
		if str(p, "status") == "approved" {
			it.Body = l.T("Ходатайство одобрено: ваш допуск повышен до уровня %d.", num(p, "level"))
		} else {
			it.Body = l.T("Ходатайство на уровень %d отклонено.", num(p, "level"))
		}
		if c := str(p, "comment"); c != "" {
			it.Body += " " + c
		}
		it.Link = "/file"
	case KindInvitation:
		it.Title = l.T("Приглашение Совета")
		it.Body = l.T("Особый Совет приглашает вас на уровень %d. Примите приглашение в личном деле.", num(p, "level"))
		if m := str(p, "message"); m != "" {
			it.Body += " " + m
		}
		it.Link = "/file"
	case KindInvitationAnswer:
		it.Title = l.T("Ответ на приглашение")
		if str(p, "status") == "accepted" {
			it.Body = l.T("Приглашённый вами читатель принял приглашение на уровень %d.", num(p, "level"))
		} else {
			it.Body = l.T("Приглашённый вами читатель отклонил приглашение на уровень %d.", num(p, "level"))
		}
	case KindSanction:
		if str(p, "kind") == "warning" {
			it.Title = l.T("Предупреждение")
			it.Body = l.T("Модерация вынесла вам предупреждение. Причина: %s", str(p, "reason"))
		} else {
			it.Title = l.T("Блокировка комментариев")
			it.Body = l.T("Вам запрещено писать пометки на полях до %s. Причина: %s", str(p, "until"), str(p, "reason"))
		}
	default:
		it.Title = string(it.Kind)
	}
	return it
}

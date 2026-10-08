package inbox

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// EmailPrefs — о чём читатель хочет получать письма (колонка users.email_prefs). Письмо — копия записки
// из внутренней почты, поэтому настройки те же виды, что и у записок: сначала общий тумблер, потом каждый вид.
type EmailPrefs struct {
	Enabled          bool `json:"enabled"`
	Note             bool `json:"note"`
	LevelUp          bool `json:"level_up"`
	Achievement      bool `json:"achievement"`
	Suggestion       bool `json:"suggestion"`
	RemarkReply      bool `json:"remark_reply"`
	Petition         bool `json:"petition"`
	Invitation       bool `json:"invitation"`
	InvitationAnswer bool `json:"invitation_answer"`
	Sanction         bool `json:"sanction"`
}

// DefaultEmailPrefs — письма включены по умолчанию: читатель их выключает, а не включает,
// иначе после включения почты Директоратом никто о событиях не узнал бы.
func DefaultEmailPrefs() EmailPrefs {
	return EmailPrefs{
		Enabled: true, Note: true, LevelUp: true, Achievement: true, Suggestion: true,
		RemarkReply: true, Petition: true, Invitation: true, InvitationAnswer: true, Sanction: true,
	}
}

// Wants — просил ли читатель письмо о таком роде записок. Неизвестный вид считается включённым,
// чтобы новая записка не потерялась из-за старых настроек.
func (p EmailPrefs) Wants(kind Kind) bool {
	if !p.Enabled {
		return false
	}
	switch kind {
	case KindNote:
		return p.Note
	case KindLevelUp:
		return p.LevelUp
	case KindAchievement:
		return p.Achievement
	case KindSuggestion:
		return p.Suggestion
	case KindRemarkReply:
		return p.RemarkReply
	case KindPetition:
		return p.Petition
	case KindInvitation:
		return p.Invitation
	case KindInvitationAnswer:
		return p.InvitationAnswer
	case KindSanction:
		return p.Sanction
	default:
		return true
	}
}

// ParseEmailPrefs — настройки из базы. Распарсиваются поверх значений по умолчанию, поэтому в JSON
// могут отсутствовать виды записок (например, строка, созданная до появления новых видов).
func ParseEmailPrefs(raw []byte) EmailPrefs {
	p := DefaultEmailPrefs()
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	return p
}

// EmailPrefsGet — настройки писем читателя.
func EmailPrefsGet(ctx context.Context, db *gorm.DB, userID int64) (EmailPrefs, error) {
	// в строку, а не в []byte: Scan читает срез как список строк и ломается на одном значении jsonb
	var raw string
	err := db.WithContext(ctx).Raw(`SELECT email_prefs FROM users WHERE id = ?`, userID).Scan(&raw).Error
	if err != nil {
		return EmailPrefs{}, err
	}
	return ParseEmailPrefs([]byte(raw)), nil
}

// EmailPrefsSet — сохранить настройки писем читателя.
func EmailPrefsSet(ctx context.Context, db *gorm.DB, userID int64, p EmailPrefs) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Exec(`UPDATE users SET email_prefs = ?::jsonb WHERE id = ?`, string(raw), userID).Error
}

// enqueueEmail кладёт копию записки в очередь писем — в той же транзакции, что и сама записка: письмо
// уходит уже после фиксации (см. Outbox), поэтому откат не оставляет писем о несделанном, а перезапуск
// не теряет начатое. Письмо о повышении уровня здесь не очередь — его шлёт accounts своим шаблоном
// (см. Service.notifyLevelUp).
func enqueueEmail(ctx context.Context, tx *gorm.DB, userID int64, kind Kind, params map[string]any, now time.Time) error {
	if kind == KindLevelUp {
		return nil
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if params == nil {
		raw = []byte("{}")
	}
	return tx.WithContext(ctx).Exec(
		`INSERT INTO email_outbox (user_id, kind, params, created_at) VALUES (?, ?, ?::jsonb, ?)`,
		userID, string(kind), string(raw), now).Error
}

// enqueueNote — то же для записки Директората: письма уходят всем, кому ушла записка (оформление
// и проверка настроек — уже при отправке, см. Outbox).
func enqueueNote(ctx context.Context, tx *gorm.DB, login string, raw []byte, now time.Time) (int64, error) {
	q := `INSERT INTO email_outbox (user_id, kind, params, created_at)
	      SELECT id, 'note', ?::jsonb, ? FROM users`
	args := []any{string(raw), now}
	if login != "" {
		q += ` WHERE login = ?`
		args = append(args, login)
	}
	res := tx.WithContext(ctx).Exec(q, args...)
	return res.RowsAffected, res.Error
}

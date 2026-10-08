// Package achievements — грамоты (шаг 5.6): фиксированный набор, выдаётся автоматически в момент
// события (вход, чтение дела, погашение скрытого кода) и больше не снимается. Независимый пакет
// (как internal/xp): работает с таблицей achievements напрямую, не импортирует accounts/documents,
// чтобы не создавать циклов — оба импортируют achievements, а не наоборот.
package achievements

import (
	"context"
	"time"

	"gorm.io/gorm"

	"kupol/internal/i18n"
	"kupol/internal/inbox"
)

// Kind — код грамоты, пишется в achievements.kind.
type Kind string

const (
	Read10       Kind = "read_10"
	Read50       Kind = "read_50"
	Read100      Kind = "read_100"
	Streak7      Kind = "streak_7"
	Streak30     Kind = "streak_30"
	SecretFinder Kind = "secret_finder"
)

// All — все грамоты в стабильном порядке.
var All = []Kind{Read10, Read50, Read100, Streak7, Streak30, SecretFinder}

var names = map[Kind]string{
	Read10:       "Прочитано 10 дел",
	Read50:       "Прочитано 50 дел",
	Read100:      "Прочитано 100 дел",
	Streak7:      "Серия входов: 7 дней",
	Streak30:     "Серия входов: 30 дней",
	SecretFinder: "Нашёл скрытый код",
}

// NameIn — название грамоты на языке l.
func (k Kind) NameIn(l i18n.Lang) string { return l.Translate(names[k]) }

// Item — грамота в ответе (для личного дела).
type Item struct {
	Kind      Kind      `json:"kind"`
	Name      string    `json:"name"`
	AwardedAt time.Time `json:"awarded_at"`
}

type row struct {
	UserID    int64
	Kind      string
	AwardedAt time.Time
}

func (row) TableName() string { return "achievements" }

// grant выдаёт грамоту, если её ещё нет. Возвращает true, если выдана только что.
func grant(ctx context.Context, tx *gorm.DB, userID int64, kind Kind, now time.Time) (bool, error) {
	res := tx.WithContext(ctx).Exec(`
		INSERT INTO achievements (user_id, kind, awarded_at) VALUES (?, ?, ?)
		ON CONFLICT (user_id, kind) DO NOTHING`, userID, string(kind), now)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	// записка во внутреннюю почту (шаг 5.7): название — ключ перевода, читателю оно соберётся на его языке
	err := inbox.Send(ctx, tx, userID, inbox.KindAchievement, map[string]any{"name": names[kind]}, now)
	return err == nil, err
}

// readThresholds — сколько разных дел нужно прочитать для каждой грамоты (по возрастанию).
var readThresholds = []struct {
	n    int64
	kind Kind
}{
	{10, Read10}, {50, Read50}, {100, Read100},
}

// CheckReads — сколько дел прочитано (document_reads: PRIMARY KEY (user_id, document_id), поэтому
// count(*) — уже число разных дел), выдаёт read_10/50/100, если порог достигнут.
func CheckReads(ctx context.Context, tx *gorm.DB, userID int64, now time.Time) ([]Kind, error) {
	var count int64
	if err := tx.WithContext(ctx).Raw(`SELECT count(*) FROM document_reads WHERE user_id = ?`, userID).Scan(&count).Error; err != nil {
		return nil, err
	}
	var out []Kind
	for _, th := range readThresholds {
		if count < th.n {
			break
		}
		ok, err := grant(ctx, tx, userID, th.kind, now)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, th.kind)
		}
	}
	return out, nil
}

var streakThresholds = []struct {
	n    int
	kind Kind
}{
	{7, Streak7}, {30, Streak30},
}

// CheckStreak — серия входов подряд (уже посчитана xp.AwardLogin), выдаёт streak_7/30.
func CheckStreak(ctx context.Context, tx *gorm.DB, userID int64, streak int, now time.Time) ([]Kind, error) {
	var out []Kind
	for _, th := range streakThresholds {
		if streak < th.n {
			break
		}
		ok, err := grant(ctx, tx, userID, th.kind, now)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, th.kind)
		}
	}
	return out, nil
}

// Grant — прямая выдача грамоты (secret_finder). kind в возвращённом списке, только если выдана впервые.
func Grant(ctx context.Context, tx *gorm.DB, userID int64, kind Kind, now time.Time) ([]Kind, error) {
	ok, err := grant(ctx, tx, userID, kind, now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return []Kind{kind}, nil
}

// List — грамоты пользователя, сначала новые (для личного дела).
func List(ctx context.Context, db *gorm.DB, userID int64, lang i18n.Lang) ([]Item, error) {
	var rows []row
	err := db.WithContext(ctx).Where("user_id = ?", userID).Order("awarded_at DESC, kind").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]Item, len(rows))
	for i, r := range rows {
		k := Kind(r.Kind)
		out[i] = Item{Kind: k, Name: k.NameIn(lang), AwardedAt: r.AwardedAt.UTC()}
	}
	return out, nil
}

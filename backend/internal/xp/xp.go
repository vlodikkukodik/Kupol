// Package xp — очки опыта, серия ежедневных входов и автоматическое повышение уровня 1→2→3 (спецификация §5).
//
// Пороги и суммы начислений — константы (правка в админке отложена до этапа 7, см. docs/architecture.md §9).
// Источники, кроме входа (оценки, комментарии, предложения), появятся в шагах 5.2–5.4 и будут использовать
// ту же таблицу xp_events для дневных лимитов.
package xp

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Source — источник начисления, пишется в xp_events.source.
type Source string

const (
	SourceLogin   Source = "login"
	SourceComment Source = "comment" // пометка на полях (шаг 5.2)
)

// Значения по умолчанию (спецификация §5).
const (
	LoginXP        = 10 // база за вход раз в календарный день (UTC)
	StreakBonusDay = 2  // +2 XP за каждый день серии сверх первого
	StreakBonusMax = 20 // бонус серии не растёт дальше

	Level2Threshold = 100 // Стажёр (~неделя активности)
	Level3Threshold = 500 // Сотрудник (~месяц активности)

	CommentXP       = 15 // за пометку на полях
	CommentDailyCap = 3  // не больше стольких пометок в день приносят XP; сами пометки сверх лимита публикуются как обычно
)

type userRow struct {
	ID          int64
	Level       int
	XP          int        `gorm:"column:xp"`
	LoginStreak int        `gorm:"column:login_streak"`
	LastXPDay   *time.Time `gorm:"column:last_xp_day"`
}

func (userRow) TableName() string { return "users" }

type eventRow struct {
	UserID    int64
	Source    string
	Amount    int
	CreatedAt time.Time
}

func (eventRow) TableName() string { return "xp_events" }

// LoginResult — итог начисления XP за вход.
type LoginResult struct {
	// Awarded — сколько начислено сейчас; 0, если за сегодня уже начисляли (повторный вход в тот же день).
	Awarded  int
	Streak   int
	XP       int
	Level    int
	Promoted bool // уровень только что вырос (1→2 или 2→3)
}

func dayKey(t time.Time) string { return t.UTC().Format("2006-01-02") }

// AwardLogin начисляет XP за вход и продлевает серию; не чаще раза в календарный день (UTC).
// Серия продолжается только при входе на следующий день подряд, иначе начинается заново.
// Повышает уровень 1→2→3 сам, если новая сумма XP перешла порог. Вызывать внутри транзакции входа.
func AwardLogin(ctx context.Context, tx *gorm.DB, userID int64, now time.Time) (LoginResult, error) {
	var row userRow
	// SELECT … FOR UPDATE: два одновременных входа одного пользователя не должны начислить дважды за день.
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).Take(&row).Error; err != nil {
		return LoginResult{}, err
	}
	res := LoginResult{XP: row.XP, Level: row.Level, Streak: row.LoginStreak}
	today := dayKey(now)
	if row.LastXPDay != nil && dayKey(*row.LastXPDay) == today {
		return res, nil
	}

	streak := 1
	if row.LastXPDay != nil {
		yesterday := dayKey(now.AddDate(0, 0, -1))
		if dayKey(*row.LastXPDay) == yesterday {
			streak = row.LoginStreak + 1
		}
	}
	bonus := min((streak-1)*StreakBonusDay, StreakBonusMax)
	amount := LoginXP + bonus
	newXP := row.XP + amount
	newLevel := promote(row.Level, newXP)

	lastDay := now.UTC().Truncate(24 * time.Hour)
	updates := map[string]any{"xp": newXP, "login_streak": streak, "last_xp_day": lastDay}
	if newLevel != row.Level {
		updates["level"] = newLevel
	}
	if err := tx.WithContext(ctx).Model(&userRow{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		return LoginResult{}, err
	}
	if err := tx.WithContext(ctx).Create(&eventRow{UserID: userID, Source: string(SourceLogin), Amount: amount, CreatedAt: now}).Error; err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Awarded: amount, Streak: streak, XP: newXP, Level: newLevel, Promoted: newLevel != row.Level}, nil
}

// Result — итог начисления XP по дневному лимиту количества событий (Award).
type Result struct {
	// Awarded — сколько начислено сейчас; 0, если дневной лимит источника уже исчерпан (событие всё равно случилось,
	// просто без XP — комментарий сверх лимита публикуется как обычно, его не отклоняют).
	Awarded  int
	XP       int
	Level    int
	Promoted bool
}

// Award начисляет amount XP за источник source, но не больше maxPerDay раз за календарный день (UTC) —
// «оценка +5 (до 10/день)», «комментарий +15 (до 3/день)» и т.п. (спецификация §5). В отличие от AwardLogin
// лимит — по числу событий, а не «раз в день»: 3-е начисление в лимите 3/день ещё проходит, 4-е — уже нет.
// Вызывать внутри той же транзакции, что создаёт само событие (комментарий, оценку…).
func Award(ctx context.Context, tx *gorm.DB, userID int64, source Source, amount, maxPerDay int, now time.Time) (Result, error) {
	var row userRow
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).Take(&row).Error; err != nil {
		return Result{}, err
	}
	res := Result{XP: row.XP, Level: row.Level}

	dayStart := now.UTC().Truncate(24 * time.Hour)
	var todayCount int64
	if err := tx.WithContext(ctx).Model(&eventRow{}).
		Where("user_id = ? AND source = ? AND created_at >= ?", userID, string(source), dayStart).
		Count(&todayCount).Error; err != nil {
		return Result{}, err
	}

	awarded := 0
	if todayCount < int64(maxPerDay) {
		awarded = amount
	}
	if awarded > 0 {
		newXP := row.XP + awarded
		newLevel := promote(row.Level, newXP)
		updates := map[string]any{"xp": newXP}
		if newLevel != row.Level {
			updates["level"] = newLevel
		}
		if err := tx.WithContext(ctx).Model(&userRow{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
			return Result{}, err
		}
		res.XP, res.Level, res.Promoted = newXP, newLevel, newLevel != row.Level
	}
	if err := tx.WithContext(ctx).Create(&eventRow{UserID: userID, Source: string(source), Amount: awarded, CreatedAt: now}).Error; err != nil {
		return Result{}, err
	}
	res.Awarded = awarded
	return res, nil
}

// promote поднимает уровень 1→2 и 2→3 по порогам; на более высокие уровни (выдаёт Особый Совет) не влияет.
func promote(level, xpTotal int) int {
	if level == 1 && xpTotal >= Level2Threshold {
		level = 2
	}
	if level == 2 && xpTotal >= Level3Threshold {
		level = 3
	}
	return level
}

// NextLevelThreshold — сколько XP нужно для следующего уровня; 0, если автоматических повышений больше нет
// (уровень 3 и выше — выдаёт Особый Совет).
func NextLevelThreshold(level int) int {
	switch level {
	case 1:
		return Level2Threshold
	case 2:
		return Level3Threshold
	default:
		return 0
	}
}

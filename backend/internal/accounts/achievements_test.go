package accounts

import (
	"testing"
	"time"

	"kupol/internal/achievements"
)

// TestLoginGrantsStreakAchievements — семь и тридцать дней подряд выдают грамоты streak_7/streak_30
// один раз, ровно в момент достижения порога.
func TestLoginGrantsStreakAchievements(t *testing.T) {
	e := newEnv(t)
	reg := e.register("marathon", "верный пароль")
	if len(reg.NewAchievements) != 0 {
		t.Fatalf("день 1: неожиданные грамоты %v", reg.NewAchievements)
	}

	for day := 2; day <= 6; day++ {
		e.clock.Advance(24 * time.Hour)
		res, err := e.svc.Login(ctx, "marathon", "верный пароль", e.freshIP())
		if err != nil {
			t.Fatalf("день %d: %v", day, err)
		}
		if len(res.NewAchievements) != 0 {
			t.Fatalf("день %d: неожиданные грамоты %v", day, res.NewAchievements)
		}
	}

	e.clock.Advance(24 * time.Hour) // день 7
	res, err := e.svc.Login(ctx, "marathon", "верный пароль", e.freshIP())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NewAchievements) != 1 || res.NewAchievements[0] != achievements.Streak7 {
		t.Fatalf("день 7: ожидался streak_7, получено %v", res.NewAchievements)
	}

	// повторный вход на седьмой день не переотправляет грамоту
	if res, err := e.svc.Login(ctx, "marathon", "верный пароль", e.freshIP()); err != nil || len(res.NewAchievements) != 0 {
		t.Fatalf("повтор дня 7: %v %v", res, err)
	}

	for day := 8; day <= 29; day++ {
		e.clock.Advance(24 * time.Hour)
		if res, err := e.svc.Login(ctx, "marathon", "верный пароль", e.freshIP()); err != nil || len(res.NewAchievements) != 0 {
			t.Fatalf("день %d: %v %v", day, res, err)
		}
	}

	e.clock.Advance(24 * time.Hour) // день 30
	res, err = e.svc.Login(ctx, "marathon", "верный пароль", e.freshIP())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NewAchievements) != 1 || res.NewAchievements[0] != achievements.Streak30 {
		t.Fatalf("день 30: ожидался streak_30, получено %v", res.NewAchievements)
	}

	items, err := e.svc.Achievements(ctx, res.User.ID, "ru")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("список грамот: %+v", items)
	}
}

package xp_test

import (
	"context"
	"testing"
	"time"

	"kupol/internal/testutil"
	"kupol/internal/xp"
)

func TestAwardLogin(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	ctx := context.Background()

	var userID int64
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := db.Exec(
		`INSERT INTO users (login, password_hash, backup_code_hash, level, created_at, password_changed_at) VALUES (?,?,?,?,?,?)`,
		"tester", "x", "y", 1, now, now,
	).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := db.Raw(`SELECT id FROM users WHERE login = 'tester'`).Scan(&userID).Error; err != nil {
		t.Fatalf("find user: %v", err)
	}

	// первый вход: базовые 10 XP, серия 1
	res, err := xp.AwardLogin(ctx, db, userID, now)
	if err != nil {
		t.Fatalf("award 1: %v", err)
	}
	if res.Awarded != xp.LoginXP || res.Streak != 1 || res.XP != xp.LoginXP || res.Level != 1 || res.Promoted {
		t.Fatalf("день 1: неожиданный результат %+v", res)
	}

	// повторный вход в тот же день не начисляет второй раз
	res, err = xp.AwardLogin(ctx, db, userID, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("award повтор: %v", err)
	}
	if res.Awarded != 0 || res.Streak != 1 || res.XP != xp.LoginXP {
		t.Fatalf("повтор в тот же день начислил XP: %+v", res)
	}

	// вход на следующий день продлевает серию и даёт бонус
	day2 := now.AddDate(0, 0, 1)
	res, err = xp.AwardLogin(ctx, db, userID, day2)
	if err != nil {
		t.Fatalf("award день 2: %v", err)
	}
	wantDay2 := xp.LoginXP + xp.StreakBonusDay
	if res.Awarded != wantDay2 || res.Streak != 2 {
		t.Fatalf("день 2: awarded=%d streak=%d, хотели %d/2", res.Awarded, res.Streak, wantDay2)
	}

	// пропуск дня сбрасывает серию к 1
	day4 := now.AddDate(0, 0, 3)
	res, err = xp.AwardLogin(ctx, db, userID, day4)
	if err != nil {
		t.Fatalf("award день 4 (пропуск): %v", err)
	}
	if res.Streak != 1 || res.Awarded != xp.LoginXP {
		t.Fatalf("пропуск дня не сбросил серию: %+v", res)
	}
}

func TestAwardLoginPromotesLevel(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	var userID int64
	if err := db.Exec(
		`INSERT INTO users (login, password_hash, backup_code_hash, level, xp, created_at, password_changed_at) VALUES (?,?,?,?,?,?,?)`,
		"promo", "x", "y", 1, xp.Level2Threshold-xp.LoginXP, now, now,
	).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := db.Raw(`SELECT id FROM users WHERE login = 'promo'`).Scan(&userID).Error; err != nil {
		t.Fatalf("find user: %v", err)
	}

	res, err := xp.AwardLogin(ctx, db, userID, now)
	if err != nil {
		t.Fatalf("award: %v", err)
	}
	if !res.Promoted || res.Level != 2 {
		t.Fatalf("ожидалось повышение до 2: %+v", res)
	}

	var level int
	if err := db.Raw(`SELECT level FROM users WHERE id = ?`, userID).Scan(&level).Error; err != nil {
		t.Fatalf("read level: %v", err)
	}
	if level != 2 {
		t.Fatalf("уровень в БД не обновился: %d", level)
	}
}

func TestAwardCapsByCountPerDay(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	var userID int64
	if err := db.Exec(
		`INSERT INTO users (login, password_hash, backup_code_hash, level, created_at, password_changed_at) VALUES (?,?,?,?,?,?)`,
		"commenter", "x", "y", 1, now, now,
	).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := db.Raw(`SELECT id FROM users WHERE login = 'commenter'`).Scan(&userID).Error; err != nil {
		t.Fatalf("find user: %v", err)
	}

	var xpTotal int
	for i := 0; i < xp.CommentDailyCap; i++ {
		res, err := xp.Award(ctx, db, userID, xp.SourceComment, xp.CommentXP, xp.CommentDailyCap, now.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("award %d: %v", i, err)
		}
		if res.Awarded != xp.CommentXP {
			t.Fatalf("событие %d: awarded=%d, хотели %d", i, res.Awarded, xp.CommentXP)
		}
		xpTotal += xp.CommentXP
		if res.XP != xpTotal {
			t.Fatalf("событие %d: XP=%d, хотели %d", i, res.XP, xpTotal)
		}
	}

	// событие сверх лимита случается (запись в xp_events есть), но XP не приносит
	res, err := xp.Award(ctx, db, userID, xp.SourceComment, xp.CommentXP, xp.CommentDailyCap, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("award сверх лимита: %v", err)
	}
	if res.Awarded != 0 || res.XP != xpTotal {
		t.Fatalf("сверх лимита начислило XP: %+v", res)
	}

	var events int64
	if err := db.Raw("SELECT count(*) FROM xp_events WHERE user_id = ? AND source = ?", userID, string(xp.SourceComment)).Scan(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != int64(xp.CommentDailyCap+1) {
		t.Fatalf("событий в xp_events: %d, хотели %d", events, xp.CommentDailyCap+1)
	}

	// на следующий день лимит считается заново
	res, err = xp.Award(ctx, db, userID, xp.SourceComment, xp.CommentXP, xp.CommentDailyCap, now.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("award день 2: %v", err)
	}
	if res.Awarded != xp.CommentXP {
		t.Fatalf("день 2: лимит не сбросился: %+v", res)
	}
}

func TestAwardPromotesLevel(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	var userID int64
	if err := db.Exec(
		`INSERT INTO users (login, password_hash, backup_code_hash, level, xp, created_at, password_changed_at) VALUES (?,?,?,?,?,?,?)`,
		"commenter2", "x", "y", 1, xp.Level2Threshold-xp.CommentXP, now, now,
	).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := db.Raw(`SELECT id FROM users WHERE login = 'commenter2'`).Scan(&userID).Error; err != nil {
		t.Fatalf("find user: %v", err)
	}

	res, err := xp.Award(ctx, db, userID, xp.SourceComment, xp.CommentXP, xp.CommentDailyCap, now)
	if err != nil {
		t.Fatalf("award: %v", err)
	}
	if !res.Promoted || res.Level != 2 {
		t.Fatalf("ожидалось повышение до 2: %+v", res)
	}
}

func TestNextLevelThreshold(t *testing.T) {
	if got := xp.NextLevelThreshold(1); got != xp.Level2Threshold {
		t.Fatalf("уровень 1: %d, хотели %d", got, xp.Level2Threshold)
	}
	if got := xp.NextLevelThreshold(2); got != xp.Level3Threshold {
		t.Fatalf("уровень 2: %d, хотели %d", got, xp.Level3Threshold)
	}
	if got := xp.NextLevelThreshold(3); got != 0 {
		t.Fatalf("уровень 3: %d, хотели 0", got)
	}
}

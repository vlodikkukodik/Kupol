package achievements

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"kupol/internal/i18n"
	"kupol/internal/testutil"
)

var ctx = context.Background()

func newUser(t *testing.T, db *gorm.DB, login string) int64 {
	t.Helper()
	now := time.Now().UTC()
	var id int64
	err := db.Raw(`INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at)
		VALUES (?, 'h', 'b', ?, ?) RETURNING id`, login, now, now).Scan(&id).Error
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedReads(t *testing.T, db *gorm.DB, userID int64, n int) {
	t.Helper()
	now := time.Now().UTC()
	for i := 0; i < n; i++ {
		var docID int64
		err := db.Raw(`INSERT INTO documents (type, title, status, level, composed_year, revision, created_at, updated_at, translations)
			VALUES ('memo', 'x', 'draft', 0, 1979, 1, ?, ?, '{}') RETURNING id`, now, now).Scan(&docID).Error
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`INSERT INTO document_reads (user_id, document_id, first_read_at, last_read_at, read_count)
			VALUES (?, ?, ?, ?, 1)`, userID, docID, now, now).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestCheckReadsThresholds(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	uid := newUser(t, db, "reader")
	now := time.Now().UTC()

	seedReads(t, db, uid, 9)
	if got, err := CheckReads(ctx, db, uid, now); err != nil || len(got) != 0 {
		t.Fatalf("9 дел: %v %v", got, err)
	}

	seedReads(t, db, uid, 1) // 10-е
	got, err := CheckReads(ctx, db, uid, now)
	if err != nil || len(got) != 1 || got[0] != Read10 {
		t.Fatalf("10 дел: %v %v", got, err)
	}
	// повтор — уже не «новая»
	if got, err := CheckReads(ctx, db, uid, now); err != nil || len(got) != 0 {
		t.Fatalf("повтор на 10: %v %v", got, err)
	}

	seedReads(t, db, uid, 40) // 50-е
	got, err = CheckReads(ctx, db, uid, now)
	if err != nil || len(got) != 1 || got[0] != Read50 {
		t.Fatalf("50 дел: %v %v", got, err)
	}

	seedReads(t, db, uid, 50) // 100-е
	got, err = CheckReads(ctx, db, uid, now)
	if err != nil || len(got) != 1 || got[0] != Read100 {
		t.Fatalf("100 дел: %v %v", got, err)
	}

	items, err := List(ctx, db, uid, i18n.RU)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("список: %+v", items)
	}
}

func TestCheckStreakThresholds(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	uid := newUser(t, db, "streaker")
	now := time.Now().UTC()

	if got, err := CheckStreak(ctx, db, uid, 6, now); err != nil || len(got) != 0 {
		t.Fatalf("6 дней: %v %v", got, err)
	}
	got, err := CheckStreak(ctx, db, uid, 7, now)
	if err != nil || len(got) != 1 || got[0] != Streak7 {
		t.Fatalf("7 дней: %v %v", got, err)
	}
	// streak_7 уже выдана — при пороге 30 в «новых» только streak_30
	got, err = CheckStreak(ctx, db, uid, 30, now)
	if err != nil || len(got) != 1 || got[0] != Streak30 {
		t.Fatalf("30 дней: %v %v", got, err)
	}
}

func TestGrantIsIdempotent(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	uid := newUser(t, db, "finder")
	now := time.Now().UTC()

	got, err := Grant(ctx, db, uid, SecretFinder, now)
	if err != nil || len(got) != 1 || got[0] != SecretFinder {
		t.Fatalf("первая выдача: %v %v", got, err)
	}
	got, err = Grant(ctx, db, uid, SecretFinder, now)
	if err != nil || len(got) != 0 {
		t.Fatalf("повтор: %v %v", got, err)
	}

	items, err := List(ctx, db, uid, i18n.IT)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != SecretFinder || items[0].Name != "Ha trovato un codice segreto" {
		t.Fatalf("список на IT: %+v", items)
	}
}

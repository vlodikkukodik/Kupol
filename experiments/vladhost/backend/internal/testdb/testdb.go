// Package testdb даёт тестам чистую схему PostgreSQL с применёнными миграциями.
package testdb

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"gorm.io/gorm"

	"vladhost/internal/database"
)

// Open создаёт отдельную схему, мигрирует её и удаляет после теста.
// Тесты не пропускаются молча: без VLADHOST_TEST_DATABASE_URL они падают (make test-back поднимает БД).
func Open(t *testing.T) *gorm.DB {
	t.Helper()
	base := os.Getenv("VLADHOST_TEST_DATABASE_URL")
	if base == "" {
		t.Fatal("VLADHOST_TEST_DATABASE_URL не задан (make db-up, затем make test-back)")
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	schema := "t_" + hex.EncodeToString(b)

	admin, err := database.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	db, err := database.Open(u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

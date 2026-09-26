// Package testutil даёт интеграционным тестам настоящую PostgreSQL-БД:
// для каждого теста создаётся отдельная база, в неё применяются миграции,
// после теста база удаляется.
package testutil

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"gorm.io/gorm"

	"kupol/internal/database"
)

const envAdminURL = "KUPOL_TEST_DATABASE_URL"

// Logger — тихий логгер для тестов.
func Logger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func adminURL(t testing.TB) string {
	t.Helper()
	u := os.Getenv(envAdminURL)
	if u == "" {
		t.Fatalf("не задан %s: тесты работают с настоящим PostgreSQL. Запускайте через `make test-back` (поднимет БД) или задайте переменную вручную", envAdminURL)
	}
	return u
}

// NewDB создаёт пустую БД (без миграций) и возвращает подключение к ней.
func NewDB(t testing.TB) *gorm.DB {
	t.Helper()
	admin := adminURL(t)

	adminDB, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatalf("подключение к %s: %v", envAdminURL, err)
	}
	defer adminDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var b [6]byte
	_, _ = rand.Read(b[:])
	name := "kupol_t_" + hex.EncodeToString(b[:])
	create := fmt.Sprintf(`CREATE DATABASE %s ENCODING 'UTF8' LOCALE_PROVIDER icu ICU_LOCALE 'ru-RU' LOCALE 'C' TEMPLATE template0`, name)
	if _, err := adminDB.ExecContext(ctx, create); err != nil {
		t.Fatalf("create database: %v", err)
	}

	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name

	db, err := database.Open(ctx, u.String(), Logger())
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		cdb, err := sql.Open("pgx", admin)
		if err != nil {
			return
		}
		defer cdb.Close()
		cctx, ccancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer ccancel()
		if _, err := cdb.ExecContext(cctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name)); err != nil {
			t.Logf("drop database %s: %v", name, err)
		}
	})
	return db
}

// NewMigratedDB — NewDB + все миграции.
func NewMigratedDB(t testing.TB) *gorm.DB {
	t.Helper()
	db := NewDB(t)
	if err := database.MigrateUp(context.Background(), db, Logger()); err != nil {
		t.Fatalf("миграции: %v", err)
	}
	return db
}

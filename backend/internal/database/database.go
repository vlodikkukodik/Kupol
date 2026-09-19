// Package database открывает соединение с PostgreSQL через GORM и применяет миграции.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	connectTimeout = 30 * time.Second
	retryEvery     = time.Second
)

// Open подключается к БД и ждёт её готовности до connectTimeout
// (при старте systemd/compose Postgres может подниматься параллельно).
func Open(ctx context.Context, url string, log *slog.Logger) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(url), &gorm.Config{
		Logger:         newGormLogger(log),
		NowFunc:        func() time.Time { return time.Now().UTC() },
		TranslateError: true,
		// Ждём готовность сами (waitReady), а не падаем на первом же отказе.
		DisableAutomaticPing: true,
	})
	if err != nil {
		return nil, fmt.Errorf("gorm open: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	if err := waitReady(ctx, sqlDB, log); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func waitReady(ctx context.Context, sqlDB *sql.DB, log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	var last error
	for {
		pctx, pcancel := context.WithTimeout(ctx, 3*time.Second)
		last = sqlDB.PingContext(pctx)
		pcancel()
		if last == nil {
			return nil
		}
		log.Warn("БД пока недоступна", "err", last)
		select {
		case <-ctx.Done():
			return fmt.Errorf("БД недоступна за %s: %w", connectTimeout, errors.Join(last, ctx.Err()))
		case <-time.After(retryEvery):
		}
	}
}

// Ping проверяет живость БД (для /health).
func Ping(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

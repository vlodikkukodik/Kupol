// Package database открывает PostgreSQL через GORM и применяет миграции goose.
package database

import (
	"embed"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(url string) (*gorm.DB, error) {
	// «Запись не найдена» — обычный исход (неизвестный логин, чужой сайт), а не ошибка: в журнал её не пишем.
	lg := logger.New(log.New(os.Stderr, "\r\n", log.LstdFlags), logger.Config{
		SlowThreshold: 500 * time.Millisecond, LogLevel: logger.Warn, IgnoreRecordNotFoundError: true,
	})
	db, err := gorm.Open(postgres.Open(url), &gorm.Config{Logger: lg})
	if err != nil {
		return nil, fmt.Errorf("подключение к БД: %w", err)
	}
	return db, nil
}

func Migrate(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	if err := goose.Up(sqlDB, "migrations"); err != nil {
		return fmt.Errorf("миграции: %w", err)
	}
	return nil
}

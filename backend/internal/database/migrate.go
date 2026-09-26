package database

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func provider(db *gorm.DB) (*goose.Provider, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	// Advisory-lock, чтобы два процесса не мигрировали одновременно.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, sqlDB, sub, goose.WithSessionLocker(locker))
}

// MigrateUp применяет все ожидающие миграции.
func MigrateUp(ctx context.Context, db *gorm.DB, log *slog.Logger) error {
	p, err := provider(db)
	if err != nil {
		return err
	}
	results, err := p.Up(ctx)
	for _, r := range results {
		log.Info("миграция применена", "version", r.Source.Version, "file", r.Source.Path, "took", r.Duration)
	}
	if err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// MigrateDown откатывает одну последнюю миграцию.
func MigrateDown(ctx context.Context, db *gorm.DB, log *slog.Logger) error {
	p, err := provider(db)
	if err != nil {
		return err
	}
	r, err := p.Down(ctx)
	if err != nil {
		return fmt.Errorf("migrate down: %w", err)
	}
	log.Info("миграция откачена", "version", r.Source.Version, "file", r.Source.Path)
	return nil
}

// MigrateStatus печатает состояние миграций.
func MigrateStatus(ctx context.Context, db *gorm.DB, log *slog.Logger, out io.Writer) error {
	p, err := provider(db)
	if err != nil {
		return err
	}
	st, err := p.Status(ctx)
	if err != nil {
		return err
	}
	for _, s := range st {
		applied := "ожидает"
		if s.State == goose.StateApplied {
			applied = "применена " + s.AppliedAt.Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(out, "%04d  %-40s %s\n", s.Source.Version, s.Source.Path, applied)
	}
	return nil
}

// SchemaVersion возвращает номер последней применённой миграции.
func SchemaVersion(ctx context.Context, db *gorm.DB, log *slog.Logger) (int64, error) {
	p, err := provider(db)
	if err != nil {
		return 0, err
	}
	return p.GetDBVersion(ctx)
}

package database

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const slowQuery = 200 * time.Millisecond

// gormLogger направляет логи GORM в slog: ошибки и медленные запросы.
type gormLogger struct{ log *slog.Logger }

func newGormLogger(log *slog.Logger) gormlogger.Interface { return gormLogger{log: log} }

func (l gormLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }
func (l gormLogger) Info(ctx context.Context, msg string, a ...any) {
	l.log.InfoContext(ctx, msg, "args", a)
}
func (l gormLogger) Warn(ctx context.Context, msg string, a ...any) {
	l.log.WarnContext(ctx, msg, "args", a)
}
func (l gormLogger) Error(ctx context.Context, msg string, a ...any) {
	l.log.ErrorContext(ctx, msg, "args", a)
}

func (l gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, context.Canceled):
		sql, rows := fc()
		l.log.ErrorContext(ctx, "sql: ошибка", "err", err, "elapsed", elapsed, "rows", rows, "sql", sql)
	case elapsed > slowQuery:
		sql, rows := fc()
		l.log.WarnContext(ctx, "sql: медленный запрос", "elapsed", elapsed, "rows", rows, "sql", sql)
	}
}

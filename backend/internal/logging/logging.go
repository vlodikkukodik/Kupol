// Package logging настраивает структурные логи (slog).
package logging

import (
	"io"
	"log/slog"
)

// New: в проде JSON (journald/logrotate), в dev — читаемый текст.
func New(w io.Writer, prod bool, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	if prod {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

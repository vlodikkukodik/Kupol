// Команда kupol — Go API проекта КУПОЛ.
//
//	kupol serve            запустить API (по умолчанию), применяя миграции
//	kupol migrate up       применить миграции
//	kupol migrate down     откатить одну миграцию
//	kupol migrate status   показать состояние миграций
//	kupol user ...         управление пользователями (уровни, Директорат, сброс пароля); см. kupol user
//	kupol doc ...          документы: загрузка из JSON, выгрузка, список, статус, удаление; см. kupol doc
//	kupol audit ...        журнал событий (роли, замки, откаты, пароли); см. kupol audit
//	kupol version          версия сборки
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gorm.io/gorm"

	"kupol/internal/accounts"
	"kupol/internal/config"
	"kupol/internal/database"
	"kupol/internal/documents"
	"kupol/internal/httpapi"
	"kupol/internal/logging"
	"kupol/internal/passwords"
	"kupol/internal/ratelimit"
	"kupol/internal/version"
)

// maxConcurrentHashes — сколько хешей паролей считается одновременно (каждый ~19 МиБ памяти).
const maxConcurrentHashes = 4

// newAccounts собирает сервис аккаунтов и ограничитель частоты, общий с HTTP-слоем.
func newAccounts(cfg config.Config, db *gorm.DB, log *slog.Logger) (*accounts.Service, *ratelimit.Limiter, error) {
	hasher, err := passwords.NewHasher(passwords.DefaultParams, maxConcurrentHashes)
	if err != nil {
		return nil, nil, err
	}
	limiter := ratelimit.New(nil)
	svc, err := accounts.NewService(accounts.Options{DB: db, Hasher: hasher, Limiter: limiter, Limits: cfg.Limits, Log: log, SecretKey: cfg.ProxySecret})
	if err != nil {
		return nil, nil, err
	}
	return svc, limiter, nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "kupol:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}

	if cmd == "version" {
		fmt.Printf("kupol %s (%s)\n", version.Version, version.Commit)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("конфигурация:\n%w", err)
	}
	log := logging.New(os.Stderr, cfg.IsProd(), cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	switch cmd {
	case "migrate":
		sub := "up"
		if len(args) > 0 {
			sub = args[0]
		}
		switch sub {
		case "up":
			return database.MigrateUp(ctx, db, log)
		case "down":
			return database.MigrateDown(ctx, db, log)
		case "status":
			return database.MigrateStatus(ctx, db, log, os.Stdout)
		default:
			return fmt.Errorf("неизвестная подкоманда migrate %q (up|down|status)", sub)
		}
	case "user":
		svc, _, err := newAccounts(cfg, db, log)
		if err != nil {
			return err
		}
		return runUser(ctx, svc, args, os.Stdout)
	case "doc":
		return runDoc(ctx, documents.NewService(db, log, nil), args, os.Stdout)
	case "audit":
		return runAudit(ctx, db, args, os.Stdout)
	case "serve":
		return serve(ctx, cfg, db, log)
	default:
		return fmt.Errorf("неизвестная команда %q (serve|migrate|user|doc|audit|version)", cmd)
	}
}

func serve(ctx context.Context, cfg config.Config, db *gorm.DB, log *slog.Logger) error {
	if err := database.MigrateUp(ctx, db, log); err != nil {
		return err
	}

	svc, limiter, err := newAccounts(cfg, db, log)
	if err != nil {
		return err
	}
	// Фоновая уборка: просроченные ключи ограничителя, сессии и вопросы анкеты.
	go limiter.RunSweeper(ctx, time.Minute)
	go svc.RunCleanup(ctx, time.Hour)

	docs := documents.NewService(db, log, nil)
	// Индекс поиска строится при первом запуске после появления поиска и при смене правил его построения.
	if err := docs.EnsureSearchIndex(ctx); err != nil {
		return fmt.Errorf("индекс поиска: %w", err)
	}

	handler, err := httpapi.New(httpapi.Deps{
		Config: cfg, DB: db, Log: log, Accounts: svc, Limiter: limiter,
		Documents: docs,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// WriteTimeout не задаём: WebSocket и выдача PDF/файлов живут дольше;
		// таймауты записи будут ставиться на конкретных маршрутах.
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("API запущен", "addr", cfg.HTTPAddr, "env", cfg.Env, "version", version.Version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("получен сигнал остановки, завершаю запросы")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("API остановлен")
	return nil
}

// Команда vladhost: serve — запустить панель, migrate up — применить миграции,
// admin create — завести администратора (пароль из VLADHOST_ADMIN_PASSWORD).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"vladhost/internal/auth"
	"vladhost/internal/config"
	"vladhost/internal/database"
	"vladhost/internal/httpapi"
	"vladhost/internal/sites"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("использование: vladhost serve | migrate up | admin create --email E --username U")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := database.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	svc := auth.NewService(db, cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)

	switch {
	case args[0] == "migrate" && len(args) == 2 && args[1] == "up":
		return database.Migrate(db)
	case args[0] == "serve":
		if err := database.Migrate(db); err != nil {
			return err
		}
		fmt.Println("слушаю", cfg.Addr)
		siteSvc := sites.NewService(db, cfg.SitesRoot, cfg.BaseDomain, cfg.CertsDir,
			sites.Limits{MaxSites: cfg.MaxSites, DiskQuotaBytes: cfg.DiskQuotaBytes})
		go siteSvc.WatchCerts(context.Background(), 5*time.Second)
		return httpapi.New(svc, siteSvc, cfg).Run(cfg.Addr)
	case args[0] == "admin" && len(args) >= 2 && args[1] == "create":
		fs := flag.NewFlagSet("admin create", flag.ContinueOnError)
		email := fs.String("email", "", "email администратора")
		username := fs.String("username", "", "имя пользователя")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		pw := os.Getenv("VLADHOST_ADMIN_PASSWORD")
		if pw == "" {
			return fmt.Errorf("задайте пароль в VLADHOST_ADMIN_PASSWORD")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		u, err := svc.CreateAdmin(ctx, *email, *username, pw)
		if err != nil {
			return err
		}
		fmt.Printf("администратор %s (%s) создан\n", u.Username, u.Email)
		return nil
	}
	return fmt.Errorf("неизвестная команда %q", args[0])
}

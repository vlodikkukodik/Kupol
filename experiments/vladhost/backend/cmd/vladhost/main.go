// Команда vladhost: serve — запустить панель, migrate up — применить миграции,
// admin create — завести администратора (пароль из VLADHOST_ADMIN_PASSWORD).
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"vladhost/internal/auth"
	"vladhost/internal/config"
	"vladhost/internal/database"
	"vladhost/internal/ftpd"
	"vladhost/internal/httpapi"
	"vladhost/internal/sites"
	"vladhost/internal/webgw"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

// runWeb запускает веб-шлюз сайтов пользователей (отдельная служба, читает только каталог сайтов).
func runWeb(cfg config.WebConfig) error {
	gw := webgw.New(webgw.Options{Root: cfg.SitesRoot, BaseDomain: cfg.BaseDomain, DomainsDir: cfg.DomainsDir, LogDir: cfg.LogDir})
	go gw.MaintainLogs(context.Background(), time.Hour)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           gw,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    32 << 10,
	}
	fmt.Println("веб-шлюз слушает", cfg.Addr, "каталог сайтов", cfg.SitesRoot)
	return srv.ListenAndServe()
}

func startFTP(svc *sites.Service, cfg config.FTPConfig) error {
	ftp, err := ftpd.New(svc, cfg)
	if err != nil {
		return err
	}
	if err := ftp.Listen(); err != nil {
		return err
	}
	fmt.Println("FTPS слушает", ftp.Addr())
	go func() {
		if err := ftp.Serve(); err != nil {
			fmt.Fprintln(os.Stderr, "FTP остановился:", err)
		}
	}()
	return nil
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("использование: vladhost serve | web | migrate up | admin create --email E --username U")
	}
	if args[0] == "web" {
		return runWeb(config.LoadWeb()) // шлюзу не нужны ни БД, ни секреты панели
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
		siteSvc.ConfigureLogs(cfg.LogDir)
		go siteSvc.WatchCerts(context.Background(), 5*time.Second)
		if len(cfg.ServerIPs) > 0 {
			siteSvc.ConfigureDomains(sites.DomainConfig{ServerIPs: cfg.ServerIPs, MappingDir: cfg.DomainsDir})
			go siteSvc.WatchDomains(context.Background(), 30*time.Second)
		}
		if cfg.FTP.Addr != "" {
			// FTP необязателен для панели: если он не запустился (нет сертификата, порт занят), панель
			// продолжает работать, а в интерфейсе FTP показывается как недоступный.
			if err := startFTP(siteSvc, cfg.FTP); err != nil {
				fmt.Fprintln(os.Stderr, "ВНИМАНИЕ: FTP отключён:", err)
				cfg.FTP.Addr = ""
			}
		}
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

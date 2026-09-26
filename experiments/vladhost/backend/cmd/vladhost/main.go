// Команда vladhost: serve — запустить панель, migrate up — применить миграции,
// admin create — завести администратора (пароль из VLADHOST_ADMIN_PASSWORD),
// admin reset-2fa ЛОГИН — снять двухфакторный вход (пользователь потерял телефон и коды восстановления).
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"vladhost/internal/activity"
	"vladhost/internal/auth"
	"vladhost/internal/cms"
	"vladhost/internal/config"
	"vladhost/internal/cronjobs"
	"vladhost/internal/database"
	"vladhost/internal/dnszones"
	"vladhost/internal/ftpd"
	"vladhost/internal/httpapi"
	"vladhost/internal/mailer"
	"vladhost/internal/mailhost"
	"vladhost/internal/notify"
	"vladhost/internal/runtimes"
	"vladhost/internal/shellaccess"
	"vladhost/internal/shellbroker"
	"vladhost/internal/shellclient"
	"vladhost/internal/sites"
	"vladhost/internal/sshd"
	"vladhost/internal/tickets"
	"vladhost/internal/userdb"
	"vladhost/internal/webgw"

	"gorm.io/gorm"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

// runWeb запускает веб-шлюз сайтов пользователей (отдельная служба, читает только каталог сайтов).
func runWeb(cfg config.WebConfig) error {
	// Остановка по сигналу: дожидаемся, пока счётчики статистики сохранятся на диск.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	gw := webgw.New(webgw.Options{Root: cfg.SitesRoot, BaseDomain: cfg.BaseDomain, DomainsDir: cfg.DomainsDir, LogDir: cfg.LogDir, PHPSocketDir: cfg.PHPSocketDir})
	var bg sync.WaitGroup
	bg.Go(func() { gw.MaintainLogs(ctx, time.Hour) })
	bg.Go(func() { gw.RunStats(ctx.Done(), 30*time.Second) })
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           gw,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    32 << 10,
	}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(sh)
	}()
	fmt.Println("веб-шлюз слушает", cfg.Addr, "каталог сайтов", cfg.SitesRoot)
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	stop()
	bg.Wait()
	return err
}

// runShellBroker запускает посредника оболочек: служба от root, к сокету пускает только панель.
func runShellBroker(cfg config.BrokerConfig) error {
	u, err := user.Lookup(cfg.PanelUser)
	if err != nil {
		return fmt.Errorf("пользователь панели %q: %w", cfg.PanelUser, err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	l, err := shellbroker.Listen(cfg.Socket, gid)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()
	fmt.Println("посредник оболочек слушает", cfg.Socket, "для пользователя", cfg.PanelUser)
	err = shellbroker.New(shellbroker.Config{Dir: cfg.Dir, Sites: cfg.SitesRoot, PanelUID: uid, SystemdRun: cfg.SystemdRun, Systemctl: cfg.Systemctl}).Serve(l)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// startMailHost подключает почту на своих доменах. Без имени почтового сервера или папки исполнителя раздел выключен.
func startMailHost(db *gorm.DB, siteSvc *sites.Service, cfg config.Config, dnsSvc *dnszones.Service) *mailhost.Service {
	if cfg.MailHost == "" || cfg.RuntimeDir == "" {
		return nil
	}
	ip := cfg.MailServerIP
	if ip == "" && len(cfg.ServerIPs) > 0 {
		ip = cfg.ServerIPs[0]
	}
	var hosting mailhost.DNSHosting
	if dnsSvc.Enabled() {
		hosting = dnsSvc.ForMail()
	}
	svc := mailhost.New(db, mailhost.Config{Host: cfg.MailHost, WebmailURL: cfg.WebmailURL, ServerIP: ip, BaseDomain: cfg.BaseDomain, Dir: cfg.RuntimeDir,
		Applier: runtimes.FileApplier{Dir: cfg.RuntimeDir}, Sites: siteSvc, DNS: hosting})
	go svc.Watch(context.Background(), time.Minute)
	fmt.Println("почта на своих доменах: включена, сервер", cfg.MailHost)
	return svc
}

// startDNS подключает собственный DNS (зоны своих доменов). Без серверов имён или папки исполнителя раздел выключен.
func startDNS(db *gorm.DB, siteSvc *sites.Service, cfg config.Config) *dnszones.Service {
	if len(cfg.DNSNameservers) == 0 || cfg.RuntimeDir == "" {
		return nil
	}
	svc := dnszones.New(db, dnszones.Config{NS: cfg.DNSNameservers, Hostmaster: "hostmaster." + cfg.BaseDomain, ServerIPs: cfg.ServerIPs, BaseDomain: cfg.BaseDomain,
		Dir: cfg.RuntimeDir, Applier: runtimes.FileApplier{Dir: cfg.RuntimeDir}, Sites: siteSvc})
	go svc.Watch(context.Background(), time.Minute)
	fmt.Println("свой DNS: включён, серверы имён", strings.Join(cfg.DNSNameservers, ", "))
	return svc
}

// startShell подключает SSH-сервер и веб-терминал. Без исполнителя сред или сокета посредника раздел остаётся выключенным.
func startShell(db *gorm.DB, siteSvc *sites.Service, cfg config.Config) (*shellaccess.Service, shellclient.Client, error) {
	client := shellclient.Client{Socket: cfg.SSH.Socket}
	if cfg.SSH.Socket == "" || cfg.RuntimeDir == "" {
		return nil, client, nil
	}
	var srv *sshd.Server
	fp := func() string {
		if srv == nil {
			return ""
		}
		return srv.HostFingerprint()
	}
	svc := shellaccess.New(db, siteSvc, shellaccess.Config{Applier: runtimes.FileApplier{Dir: cfg.RuntimeDir}, Broker: client, BaseDomain: cfg.BaseDomain,
		SSHHost: cfg.SSH.Host, SSHPort: cfg.SSH.Port(), HostFingerprint: fp})
	if cfg.SSH.Addr == "" {
		fmt.Println("доступ к оболочке: включён только веб-терминал (VLADHOST_SSH_ADDR не задан)")
		return svc, client, nil
	}
	s, err := sshd.New(sshd.Config{HostKeyPath: cfg.SSH.HostKeyPath, Access: svc, Broker: client})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ВНИМАНИЕ: SSH-сервер отключён:", err)
		return svc, client, nil
	}
	l, err := net.Listen("tcp", cfg.SSH.Addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ВНИМАНИЕ: SSH-сервер отключён:", err)
		return svc, client, nil
	}
	srv = s
	go func() {
		if err := s.Serve(l); err != nil {
			fmt.Fprintln(os.Stderr, "SSH остановился:", err)
		}
	}()
	fmt.Println("SSH слушает", cfg.SSH.Addr, "отпечаток ключа", s.HostFingerprint())
	return svc, client, nil
}

// mailTest отправляет проверочное письмо и печатает результат: так видно, верны ли реквизиты SMTP, до того как о них узнают пользователи.
func mailTest(cfg config.Config, to string) error {
	if to == "" {
		return errors.New("использование: vladhost mail-test --to адрес@example.com")
	}
	if cfg.Mail.SpoolDir == "" && (cfg.Mail.Host == "" || cfg.Mail.From == "") {
		return errors.New("почта не настроена: задайте VLADHOST_SMTP_HOST и VLADHOST_SMTP_FROM (и логин/пароль) в /etc/vladhost/env")
	}
	var sender mailer.Sender
	var err error
	if cfg.Mail.SpoolDir != "" {
		sender, err = mailer.NewSpool(cfg.Mail.SpoolDir, cmp.Or(cfg.Mail.From, "noreply@localhost"))
	} else {
		sender, err = mailer.NewSMTP(mailer.Config{
			Host: cfg.Mail.Host, Port: cfg.Mail.Port, User: cfg.Mail.User, Password: cfg.Mail.Password,
			From: cfg.Mail.From, FromName: cfg.Mail.FromName, TLS: cfg.Mail.TLS, HelloName: cfg.BaseDomain,
		})
	}
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	msg := mailer.Message{To: to, Subject: "Vladhost: проверка почты", Text: "Это проверочное письмо панели Vladhost. Если вы его читаете, отправка почты настроена верно.\n"}
	if err := sender.Send(ctx, msg); err != nil {
		return fmt.Errorf("письмо не отправлено: %w", err)
	}
	fmt.Println("письмо отправлено на", to)
	return nil
}

// startCMS подключает установку приложений. Без сред выполнения или MariaDB она сама остаётся недоступной (cms.Enabled).
func startCMS(db *gorm.DB, siteSvc *sites.Service, dbs *userdb.Service, rt *runtimes.Service, cfg config.Config) (*cms.Service, error) {
	catalog, err := cms.LoadCatalog(cfg.CMS.CatalogFile)
	if err != nil {
		return nil, err
	}
	return cms.New(db, siteSvc, dbs, rt, cms.Config{Catalog: catalog, CacheDir: cfg.CMS.CacheDir, GatewayURL: cfg.CMS.GatewayURL, DBHost: cfg.UserDB.WebMariaHost}), nil
}

// startCron запускает планировщик задач. Команды включаются, только если настроены папки обмена с исполнителем.
func startCron(db *gorm.DB, siteSvc *sites.Service, cfg config.Config) *cronjobs.Service {
	c := cfg.Cron
	cc := cronjobs.Config{MaxJobs: c.MaxJobs, MinIntervalMi: c.MinIntervalMin, Timeout: time.Duration(c.TimeoutSec) * time.Second}
	if c.Queue != "" {
		cc.Commands = cronjobs.FileRunner{Queue: c.Queue, Results: c.Results}
	} else {
		fmt.Println("планировщик: команды выключены (VLADHOST_CRON_QUEUE не задан), работают только HTTP-задачи")
	}
	svc := cronjobs.New(db, siteSvc, cc)
	go svc.Watch(context.Background(), 20*time.Second)
	return svc
}

// startUserDB подключает серверы баз данных пользователей. Недоступный сервер только отключает свою СУБД (в журнал уходит
// предупреждение): панель, сайты и FTP от этого не страдают. Без настроек возвращает nil: раздел «Базы данных» выключен.
func startUserDB(db *gorm.DB, cfg config.Config) *userdb.Service {
	u := cfg.UserDB
	if !u.Enabled() {
		fmt.Println("базы данных пользователей не настроены (VLADHOST_DB_*): раздел «Базы данных» выключен")
		return nil
	}
	backends := map[userdb.Engine]userdb.Backend{}
	ping := func(name string, b userdb.Backend) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := b.Ping(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "ВНИМАНИЕ: %s для пользовательских баз недоступна, отключена: %v\n", name, err)
			return
		}
		fmt.Println("базы данных:", name, "подключена")
		switch name {
		case "PostgreSQL":
			backends[userdb.Postgres] = b
		case "MariaDB":
			backends[userdb.MariaDB] = b
		}
	}
	if u.PGAdminURL != "" {
		if b, err := userdb.NewPG(userdb.PGConfig{AdminURL: u.PGAdminURL, HBADir: u.PGHBADir}); err != nil {
			fmt.Fprintln(os.Stderr, "ВНИМАНИЕ: PostgreSQL для пользовательских баз отключена:", err)
		} else {
			ping("PostgreSQL", b)
		}
	}
	if u.MariaAdminDSN != "" {
		if b, err := userdb.NewMaria(userdb.MariaConfig{AdminDSN: u.MariaAdminDSN, ExternalAccess: u.MariaExternal}); err != nil {
			fmt.Fprintln(os.Stderr, "ВНИМАНИЕ: MariaDB для пользовательских баз отключена:", err)
		} else {
			ping("MariaDB", b)
		}
	}
	if len(backends) == 0 {
		return nil
	}
	svc := userdb.NewService(db, backends, userdb.Config{
		PerEngine: u.PerEngine, SizeLimit: int64(u.SizeMB) << 20, Host: u.Host,
		Ports:    map[userdb.Engine]int{userdb.Postgres: u.PGPort, userdb.MariaDB: u.MariaPort},
		External: map[userdb.Engine]bool{userdb.Postgres: u.PGHBADir != "", userdb.MariaDB: u.MariaExternal},
		WebURL:   u.WebURL, WebServers: map[userdb.Engine]string{userdb.Postgres: u.WebPGServer, userdb.MariaDB: u.WebMariaHost},
	})
	go svc.Watch(context.Background(), 5*time.Minute)
	return svc
}

// startMail включает почту, если она настроена: отправитель, фоновая отправка очереди и проверки для уведомлений.
// Без настроек возвращает nil: подтверждение почты, сброс пароля и уведомления тогда выключены.
func startMail(db *gorm.DB, siteSvc *sites.Service, cfg config.Config) (*notify.Service, error) {
	if !cfg.Mail.Enabled() {
		fmt.Println("почта не настроена (VLADHOST_SMTP_HOST): подтверждение адреса, сброс пароля и уведомления выключены")
		return nil, nil
	}
	var sender mailer.Sender
	var err error
	if cfg.Mail.SpoolDir != "" {
		sender, err = mailer.NewSpool(cfg.Mail.SpoolDir, cmp.Or(cfg.Mail.From, "noreply@localhost"))
	} else {
		sender, err = mailer.NewSMTP(mailer.Config{
			Host: cfg.Mail.Host, Port: cfg.Mail.Port, User: cfg.Mail.User, Password: cfg.Mail.Password,
			From: cfg.Mail.From, FromName: cfg.Mail.FromName, TLS: cfg.Mail.TLS, HelloName: cfg.BaseDomain,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("почта: %w", err)
	}
	origin := cmp.Or(cfg.PanelOrigin, "http://127.0.0.1:5174")
	svc := notify.New(db, sender, origin)
	go svc.Run(context.Background(), 10*time.Second)
	go svc.Watch(context.Background(), siteSvc, 30*time.Minute)
	fmt.Println("почта включена")
	return svc, nil
}

func startFTP(svc *sites.Service, cfg config.FTPConfig, onLogin func(userID int64, host, ip string)) error {
	ftp, err := ftpd.New(svc, cfg)
	if err != nil {
		return err
	}
	ftp.SetLoginHook(onLogin)
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
		return fmt.Errorf("использование: vladhost serve | web | migrate up | mail-test --to ADDR | admin create --email E --username U | admin reset-2fa LOGIN")
	}
	if args[0] == "shell-broker" {
		return runShellBroker(config.LoadBroker())
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
		siteSvc.ConfigureBackups(cfg.BackupDir)
		if cfg.BackupDir != "" {
			go siteSvc.RunBackups(context.Background(), time.Hour)
		}
		go siteSvc.WatchCerts(context.Background(), 5*time.Second)
		if len(cfg.ServerIPs) > 0 {
			siteSvc.ConfigureDomains(sites.DomainConfig{ServerIPs: cfg.ServerIPs, MappingDir: cfg.DomainsDir})
			go siteSvc.WatchDomains(context.Background(), 30*time.Second)
		}
		activitySvc := activity.New(db)
		go activitySvc.Run(context.Background(), time.Hour)
		if cfg.FTP.Addr != "" {
			// FTP необязателен для панели: если он не запустился (нет сертификата, порт занят), панель
			// продолжает работать, а в интерфейсе FTP показывается как недоступный.
			ftpLogin := func(userID int64, host, ip string) {
				activitySvc.Record(context.Background(), activity.Input{UserID: userID, Kind: activity.KindFTPLogin, Target: host, IP: ip})
			}
			if err := startFTP(siteSvc, cfg.FTP, ftpLogin); err != nil {
				fmt.Fprintln(os.Stderr, "ВНИМАНИЕ: FTP отключён:", err)
				cfg.FTP.Addr = ""
			}
		}
		mailSvc, err := startMail(db, siteSvc, cfg)
		if err != nil {
			return err
		}
		dbSvc := startUserDB(db, cfg)
		mailSvc.SetDatabaseLimit(int64(cfg.UserDB.SizeMB) << 20)
		var rtSvc *runtimes.Service
		if cfg.RuntimeDir != "" {
			rtSvc = runtimes.New(db, siteSvc, runtimes.FileApplier{Dir: cfg.RuntimeDir}, cfg.RuntimeDir)
			siteSvc.SetPermsFixer(rtSvc.FixPerms)
			fmt.Println("среды выполнения (PHP, Node.js, Python): включены, исполнитель", cfg.RuntimeDir)
		}
		shellSvc, shellClient, err := startShell(db, siteSvc, cfg)
		if err != nil {
			return err
		}
		if rtSvc != nil {
			rtSvc.SetShellCheck(shellSvc.SiteEnabled)
		}
		// Удаление сайта: сначала закрываем оболочки и доступ, потом убираем среду выполнения.
		siteSvc.SetDeleteHook(func(ctx context.Context, site sites.Site) {
			shellSvc.Purge(ctx, site)
			rtSvc.Purge(ctx, site)
		})
		cmsSvc, err := startCMS(db, siteSvc, dbSvc, rtSvc, cfg)
		if err != nil {
			return err
		}
		cronSvc := startCron(db, siteSvc, cfg)
		dnsSvc := startDNS(db, siteSvc, cfg)
		mailHostSvc := startMailHost(db, siteSvc, cfg, dnsSvc)
		if mailHostSvc.Enabled() {
			mailSvc.SetMailboxFill(func(ctx context.Context) ([]notify.MailboxFill, error) {
				rows, err := mailHostSvc.Fill(ctx)
				out := make([]notify.MailboxFill, 0, len(rows))
				for _, r := range rows {
					out = append(out, notify.MailboxFill{UserID: r.UserID, Address: r.Address, Used: r.Used, Quota: r.Quota})
				}
				return out, err
			})
		}
		return httpapi.New(svc, siteSvc, cfg, httpapi.WithActivity(activitySvc), httpapi.WithMail(mailSvc), httpapi.WithDatabases(dbSvc, cfg.UserDB.InternalKey), httpapi.WithCron(cronSvc), httpapi.WithRuntimes(rtSvc), httpapi.WithCMS(cmsSvc), httpapi.WithShell(shellSvc, shellClient), httpapi.WithMailHost(mailHostSvc), httpapi.WithDNS(dnsSvc), httpapi.WithTickets(tickets.New(db, mailSvc))).Run(cfg.Addr)
	case args[0] == "mail-test":
		// Проверка почты на сервере: настоящее письмо по настроенному SMTP (логин, пароль, TLS проверяются по-настоящему).
		fs := flag.NewFlagSet("mail-test", flag.ContinueOnError)
		to := fs.String("to", "", "адрес, на который отправить проверочное письмо")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return mailTest(cfg, *to)
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
	case args[0] == "admin" && len(args) == 3 && args[1] == "reset-2fa":
		// Пользователь потерял и телефон, и коды восстановления: снимаем второй фактор и закрываем все его сессии.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		u, err := svc.ResetTwoFactor(ctx, args[2])
		if err != nil {
			return err
		}
		fmt.Printf("двухфакторный вход для %s (%s) выключен, сессии закрыты\n", u.Username, u.Email)
		return nil
	}
	return fmt.Errorf("неизвестная команда %q", args[0])
}

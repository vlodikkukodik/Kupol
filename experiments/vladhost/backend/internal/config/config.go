// Package config читает настройки сервера из переменных окружения.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr         string
	DatabaseURL  string
	JWTSecret    []byte
	CookieSecure bool
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
	// Лимит запросов к /api/auth/* с одного IP; 0 — значения по умолчанию (20 в минуту, всплеск 10).
	AuthPerMinute, AuthBurst int

	SitesRoot      string // где лежат файлы сайтов: {SitesRoot}/{host}/public
	BaseDomain     string // сайты живут на {site}.{user}.{BaseDomain}
	FTP            FTPConfig
	ServerIPs      []string // IP сервера: на него пользователь направляет A-запись своего домена; пусто — свои домены выключены
	DomainsDir     string   // {домен} → адрес сайта; эту папку читает веб-шлюз
	LogDir         string   // журналы сайтов, которые пишет веб-шлюз; пусто — раздел «Журналы» недоступен
	CertsDir       string   // обмен с выпускателем сертификатов (queue/ и status/); пусто — выключено
	BackupDir      string   // ежедневные снимки сайтов; пусто — раздел выключен
	PanelOrigin    string   // https://app.vladinc.ru — для проверки Origin у cookie-эндпоинтов; пусто — не проверять
	Mail           MailConfig
	UserDB         UserDBConfig
	Cron           CronConfig
	CMS            CMSConfig
	SSH            SSHConfig
	MailServerIP   string   // IP в записи SPF почтовых доменов; пусто — первый из ServerIPs
	DNSNameservers []string // наши серверы имён (ns.vladinc.ru, ns2.vladinc.ru на одном IP): собственный DNS; пусто — раздел «DNS» выключен
	WebmailURL     string   // https://webmail.vladinc.ru — кнопка «Открыть webmail» в разделе «Почта»; пусто — кнопки нет
	MailHost       string   // mail.vladinc.ru: почта на своих доменах (ящики, алиасы); пусто — раздел «Почта» выключен
	RuntimeDir     string   // папка обмена с исполнителем сред выполнения (queue/, results/, caps); пусто — PHP, Node.js и Python выключены
	MaxSites       int      // сайтов на пользователя
	DiskQuotaBytes int64    // диск на пользователя (все его сайты вместе)
}

const minSecretLen = 32

// MailConfig — исходящая почта (например, smtp.majordomo.ru). Пустой адрес сервера и пустая папка выключают почту:
// подтверждение адреса, сброс пароля и уведомления тогда недоступны, всё остальное работает.
type MailConfig struct {
	Host     string
	Port     int    // 0 — по режиму TLS (587 для starttls, 465 для tls)
	User     string // логин SMTP; пусто — без авторизации
	Password string
	From     string // адрес отправителя, например noreply@vladinc.ru
	FromName string
	TLS      string // starttls (по умолчанию), tls или none (none — только локальный сервер)
	// SpoolDir — вместо SMTP складывать письма файлами .eml в эту папку (разработка и сквозные тесты).
	SpoolDir string
}

// UserDBConfig — пользовательские базы данных: серверы PostgreSQL и MariaDB, доступ снаружи и веб-клиент. Пустые настройки
// сервера выключают соответствующую СУБД; без обоих раздел «Базы данных» скрыт.
type UserDBConfig struct {
	PGAdminURL    string // служебное подключение к отдельному кластеру PostgreSQL для пользовательских баз
	PGHBADir      string // папка правил pg_hba внешнего доступа (include_dir); пусто — внешнего доступа к PostgreSQL нет
	MariaAdminDSN string // служебное подключение к MariaDB
	MariaExternal bool   // MariaDB принимает подключения снаружи (по TLS)
	Host          string // имя, по которому подключаются снаружи (db.vladinc.ru)
	PGPort        int
	MariaPort     int
	WebURL        string // https://db.vladinc.ru — веб-клиент (Adminer); пусто — веб-клиента нет
	WebPGServer   string // как веб-клиент подключается к PostgreSQL на этой машине
	WebMariaHost  string // ...и к MariaDB (localhost — через сокет)
	InternalKey   string // общий секрет между панелью и веб-клиентом для обмена токена входа на данные входа
	PerEngine     int    // баз каждой СУБД на пользователя
	SizeMB        int    // мегабайт на одну базу
}

// CronConfig — планировщик задач. HTTP-задачи работают всегда; команды в песочнице сайта — только если заданы обе папки обмена
// с исполнителем от root (deploy/bin/cron-run.sh): без них панель, работающая без прав, команды выполнить не может.
type CronConfig struct {
	Queue, Results string // заявки (пишет панель) и результаты (пишет только исполнитель)
	MaxJobs        int    // задач на аккаунт
	MinIntervalMin int    // не чаще раза в столько минут
	TimeoutSec     int    // предел выполнения одного запуска
}

func loadCron() (CronConfig, error) {
	c := CronConfig{Queue: os.Getenv("VLADHOST_CRON_QUEUE"), Results: os.Getenv("VLADHOST_CRON_RESULTS")}
	if (c.Queue == "") != (c.Results == "") {
		return c, errors.New("VLADHOST_CRON_QUEUE и VLADHOST_CRON_RESULTS задаются вместе")
	}
	for _, it := range []struct {
		name string
		dst  *int
		def  int
	}{{"VLADHOST_CRON_MAX_JOBS", &c.MaxJobs, 5}, {"VLADHOST_CRON_MIN_INTERVAL_MIN", &c.MinIntervalMin, 5}, {"VLADHOST_CRON_TIMEOUT_SEC", &c.TimeoutSec, 60}} {
		*it.dst = it.def
		if v := os.Getenv(it.name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 3600 {
				return c, fmt.Errorf("%s: нужно число от 1 до 3600, получено %q", it.name, v)
			}
			*it.dst = n
		}
	}
	return c, nil
}

// CMSConfig — установка приложений «в один клик» (WordPress). Работает, если есть среды выполнения и MariaDB.
type CMSConfig struct {
	CatalogFile string // JSON-каталог вместо встроенного (версия, адрес, sha256); пусто — встроенный
	CacheDir    string // где хранятся скачанные проверенные архивы; пусто — временная папка на установку
	GatewayURL  string // веб-шлюз, через который выполняется установка самого приложения
}

// SSHConfig — доступ к оболочке сайта: SSH-сервер панели и веб-терминал. Оболочки запускает посредник от root (vladhost shell-broker),
// панель общается с ним по unix-сокету. Пустой Socket выключает всё; пустой Addr выключает только внешний SSH (веб-терминал остаётся).
type SSHConfig struct {
	Addr        string // :2222
	HostKeyPath string // ключ SSH-сервера; создаётся при первом запуске
	Host        string // как подключаться снаружи (ssh.vladinc.ru), для подсказок в интерфейсе
	Socket      string // сокет посредника оболочек
}

// Port возвращает порт SSH-сервера для подсказок.
func (c SSHConfig) Port() int {
	_, p, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return 2222
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return 2222
	}
	return n
}

// BrokerConfig — настройки посредника оболочек (отдельная служба от root, без БД).
type BrokerConfig struct {
	Socket    string
	Dir       string // папка исполнителя сред: в ней метки shell/{id}
	SitesRoot string
	PanelUser string // кому разрешено подключаться к сокету
	// SystemdRun и Systemctl — пути к командам; меняются только в разработке и тестах (там нет systemd).
	SystemdRun string
	Systemctl  string
}

// LoadBroker читает настройки посредника оболочек.
func LoadBroker() BrokerConfig {
	return BrokerConfig{
		Socket:     env("VLADHOST_SHELL_SOCKET", "/run/vladhost/shell.sock"),
		Dir:        env("VLADHOST_RUNTIME_DIR", "/var/lib/vladhost/runtime"),
		SitesRoot:  env("VLADHOST_SITES_ROOT", "/data/vladhost/sites"),
		PanelUser:  env("VLADHOST_PANEL_USER", "vladhost"),
		SystemdRun: os.Getenv("VLADHOST_SHELL_SYSTEMD_RUN"), Systemctl: os.Getenv("VLADHOST_SHELL_SYSTEMCTL"),
	}
}

// Enabled сообщает, включена ли хотя бы одна СУБД.
func (u UserDBConfig) Enabled() bool { return u.PGAdminURL != "" || u.MariaAdminDSN != "" }

func loadUserDB() (UserDBConfig, error) {
	u := UserDBConfig{
		PGAdminURL: os.Getenv("VLADHOST_DB_PG_ADMIN_URL"), PGHBADir: os.Getenv("VLADHOST_DB_PG_HBA_DIR"),
		MariaAdminDSN: os.Getenv("VLADHOST_DB_MARIADB_ADMIN_DSN"), MariaExternal: env("VLADHOST_DB_MARIADB_EXTERNAL", "false") == "true",
		Host: os.Getenv("VLADHOST_DB_HOST"), WebURL: strings.TrimRight(os.Getenv("VLADHOST_DB_WEB_URL"), "/"),
		WebPGServer: env("VLADHOST_DB_WEB_PG_SERVER", "127.0.0.1:5433"), WebMariaHost: env("VLADHOST_DB_WEB_MARIADB_SERVER", "localhost"),
		InternalKey: os.Getenv("VLADHOST_DB_INTERNAL_KEY"),
	}
	ints := []struct {
		name string
		dst  *int
		def  int
	}{
		{"VLADHOST_DB_PG_PORT", &u.PGPort, 5433}, {"VLADHOST_DB_MARIADB_PORT", &u.MariaPort, 3306},
		{"VLADHOST_DB_PER_ENGINE", &u.PerEngine, 3}, {"VLADHOST_DB_SIZE_MB", &u.SizeMB, 100},
	}
	for _, it := range ints {
		*it.dst = it.def
		if v := os.Getenv(it.name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 1<<20 {
				return u, fmt.Errorf("%s: нужно положительное число, получено %q", it.name, v)
			}
			*it.dst = n
		}
	}
	if u.WebURL != "" && (!strings.HasPrefix(u.WebURL, "https://") && !strings.HasPrefix(u.WebURL, "http://127.0.0.1") && !strings.HasPrefix(u.WebURL, "http://localhost")) {
		return u, errors.New("VLADHOST_DB_WEB_URL: нужен адрес https:// (для разработки допустим http://127.0.0.1)")
	}
	if u.WebURL != "" && len(u.InternalKey) < 32 {
		return u, errors.New("VLADHOST_DB_INTERNAL_KEY: при включённом веб-клиенте нужен секрет не короче 32 символов")
	}
	return u, nil
}

// Enabled сообщает, настроена ли отправка почты.
func (m MailConfig) Enabled() bool { return m.SpoolDir != "" || (m.Host != "" && m.From != "") }

func loadMail() (MailConfig, error) {
	m := MailConfig{
		Host: os.Getenv("VLADHOST_SMTP_HOST"), User: os.Getenv("VLADHOST_SMTP_USER"), Password: os.Getenv("VLADHOST_SMTP_PASSWORD"),
		From: os.Getenv("VLADHOST_SMTP_FROM"), FromName: env("VLADHOST_SMTP_FROM_NAME", "Vladhost"),
		TLS: strings.ToLower(env("VLADHOST_SMTP_TLS", "starttls")), SpoolDir: os.Getenv("VLADHOST_MAIL_SPOOL"),
	}
	if v := os.Getenv("VLADHOST_SMTP_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			return m, fmt.Errorf("VLADHOST_SMTP_PORT: ожидается порт 1–65535, получено %q", v)
		}
		m.Port = p
	}
	if (m.Host != "") != (m.From != "") && m.SpoolDir == "" {
		return m, errors.New("VLADHOST_SMTP_HOST и VLADHOST_SMTP_FROM задаются вместе")
	}
	if m.User != "" && m.Password == "" {
		return m, errors.New("VLADHOST_SMTP_USER задан без VLADHOST_SMTP_PASSWORD")
	}
	return m, nil
}

// FTPConfig — встроенный FTP-сервер (только FTPS). Пустой Addr выключает FTP.
type FTPConfig struct {
	Addr         string // например ":2121"
	Host         string // имя, которое видят клиенты (ftp.vladinc.ru)
	PublicIP     string // IP для пассивного режима
	AllowPlain   bool   // принимать и обычный FTP без TLS (пароль и файлы идут открытым текстом); false — только FTPS
	PassiveStart int    // диапазон портов пассивного режима
	PassiveEnd   int
	CertFile     string // пусто — на старте создаётся временный самоподписанный сертификат (только dev)
	KeyFile      string
}

// Load собирает конфиг; обязательные значения без умолчаний, чтобы не запуститься с небезопасным секретом.
func Load() (Config, error) {
	cfg := Config{
		Addr:         env("VLADHOST_ADDR", "127.0.0.1:8090"),
		DatabaseURL:  os.Getenv("VLADHOST_DATABASE_URL"),
		JWTSecret:    []byte(os.Getenv("VLADHOST_JWT_SECRET")),
		CookieSecure: env("VLADHOST_COOKIE_SECURE", "true") != "false",
		AccessTTL:    15 * time.Minute,
		RefreshTTL:   30 * 24 * time.Hour,

		SitesRoot:      env("VLADHOST_SITES_ROOT", "/data/vladhost/sites"),
		BaseDomain:     env("VLADHOST_BASE_DOMAIN", "vladinc.ru"),
		CertsDir:       os.Getenv("VLADHOST_CERTS_DIR"),
		DomainsDir:     env("VLADHOST_DOMAINS_DIR", "/data/vladhost/domains"),
		LogDir:         os.Getenv("VLADHOST_LOG_DIR"),
		RuntimeDir:     os.Getenv("VLADHOST_RUNTIME_DIR"),
		BackupDir:      os.Getenv("VLADHOST_BACKUP_DIR"),
		PanelOrigin:    os.Getenv("VLADHOST_PANEL_ORIGIN"),
		MaxSites:       1,
		DiskQuotaBytes: 500 << 20,
	}
	if cfg.DatabaseURL == "" {
		return cfg, errors.New("VLADHOST_DATABASE_URL не задан")
	}
	if len(cfg.JWTSecret) < minSecretLen {
		return cfg, fmt.Errorf("VLADHOST_JWT_SECRET должен быть не короче %d символов", minSecretLen)
	}
	for _, ip := range strings.Split(os.Getenv("VLADHOST_SERVER_IPS"), ",") {
		if ip = strings.TrimSpace(ip); ip != "" {
			if _, err := netip.ParseAddr(ip); err != nil {
				return cfg, fmt.Errorf("VLADHOST_SERVER_IPS: %q не похож на IP-адрес", ip)
			}
			cfg.ServerIPs = append(cfg.ServerIPs, ip)
		}
	}
	if err := loadFTP(&cfg.FTP); err != nil {
		return cfg, err
	}
	var err error
	if cfg.Mail, err = loadMail(); err != nil {
		return cfg, err
	}
	if cfg.UserDB, err = loadUserDB(); err != nil {
		return cfg, err
	}
	cfg.CMS = CMSConfig{CatalogFile: os.Getenv("VLADHOST_CMS_CATALOG"), CacheDir: os.Getenv("VLADHOST_CMS_CACHE_DIR"), GatewayURL: env("VLADHOST_GATEWAY_URL", "http://127.0.0.1:8091")}
	cfg.SSH = SSHConfig{Addr: os.Getenv("VLADHOST_SSH_ADDR"), HostKeyPath: env("VLADHOST_SSH_HOST_KEY", "/data/vladhost/ssh/host_ed25519"),
		Host: env("VLADHOST_SSH_HOST", "ssh."+cfg.BaseDomain), Socket: os.Getenv("VLADHOST_SHELL_SOCKET")}
	cfg.MailHost = os.Getenv("VLADHOST_MAIL_HOST")
	cfg.MailServerIP = os.Getenv("VLADHOST_MAIL_SERVER_IP")
	for _, n := range strings.Split(os.Getenv("VLADHOST_DNS_NS"), ",") {
		if n = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(n), ".")); n != "" {
			cfg.DNSNameservers = append(cfg.DNSNameservers, n)
		}
	}
	cfg.WebmailURL = strings.TrimRight(os.Getenv("VLADHOST_WEBMAIL_URL"), "/")
	if cfg.Cron, err = loadCron(); err != nil {
		return cfg, err
	}
	if v := os.Getenv("VLADHOST_ACCESS_TTL_MINUTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return cfg, errors.New("VLADHOST_ACCESS_TTL_MINUTES: нужно положительное число")
		}
		cfg.AccessTTL = time.Duration(n) * time.Minute
	}
	return cfg, nil
}

// WebConfig — настройки веб-шлюза сайтов. Ему не нужны ни БД, ни секреты: только каталог сайтов.
type WebConfig struct {
	Addr       string
	SitesRoot  string
	BaseDomain string
	DomainsDir string // {домен} → адрес сайта (свои домены); пусто — не используется
	LogDir     string // журналы сайтов (доступ и ошибки); пусто — не ведутся
	// PHPSocketDir — каталог сокетов пулов PHP-FPM сайтов.
	PHPSocketDir string
}

func LoadWeb() WebConfig {
	return WebConfig{
		Addr:         env("VLADHOST_WEB_ADDR", "127.0.0.1:8091"),
		SitesRoot:    env("VLADHOST_SITES_ROOT", "/data/vladhost/sites"),
		BaseDomain:   env("VLADHOST_BASE_DOMAIN", "vladinc.ru"),
		DomainsDir:   env("VLADHOST_DOMAINS_DIR", "/data/vladhost/domains"),
		LogDir:       os.Getenv("VLADHOST_LOG_DIR"),
		PHPSocketDir: env("VLADHOST_PHP_SOCKET_DIR", "/run/vhphp"),
	}
}

func loadFTP(f *FTPConfig) error {
	f.Addr = os.Getenv("VLADHOST_FTP_ADDR")
	if f.Addr == "" {
		return nil
	}
	f.Host = env("VLADHOST_FTP_HOST", "ftp.vladinc.ru")
	f.PublicIP = os.Getenv("VLADHOST_FTP_PUBLIC_IP")
	f.AllowPlain = env("VLADHOST_FTP_ALLOW_PLAIN", "true") != "false"
	f.CertFile = os.Getenv("VLADHOST_FTP_CERT")
	f.KeyFile = os.Getenv("VLADHOST_FTP_KEY")
	if (f.CertFile == "") != (f.KeyFile == "") {
		return errors.New("VLADHOST_FTP_CERT и VLADHOST_FTP_KEY задаются вместе")
	}
	ports := env("VLADHOST_FTP_PASSIVE_PORTS", "50000-50100")
	lo, hi, ok := strings.Cut(ports, "-")
	var err1, err2 error
	f.PassiveStart, err1 = strconv.Atoi(lo)
	f.PassiveEnd, err2 = strconv.Atoi(hi)
	if !ok || err1 != nil || err2 != nil || f.PassiveStart < 1024 || f.PassiveEnd < f.PassiveStart || f.PassiveEnd > 65535 {
		return fmt.Errorf("VLADHOST_FTP_PASSIVE_PORTS: ожидается диапазон вида 50000-50100, получено %q", ports)
	}
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

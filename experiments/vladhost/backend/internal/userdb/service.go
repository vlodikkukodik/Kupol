package userdb

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"vladhost/internal/apperr"
	"vladhost/internal/auth"
)

// Статусы базы.
const (
	StatusActive = "active"
	StatusFrozen = "frozen" // превышен лимит размера: чтение и удаление данных разрешены, запись нет
)

var (
	ErrEngineUnavailable = apperr.New(http.StatusConflict, "db_engine_unavailable", "database engine is not enabled")
	ErrNameInvalid       = apperr.Validation("name", "db_name", "invalid database name")
	ErrLimit             = apperr.New(http.StatusForbidden, "db_limit", "database limit reached")
	ErrTaken             = apperr.New(http.StatusConflict, "db_taken", "database name is already taken").OnField("name")
	ErrNotFound          = apperr.New(http.StatusNotFound, "db_not_found", "database not found")
	ErrAddrInvalid       = apperr.Validation("addrs", "db_addr", "invalid address")
	ErrAddrLimit         = apperr.New(http.StatusUnprocessableEntity, "db_addr_limit", "too many addresses").OnField("addrs")
	ErrExternalOff       = apperr.New(http.StatusConflict, "db_external_unavailable", "external access is not enabled")
	ErrWebOff            = apperr.New(http.StatusConflict, "db_web_unavailable", "web client is not enabled")
	ErrBadSession        = apperr.New(http.StatusUnauthorized, "db_bad_session", "invalid or expired web client session")
)

// Database — учтённая в панели база.
type Database struct {
	ID            int64      `gorm:"primaryKey" json:"id"`
	UserID        int64      `json:"-"`
	Engine        Engine     `json:"engine"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	SizeBytes     int64      `json:"size_bytes"`
	SizeCheckedAt *time.Time `json:"size_checked_at"`
	FrozenAt      *time.Time `json:"frozen_at"`
	CreatedAt     time.Time  `json:"created_at"`
	// Addrs — адреса внешнего доступа (заполняется при чтении).
	Addrs []string `gorm:"-" json:"addrs"`
}

func (Database) TableName() string { return "user_databases" }

type allowedIP struct {
	ID         int64 `gorm:"primaryKey"`
	DatabaseID int64
	Addr       string
	CreatedAt  time.Time
}

func (allowedIP) TableName() string { return "database_allowed_ips" }

type tempAccount struct {
	ID         int64 `gorm:"primaryKey"`
	DatabaseID int64
	Account    string
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

func (tempAccount) TableName() string { return "database_temp_accounts" }

// Config — настройки службы.
type Config struct {
	PerEngine int   // баз каждой СУБД на пользователя
	SizeLimit int64 // байт на одну базу
	// Host и Ports — куда подключаться снаружи (показывается в панели).
	Host  string
	Ports map[Engine]int
	// External — какие СУБД принимают внешние подключения.
	External map[Engine]bool
	// WebURL — адрес веб-клиента (https://db.vladinc.ru); пусто — веб-клиента нет.
	WebURL string
	// WebServers — как веб-клиент подключается к серверам на этой же машине: "127.0.0.1:5433" для PostgreSQL, "localhost" для MariaDB.
	WebServers map[Engine]string
	// WebTTL — сколько живёт временная учётная запись веб-клиента.
	WebTTL time.Duration
}

func (c Config) withDefaults() Config {
	if c.PerEngine <= 0 {
		c.PerEngine = 3
	}
	if c.SizeLimit <= 0 {
		c.SizeLimit = 100 << 20
	}
	if c.WebTTL <= 0 {
		c.WebTTL = time.Hour
	}
	return c
}

// Session — данные для входа веб-клиента; отдаются один раз по одноразовому токену.
type Session struct {
	Driver   string `json:"driver"` // pgsql или server (так называются драйверы в Adminer)
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
	DB       string `json:"db"`
}

type pending struct {
	sess    Session
	expires time.Time
}

// Service — учёт баз, лимиты и операции над серверами СУБД.
type Service struct {
	db       *gorm.DB
	backends map[Engine]Backend
	cfg      Config
	now      func() time.Time

	locks    sync.Map // id базы → *sync.Mutex
	mu       sync.Mutex
	tokens   map[string]pending // хеш токена → данные входа
	compacts map[int64]time.Time
}

// NewService создаёт службу. backends — включённые СУБД.
func NewService(db *gorm.DB, backends map[Engine]Backend, cfg Config) *Service {
	return &Service{db: db, backends: backends, cfg: cfg.withDefaults(), now: time.Now, tokens: map[string]pending{}, compacts: map[int64]time.Time{}}
}

// Info — сведения для интерфейса.
type Info struct {
	Engines   []Engine        `json:"engines"`
	PerEngine int             `json:"per_engine"`
	SizeLimit int64           `json:"size_limit"`
	Host      string          `json:"host"`
	Ports     map[Engine]int  `json:"ports"`
	External  map[Engine]bool `json:"external"`
	WebClient bool            `json:"web_client"`
	MaxAddrs  int             `json:"max_addrs"`
}

// Info возвращает включённые СУБД и лимиты.
func (s *Service) Info() Info {
	in := Info{Engines: []Engine{}, PerEngine: s.cfg.PerEngine, SizeLimit: s.cfg.SizeLimit, Host: s.cfg.Host, Ports: map[Engine]int{}, External: map[Engine]bool{},
		WebClient: s.cfg.WebURL != "", MaxAddrs: MaxIPs}
	for _, e := range Engines {
		if _, ok := s.backends[e]; ok {
			in.Engines = append(in.Engines, e)
			in.Ports[e] = s.cfg.Ports[e]
			in.External[e] = s.cfg.External[e] && s.cfg.Host != ""
		}
	}
	return in
}

// Enabled сообщает, включена ли хотя бы одна СУБД.
func (s *Service) Enabled() bool { return s != nil && len(s.backends) > 0 }

func (s *Service) lock(id int64) *sync.Mutex {
	m, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func (s *Service) backend(e Engine) (Backend, error) {
	b, ok := s.backends[e]
	if !e.Valid() || !ok {
		return nil, ErrEngineUnavailable
	}
	return b, nil
}

// Create заводит базу и возвращает её пароль (виден один раз: в БД панели пароль не хранится).
func (s *Service) Create(ctx context.Context, u auth.User, engine Engine, name string) (*Database, string, error) {
	be, err := s.backend(engine)
	if err != nil {
		return nil, "", err
	}
	if !ValidName(name) {
		return nil, "", ErrNameInvalid
	}
	full := FullName(u.Username, name)
	row := &Database{UserID: u.ID, Engine: engine, Name: full, Status: StatusActive, CreatedAt: s.now()}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Блокировка строки пользователя сериализует параллельные создания: лимит нельзя обойти гонкой.
		if err := tx.Raw("SELECT id FROM users WHERE id = ? FOR UPDATE", u.ID).Scan(new(int64)).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&Database{}).Where("user_id = ? AND engine = ?", u.ID, engine).Count(&n).Error; err != nil {
			return err
		}
		if int(n) >= s.cfg.PerEngine {
			return ErrLimit.With(s.cfg.PerEngine)
		}
		if err := tx.Create(row).Error; err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return ErrTaken
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	pw, err := NewPassword()
	if err != nil {
		s.forget(ctx, row.ID)
		return nil, "", err
	}
	if err := be.Create(ctx, full, pw); err != nil {
		s.forget(ctx, row.ID)
		log.Printf("базы данных: создание %s/%s: %v", engine, full, err)
		return nil, "", fmt.Errorf("create database: %w", err)
	}
	row.Addrs = []string{}
	return row, pw, nil
}

func (s *Service) forget(ctx context.Context, id int64) {
	if err := s.db.WithContext(context.WithoutCancel(ctx)).Delete(&Database{}, id).Error; err != nil {
		log.Printf("базы данных: откат записи %d: %v", id, err)
	}
}

// fill добавляет адреса внешнего доступа.
func (s *Service) fill(ctx context.Context, list []Database) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]int64, len(list))
	for i := range list {
		ids[i] = list[i].ID
		list[i].Addrs = []string{}
	}
	var rows []allowedIP
	if err := s.db.WithContext(ctx).Where("database_id IN ?", ids).Order("id").Find(&rows).Error; err != nil {
		return err
	}
	byID := map[int64]*Database{}
	for i := range list {
		byID[list[i].ID] = &list[i]
	}
	for _, r := range rows {
		byID[r.DatabaseID].Addrs = append(byID[r.DatabaseID].Addrs, r.Addr)
	}
	return nil
}

// List — базы пользователя.
func (s *Service) List(ctx context.Context, userID int64) ([]Database, error) {
	var list []Database
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("id").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, s.fill(ctx, list)
}

// Get возвращает базу пользователя.
func (s *Service) Get(ctx context.Context, userID, id int64) (*Database, error) {
	var d Database
	err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	list := []Database{d}
	if err := s.fill(ctx, list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// Delete удаляет базу вместе с данными и учётными записями.
func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	d, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	mu := s.lock(id)
	mu.Lock()
	defer mu.Unlock()
	be, err := s.backend(d.Engine)
	if err != nil {
		return err
	}
	if err := be.Drop(ctx, d.Name, d.ID); err != nil {
		return fmt.Errorf("drop database: %w", err)
	}
	return s.db.WithContext(ctx).Delete(&Database{}, d.ID).Error
}

// ResetPassword выдаёт базе новый пароль (виден один раз).
func (s *Service) ResetPassword(ctx context.Context, userID, id int64) (*Database, string, error) {
	d, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, "", err
	}
	be, err := s.backend(d.Engine)
	if err != nil {
		return nil, "", err
	}
	pw, err := NewPassword()
	if err != nil {
		return nil, "", err
	}
	mu := s.lock(id)
	mu.Lock()
	defer mu.Unlock()
	if err := be.SetPassword(ctx, d.Name, pw); err != nil {
		return nil, "", fmt.Errorf("set password: %w", err)
	}
	return d, pw, nil
}

// SetAddrs заменяет список адресов внешнего доступа.
func (s *Service) SetAddrs(ctx context.Context, userID, id int64, raw []string) (*Database, error) {
	d, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	be, err := s.backend(d.Engine)
	if err != nil {
		return nil, err
	}
	if !s.cfg.External[d.Engine] || s.cfg.Host == "" {
		return nil, ErrExternalOff
	}
	addrs, err := NormalizeAddrs(raw)
	if err != nil {
		if len(raw) > MaxIPs {
			return nil, ErrAddrLimit.With(MaxIPs)
		}
		return nil, ErrAddrInvalid
	}
	mu := s.lock(id)
	mu.Lock()
	defer mu.Unlock()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("database_id = ?", d.ID).Delete(&allowedIP{}).Error; err != nil {
			return err
		}
		for _, a := range addrs {
			if err := tx.Create(&allowedIP{DatabaseID: d.ID, Addr: a, CreatedAt: s.now()}).Error; err != nil {
				return err
			}
		}
		// Сервер СУБД меняется внутри транзакции: если он откажет, список в панели не изменится.
		return be.SetAccess(ctx, d.Name, d.ID, addrs, d.Status == StatusFrozen)
	})
	if err != nil {
		return nil, fmt.Errorf("external access: %w", err)
	}
	d.Addrs = addrs
	if d.Addrs == nil {
		d.Addrs = []string{}
	}
	return d, nil
}

// ---- веб-клиент ----

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// OpenWebClient готовит вход в веб-клиент: создаёт временную учётную запись и одноразовую ссылку. Пароль базы клиенту не передаётся.
func (s *Service) OpenWebClient(ctx context.Context, userID, id int64) (string, error) {
	if s.cfg.WebURL == "" {
		return "", ErrWebOff
	}
	d, err := s.Get(ctx, userID, id)
	if err != nil {
		return "", err
	}
	be, err := s.backend(d.Engine)
	if err != nil {
		return "", err
	}
	expires := s.now().Add(s.cfg.WebTTL)
	acct, pw, err := be.TempAccount(ctx, d.Name, expires, d.Status == StatusFrozen)
	if err != nil {
		return "", fmt.Errorf("temporary account: %w", err)
	}
	if err := s.db.WithContext(ctx).Create(&tempAccount{DatabaseID: d.ID, Account: acct, ExpiresAt: expires, CreatedAt: s.now()}).Error; err != nil {
		_ = be.DropTemp(context.WithoutCancel(ctx), d.Name, acct)
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	driver := "server"
	if d.Engine == Postgres {
		driver = "pgsql"
	}
	s.mu.Lock()
	for k, p := range s.tokens { // просроченные ссылки убираются сразу
		if p.expires.Before(s.now()) {
			delete(s.tokens, k)
		}
	}
	s.tokens[hashToken(token)] = pending{sess: Session{Driver: driver, Server: s.cfg.WebServers[d.Engine], Username: acct, Password: pw, DB: d.Name}, expires: s.now().Add(2 * time.Minute)}
	s.mu.Unlock()
	return s.cfg.WebURL + "/?vhtoken=" + token, nil
}

// Redeem меняет одноразовый токен на данные входа. Вызывается только веб-клиентом (см. внутренний обработчик API); повторно токен не работает.
func (s *Service) Redeem(token string) (Session, error) {
	h := hashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.tokens[h]
	delete(s.tokens, h)
	if !ok || p.expires.Before(s.now()) {
		return Session{}, ErrBadSession
	}
	return p.sess, nil
}

// Cleanup удаляет временные учётные записи с истёкшим сроком.
func (s *Service) Cleanup(ctx context.Context) error {
	var rows []tempAccount
	if err := s.db.WithContext(ctx).Where("expires_at < ?", s.now()).Find(&rows).Error; err != nil {
		return err
	}
	var first error
	for _, r := range rows {
		var d Database
		if err := s.db.WithContext(ctx).First(&d, r.DatabaseID).Error; err != nil {
			s.db.WithContext(ctx).Delete(&tempAccount{}, r.ID)
			continue
		}
		be, err := s.backend(d.Engine)
		if err == nil {
			err = be.DropTemp(ctx, d.Name, r.Account)
		}
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		s.db.WithContext(ctx).Delete(&tempAccount{}, r.ID)
	}
	return first
}

// ---- размер и заморозка ----

const (
	unfreezeRatio   = 0.9 // размагничиваем базу, когда она заметно меньше лимита: иначе она «дребезжала» бы на границе
	compactInterval = 15 * time.Minute
)

// CheckSizes измеряет все базы, замораживает превысившие лимит и размораживает освободившие место.
func (s *Service) CheckSizes(ctx context.Context) error {
	var list []Database
	if err := s.db.WithContext(ctx).Find(&list).Error; err != nil {
		return err
	}
	var first error
	for _, d := range list {
		if err := s.checkOne(ctx, d); err != nil {
			log.Printf("базы данных: проверка %s/%s: %v", d.Engine, d.Name, err)
			if first == nil {
				first = err
			}
		}
	}
	return first
}

func (s *Service) checkOne(ctx context.Context, d Database) error {
	be, err := s.backend(d.Engine)
	if err != nil {
		return nil // СУБД выключена: базу не трогаем
	}
	mu := s.lock(d.ID)
	mu.Lock()
	defer mu.Unlock()
	// Пока ждали замок, базу могли удалить.
	var cur Database
	if err := s.db.WithContext(ctx).First(&cur, d.ID).Error; err != nil {
		return nil
	}
	d = cur
	size, err := be.Size(ctx, d.Name)
	if err != nil {
		return err
	}
	now := s.now()
	upd := map[string]any{"size_bytes": size, "size_checked_at": now}
	switch {
	case d.Status == StatusActive && size > s.cfg.SizeLimit:
		if err := be.Freeze(ctx, d.Name, d.ID); err != nil {
			return fmt.Errorf("freeze: %w", err)
		}
		upd["status"], upd["frozen_at"] = StatusFrozen, now
	case d.Status == StatusFrozen:
		limit := int64(float64(s.cfg.SizeLimit) * unfreezeRatio)
		if size > limit && s.now().Sub(s.compacts[d.ID]) >= compactInterval {
			// Пользователь мог удалить данные: возвращаем серверу освободившееся место и измеряем заново.
			s.compacts[d.ID] = s.now()
			if err := be.Compact(ctx, d.Name); err != nil {
				return fmt.Errorf("compact: %w", err)
			}
			if size, err = be.Size(ctx, d.Name); err != nil {
				return err
			}
			upd["size_bytes"] = size
		}
		if size <= limit {
			if err := be.Unfreeze(ctx, d.Name, d.ID); err != nil {
				return fmt.Errorf("unfreeze: %w", err)
			}
			upd["status"], upd["frozen_at"] = StatusActive, nil
		}
	}
	return s.db.WithContext(ctx).Model(&Database{}).Where("id = ?", d.ID).Updates(upd).Error
}

// Check запрашивает внеплановую проверку размера одной базы (кнопка «Проверить» в панели).
func (s *Service) Check(ctx context.Context, userID, id int64) (*Database, error) {
	d, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	// Сжатие по запросу пользователя выполняется сразу, а не раз в compactInterval.
	s.mu.Lock()
	delete(s.compacts, id)
	s.mu.Unlock()
	if err := s.checkOne(ctx, *d); err != nil {
		return nil, err
	}
	return s.Get(ctx, userID, id)
}

// Watch раз в every чистит временные учётные записи и проверяет размеры баз, пока не отменён ctx.
func (s *Service) Watch(ctx context.Context, every time.Duration) {
	if !s.Enabled() {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.Cleanup(ctx); err != nil && ctx.Err() == nil {
			log.Printf("базы данных: очистка временных записей: %v", err)
		}
		_ = s.CheckSizes(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Names возвращает имена баз (для проверок и тестов).
func (s *Service) Names(ctx context.Context) ([]string, error) {
	var names []string
	err := s.db.WithContext(ctx).Model(&Database{}).Pluck("name", &names).Error
	slices.Sort(names)
	return names, err
}

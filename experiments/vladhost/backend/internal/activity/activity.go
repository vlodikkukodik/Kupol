// Package activity — журнал действий аккаунта: кто (аккаунт), что сделал, над чем, с какого адреса и когда. Пишется из слоя HTTP после успешных
// действий и при входах; пользователь читает свои события. Журнал не должен ломать основной запрос: сбой записи только попадает в лог сервера.
package activity

import (
	"context"
	"log"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// Пределы.
const (
	Retention    = 180 * 24 * time.Hour // сколько хранятся события
	MaxPerUser   = 5000                 // строк на пользователя (старые удаляются)
	CoalesceWin  = 10 * time.Minute     // окно слияния однотипных частых событий
	DefaultLimit = 50
	MaxLimit     = 100
	maxTarget    = 200
	maxAgent     = 200
	maxIP        = 45
)

// Category — группа события для фильтра по виду.
const (
	CatSecurity = "security" // входы, пароли, профиль, ключи SSH, доступ к оболочке
	CatSites    = "sites"    // сайты, файлы, домены, копии, сертификаты, среды, приложения, cron
	CatAccess   = "access"   // FTP
	CatServices = "services" // базы данных, почта, DNS
	CatOther    = "other"
)

var categories = []string{CatSecurity, CatSites, CatAccess, CatServices}

// Categories возвращает виды событий в порядке показа.
func Categories() []string { return append([]string(nil), categories...) }

// coalesced — события, которые сливаются в одну строку (правки файлов идут десятками, неудачные входы и соединения FTP — потоком).
var coalesced = map[string]bool{"files.change": true, "auth.login_failed": true, "ftp.login": true}

var kindRe = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)?$`)

// CategoryOf определяет группу по началу имени события.
func CategoryOf(kind string) string {
	prefix, _, _ := strings.Cut(kind, ".")
	switch prefix {
	case "auth", "profile", "ssh", "shell", "admin":
		return CatSecurity
	case "site", "files", "domain", "backup", "cert", "runtime", "cms", "cron":
		return CatSites
	case "ftp":
		return CatAccess
	case "db", "mail", "dns":
		return CatServices
	}
	return CatOther
}

// Event — строка журнала.
type Event struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    int64     `json:"-"`
	Kind      string    `json:"kind"`
	Category  string    `json:"category"`
	Target    string    `json:"target"`
	Count     int       `json:"count"`
	IP        string    `gorm:"column:ip" json:"ip"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Event) TableName() string { return "account_events" }

// Input — новое событие.
type Input struct {
	UserID    int64
	Kind      string
	Target    string
	IP        string
	UserAgent string
}

// Service — журнал.
type Service struct {
	db  *gorm.DB
	now func() time.Time
}

// New создаёт службу.
func New(db *gorm.DB) *Service { return &Service{db: db, now: time.Now} }

func clip(v string, n int) string {
	v = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, strings.TrimSpace(v))
	if utf8.RuneCountInString(v) > n {
		v = string([]rune(v)[:n])
	}
	if !utf8.ValidString(v) {
		v = strings.ToValidUTF8(v, "")
	}
	return v
}

// Record записывает событие. Ошибка не возвращается: журнал вторичен, а основное действие уже выполнено.
func (s *Service) Record(ctx context.Context, in Input) {
	if s == nil || in.UserID <= 0 || !kindRe.MatchString(in.Kind) {
		return
	}
	ctx = context.WithoutCancel(ctx)
	target, ip, agent := clip(in.Target, maxTarget), clip(in.IP, maxIP), clip(in.UserAgent, maxAgent)
	now := s.now()
	if coalesced[in.Kind] {
		res := s.db.WithContext(ctx).Exec(`UPDATE account_events SET count = count + 1, updated_at = ? WHERE id = (
			SELECT id FROM account_events WHERE user_id = ? AND kind = ? AND target = ? AND ip = ? AND updated_at > ? ORDER BY id DESC LIMIT 1)`,
			now, in.UserID, in.Kind, target, ip, now.Add(-CoalesceWin))
		if res.Error == nil && res.RowsAffected > 0 {
			return
		}
	}
	e := Event{UserID: in.UserID, Kind: in.Kind, Category: CategoryOf(in.Kind), Target: target, Count: 1, IP: ip, UserAgent: agent, CreatedAt: now, UpdatedAt: now}
	if err := s.db.WithContext(ctx).Create(&e).Error; err != nil {
		log.Printf("журнал действий: %v", err)
	}
}

// List возвращает события пользователя, свежие сверху. before — идентификатор, начиная ниже которого читать (0 — с начала);
// второй результат — курсор следующей страницы (0 — больше нет).
func (s *Service) List(ctx context.Context, userID int64, category string, before int64, limit int) ([]Event, int64, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	q := s.db.WithContext(ctx).Where("user_id = ?", userID)
	if category != "" {
		q = q.Where("category = ?", category)
	}
	if before > 0 {
		q = q.Where("id < ?", before)
	}
	var rows []Event
	if err := q.Order("id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	var next int64
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].ID
	}
	if rows == nil {
		rows = []Event{}
	}
	return rows, next, nil
}

// Prune удаляет старые события и лишние строки у тех, у кого их слишком много.
func (s *Service) Prune(ctx context.Context) error {
	if err := s.db.WithContext(ctx).Where("created_at < ?", s.now().Add(-Retention)).Delete(&Event{}).Error; err != nil {
		return err
	}
	var heavy []int64
	if err := s.db.WithContext(ctx).Model(&Event{}).Select("user_id").Group("user_id").Having("count(*) > ?", MaxPerUser).Pluck("user_id", &heavy).Error; err != nil {
		return err
	}
	for _, uid := range heavy {
		if err := s.db.WithContext(ctx).Exec(`DELETE FROM account_events WHERE user_id = ? AND id NOT IN (
			SELECT id FROM account_events WHERE user_id = ? ORDER BY id DESC LIMIT ?)`, uid, uid, MaxPerUser).Error; err != nil {
			return err
		}
	}
	return nil
}

// Run раз в every чистит журнал, пока не отменён ctx.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.Prune(ctx); err != nil && ctx.Err() == nil {
			log.Printf("журнал действий: очистка: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

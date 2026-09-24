// Package sites: сайты пользователей, деплой zip-архива и квоты.
package sites

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vladhost/internal/apperr"
	"vladhost/internal/auth"
)

var (
	ErrLimit     = apperr.New(http.StatusForbidden, "site_limit", "site limit reached")
	ErrSlugTaken = apperr.New(http.StatusConflict, "slug_taken", "site name already taken").OnField("slug")
	ErrNotFound  = apperr.New(http.StatusNotFound, "not_found", "site not found")
	ErrQuota     = apperr.New(http.StatusRequestEntityTooLarge, "quota_exceeded", "disk quota exceeded")
)

type Site struct {
	ID         int64      `gorm:"primaryKey" json:"id"`
	UserID     int64      `json:"-"`
	Slug       string     `json:"slug"`
	Host       string     `json:"host"`
	DiskBytes  int64      `json:"disk_bytes"`
	Status     string     `json:"status"`
	DeployedAt *time.Time `json:"deployed_at"`
	CreatedAt  time.Time  `json:"created_at"`

	FTPEnabled      bool   `json:"ftp_enabled"`
	FTPPasswordHash string `json:"-"`

	CertStatus      string     `json:"cert_status"`
	CertError       string     `json:"cert_error"`
	CertRequestedAt *time.Time `json:"-"`
}

const (
	CertNone    = "none"
	CertPending = "pending"
	CertActive  = "active"
	CertFailed  = "failed"
)

type Limits struct {
	MaxSites       int
	DiskQuotaBytes int64
}

type Service struct {
	db         *gorm.DB
	root       string
	baseDomain string
	certsDir   string // пусто — сертификаты не выпускаются (dev)
	limits     Limits
	locks      sync.Map // host -> *sync.Mutex: один деплой на сайт за раз

	ftpMu      sync.Mutex
	ftpUsage   map[int64]*siteUsage // счётчик занятого места у сайтов с открытыми FTP-сессиями
	ftpRevoked map[int64]time.Time  // когда у сайта последний раз отозвали или сменили FTP-пароль
}

func NewService(db *gorm.DB, root, baseDomain, certsDir string, limits Limits) *Service {
	return &Service{
		db: db, root: root, baseDomain: baseDomain, certsDir: certsDir, limits: limits,
		ftpUsage: map[int64]*siteUsage{}, ftpRevoked: map[int64]time.Time{},
	}
}

func (s *Service) Limits() Limits { return s.limits }

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}[a-z0-9]$`)

func normalizeSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if !slugRe.MatchString(slug) || strings.Contains(slug, "--") {
		return "", apperr.Validation("slug", "slug", "invalid site name")
	}
	return slug, nil
}

func (s *Service) Create(ctx context.Context, user auth.User, rawSlug string) (*Site, error) {
	slug, err := normalizeSlug(rawSlug)
	if err != nil {
		return nil, err
	}
	site := Site{UserID: user.ID, Slug: slug, Host: slug + "." + user.Username + "." + s.baseDomain, Status: "empty", CertStatus: CertNone}
	if s.certsEnabled() {
		now := time.Now()
		site.CertStatus, site.CertRequestedAt = CertPending, &now
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Блокировка строки пользователя сериализует параллельные создания и не даёт обойти лимит.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&auth.User{}, user.ID).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&Site{}).Where("user_id = ?", user.ID).Count(&n).Error; err != nil {
			return err
		}
		if int(n) >= s.limits.MaxSites {
			return ErrLimit
		}
		if err := tx.Create(&site).Error; err != nil {
			return mapUnique(err)
		}
		if err := os.MkdirAll(filepath.Join(s.siteDir(site.Host), "public"), 0o755); err != nil {
			return err
		}
		if s.certsEnabled() {
			return s.enqueue("issue", site.Host)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &site, nil
}

func (s *Service) List(ctx context.Context, userID int64) ([]Site, error) {
	var out []Site
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("id").Find(&out).Error
	return out, err
}

func (s *Service) Get(ctx context.Context, userID, id int64) (*Site, error) {
	var site Site
	err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&site).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &site, err
}

func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	mu := s.lock(site.Host)
	mu.Lock()
	defer mu.Unlock()
	if err := os.RemoveAll(s.siteDir(site.Host)); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&Site{}, site.ID).Error; err != nil {
		return err
	}
	if s.certsEnabled() {
		// Сертификат больше не нужен; ошибка здесь не должна мешать удалению — заявку можно повторить вручную.
		if err := s.enqueue("delete", site.Host); err != nil {
			log.Printf("заявка на удаление сертификата %s: %v", site.Host, err)
		}
	}
	return nil
}

// Deploy заменяет содержимое сайта архивом. Старая версия остаётся рабочей, пока новая не распакована
// целиком; подмена — двумя rename, так что посетитель не видит полусобранный сайт.
func (s *Service) Deploy(ctx context.Context, userID, id int64, archive io.ReaderAt, size int64) (*Site, error) {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(archive, size)
	if err != nil {
		return nil, badArchive("not_zip")
	}

	mu := s.lock(site.Host)
	mu.Lock()
	defer mu.Unlock()

	// Остаток квоты = общий лимит минус то, что занимают другие сайты пользователя.
	others, err := s.othersUsage(ctx, userID, site.ID)
	if err != nil {
		return nil, err
	}
	limit := s.limits.DiskQuotaBytes - others

	dir := s.siteDir(site.Host)
	suffix := randHex()
	incoming := filepath.Join(dir, "incoming-"+suffix)
	if err := os.MkdirAll(incoming, 0o755); err != nil {
		return nil, err
	}
	// После успешной подмены каталога incoming уже нет — RemoveAll тогда ничего не делает.
	defer func() { _ = os.RemoveAll(incoming) }()

	total, err := extractZip(zr, incoming, limit)
	if err != nil {
		return nil, err
	}

	public := filepath.Join(dir, "public")
	old := filepath.Join(dir, "old-"+suffix)
	hadOld := true
	if err := os.Rename(public, old); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		hadOld = false
	}
	if err := os.Rename(incoming, public); err != nil {
		if hadOld {
			_ = os.Rename(old, public)
		}
		return nil, err
	}
	if hadOld {
		_ = os.RemoveAll(old)
	}

	now := time.Now()
	site.DiskBytes, site.Status, site.DeployedAt = total, "live", &now
	if err := s.db.WithContext(ctx).Model(site).Updates(map[string]any{
		"disk_bytes": total, "status": "live", "deployed_at": now,
	}).Error; err != nil {
		return nil, err
	}
	return site, nil
}

func (s *Service) siteDir(host string) string { return filepath.Join(s.root, host) }

func (s *Service) lock(host string) *sync.Mutex {
	m, _ := s.locks.LoadOrStore(host, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func mapUnique(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return ErrSlugTaken
	}
	return err
}

func randHex() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

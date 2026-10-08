package sites

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"vladhost/internal/apperr"
)

var (
	ErrBackupsOff     = apperr.New(http.StatusConflict, "backups_unavailable", "site backups are not enabled")
	ErrBackupNotFound = apperr.New(http.StatusNotFound, "backup_not_found", "backup not found")
)

// backupKeep — сколько суток хранятся снимки, включая сегодняшний.
const backupKeep = 7

// ConfigureBackups включает резервные копии: снимки лежат в dir. Пустая строка — раздел выключен.
func (s *Service) ConfigureBackups(dir string) { s.backupDir = dir }

// BackupsAvailable сообщает, настроены ли резервные копии.
func (s *Service) BackupsAvailable() bool { return s.backupDir != "" }

// Backup — снимок файлов сайта за календарные сутки (UTC).
type Backup struct {
	ID      int64     `gorm:"primaryKey" json:"id"`
	SiteID  int64     `json:"-"`
	Day     string    `json:"day"` // YYYY-MM-DD по UTC
	Bytes   int64     `json:"bytes"`
	Files   int       `json:"files"`
	TakenAt time.Time `json:"taken_at"`
}

// TableName — миграция называет таблицу site_backups (см. 0007_backups.sql).
func (Backup) TableName() string { return "site_backups" }

func todayUTC() string { return time.Now().UTC().Format("2006-01-02") }

func (s *Service) backupDirFor(siteID int64, day string) string {
	return filepath.Join(s.backupDir, strconv.FormatInt(siteID, 10), day)
}

// ListBackups возвращает снимки сайта от новых к старым.
func (s *Service) ListBackups(ctx context.Context, userID, id int64) ([]Backup, error) {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if s.backupDir == "" {
		return nil, ErrBackupsOff
	}
	var out []Backup
	err = s.db.WithContext(ctx).Where("site_id = ?", site.ID).Order("day DESC").Find(&out).Error
	return out, err
}

// CreateBackup делает снимок сайта сейчас. Снимок за сегодня заменяется.
func (s *Service) CreateBackup(ctx context.Context, userID, id int64) (*Backup, error) {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if s.backupDir == "" {
		return nil, ErrBackupsOff
	}
	mu := s.lock(site.Host)
	mu.Lock()
	defer mu.Unlock()
	return s.snapshotSite(ctx, site)
}

// snapshotSite снимает файлы сайта hardlink-копией (при сбое ссылки — обычной копией).
// Вызывается под замком сайта.
func (s *Service) snapshotSite(ctx context.Context, site *Site) (*Backup, error) {
	day := todayUTC()
	var existing Backup
	err := s.db.WithContext(ctx).Where("site_id = ? AND day = ?", site.ID, day).First(&existing).Error
	if err == nil {
		_ = os.RemoveAll(s.backupDirFor(site.ID, day))
		if err := s.db.WithContext(ctx).Delete(&Backup{}, existing.ID).Error; err != nil {
			return nil, err
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	dir := s.backupDirFor(site.ID, day)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	total, n, err := snapshotTree(s.publicDir(site.Host), dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	b := Backup{SiteID: site.ID, Day: day, Bytes: total, Files: n, TakenAt: time.Now()}
	if err := s.db.WithContext(ctx).Create(&b).Error; err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &b, nil
}

// RestoreBackup возвращает сайт к снимку за день backupID. Файлы копируются в incoming
// и подменяются rename-свапом, как при деплое: посетитель не видит полусобранный сайт.
func (s *Service) RestoreBackup(ctx context.Context, userID, id, backupID int64) (*Site, error) {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if s.backupDir == "" {
		return nil, ErrBackupsOff
	}
	var b Backup
	err = s.db.WithContext(ctx).Where("id = ? AND site_id = ?", backupID, site.ID).First(&b).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBackupNotFound
	}
	if err != nil {
		return nil, err
	}
	src := s.backupDirFor(site.ID, b.Day)
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return nil, ErrBackupNotFound
	}

	mu := s.lock(site.Host)
	mu.Lock()
	defer mu.Unlock()

	others, err := s.othersUsage(ctx, userID, site.ID)
	if err != nil {
		return nil, err
	}
	if s.limits.DiskQuotaBytes-others < b.Bytes {
		return nil, ErrQuota
	}

	dir := s.siteDir(site.Host)
	suffix := randHex()
	incoming := filepath.Join(dir, "incoming-"+suffix)
	if err := os.MkdirAll(incoming, 0o755); err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(incoming) }()

	total, err := copyTree(src, incoming)
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

// WriteBackupZip отдаёт снимок zip-архивом. start — после всех проверок, до первой записи.
func (s *Service) WriteBackupZip(ctx context.Context, userID, id, backupID int64, start func(), w io.Writer) error {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if s.backupDir == "" {
		return ErrBackupsOff
	}
	var b Backup
	err = s.db.WithContext(ctx).Where("id = ? AND site_id = ?", backupID, site.ID).First(&b).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrBackupNotFound
	}
	if err != nil {
		return err
	}
	src := s.backupDirFor(site.ID, b.Day)
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return ErrBackupNotFound
	}
	mu := s.lock(site.Host)
	mu.Lock()
	defer mu.Unlock()
	start()
	return writeZipDir(src, w)
}

// WriteSiteArchive отдаёт текущие файлы сайта zip-архивом. start — после проверок, до первой записи.
func (s *Service) WriteSiteArchive(ctx context.Context, userID, id int64, start func(), w io.Writer) error {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	mu := s.lock(site.Host)
	mu.Lock()
	defer mu.Unlock()
	dir := s.publicDir(site.Host)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	start()
	return writeZipDir(dir, w)
}

// BackupCycle — один проход фоновой задачи: снимок за сегодня для каждого сайта и чистка старых.
// Вызывается из RunBackups и из тестов.
func (s *Service) BackupCycle(ctx context.Context) error {
	if !s.BackupsAvailable() {
		return nil
	}
	var all []Site
	if err := s.db.WithContext(ctx).Find(&all).Error; err != nil {
		return err
	}
	day := todayUTC()
	for i := range all {
		site := &all[i]
		var n int64
		if err := s.db.WithContext(ctx).Model(&Backup{}).
			Where("site_id = ? AND day = ?", site.ID, day).Count(&n).Error; err != nil {
			log.Printf("бэкапы: %s: %v", site.Host, err)
			continue
		}
		if n > 0 {
			continue
		}
		mu := s.lock(site.Host)
		mu.Lock()
		if _, err := s.snapshotSite(ctx, site); err != nil {
			log.Printf("бэкапы: снимок %s: %v", site.Host, err)
		}
		mu.Unlock()
	}
	return s.pruneBackups(ctx)
}

// pruneBackups удаляет снимки старше backupKeep суток (включая сегодняшний).
func (s *Service) pruneBackups(ctx context.Context) error {
	keepFrom := time.Now().UTC().AddDate(0, 0, -(backupKeep - 1)).Format("2006-01-02")
	var old []Backup
	if err := s.db.WithContext(ctx).Where("day < ?", keepFrom).Find(&old).Error; err != nil {
		return err
	}
	for i := range old {
		b := &old[i]
		if err := os.RemoveAll(s.backupDirFor(b.SiteID, b.Day)); err != nil {
			log.Printf("бэкапы: удаление каталога %d/%s: %v", b.SiteID, b.Day, err)
		}
		if err := s.db.WithContext(ctx).Delete(&Backup{}, b.ID).Error; err != nil {
			return err
		}
	}
	return nil
}

// RunBackups раз в interval делает недостающие снимки за сегодня и чистит старые.
func (s *Service) RunBackups(ctx context.Context, interval time.Duration) {
	if !s.BackupsAvailable() {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := s.BackupCycle(ctx); err != nil && ctx.Err() == nil {
			log.Printf("бэкапы: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// snapshotTree hardlink-копирует дерево src в пустой каталог dst.
// Hardlink дёшев для статики; если ссылаться нельзя (другая ФС) — файл копируется.
// Временные файлы файлового менеджера в снимок не попадают.
func snapshotTree(src, dst string) (int64, int, error) {
	var total int64
	var files int
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if strings.HasPrefix(filepath.Base(p), tmpPrefix) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.Link(p, target); err != nil {
			if _, err2 := copyFileContents(p, target); err2 != nil {
				return err2
			}
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		total += info.Size()
		files++
		return nil
	})
	return total, files, err
}

// copyTree копирует дерево src в каталог dst (восстановление): живые файлы не должны
// делить inode со снимком, иначе последующие записи изменили бы и копию, и оригинал.
func copyTree(src, dst string) (int64, error) {
	var total int64
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		n, err := copyFileContents(p, target)
		if err != nil {
			return err
		}
		total += n
		return nil
	})
	return total, err
}

func copyFileContents(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
		return 0, err
	}
	return n, nil
}

// writeZipDir пишет каталог zip-архивом: только обычные файлы, без временных.
func writeZipDir(dir string, w io.Writer) error {
	zw := zip.NewWriter(w)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() || strings.HasPrefix(filepath.Base(p), tmpPrefix) {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: filepath.ToSlash(rel), Method: zip.Deflate})
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, f)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	})
	if err != nil {
		_ = zw.Close()
		return err
	}
	return zw.Close()
}

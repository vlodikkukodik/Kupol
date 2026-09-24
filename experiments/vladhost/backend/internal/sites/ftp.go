package sites

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"math/big"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"vladhost/internal/apperr"
)

var (
	ErrFTPAuth    = apperr.New(http.StatusUnauthorized, "ftp_auth", "invalid FTP login or password")
	ErrFTPRevoked = apperr.New(http.StatusForbidden, "ftp_revoked", "FTP access revoked")
	errBadOffset  = apperr.New(http.StatusUnprocessableEntity, "bad_offset", "offset is beyond the file size")
)

// Алфавит без похожих символов (0/O, 1/l/I): пароль читают глазами и вводят руками.
const passwordAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newFTPPassword() (string, error) {
	out := make([]byte, 20)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(passwordAlphabet))))
		if err != nil {
			return "", err
		}
		out[i] = passwordAlphabet[n.Int64()]
	}
	return string(out), nil
}

// FTPUsername — логин FTP: адрес сайта без общего домена ("blog.john").
func (s *Service) FTPUsername(host string) string { return strings.TrimSuffix(host, "."+s.baseDomain) }

// EnableFTP включает FTP для сайта и выдаёт новый пароль; повторный вызов меняет пароль.
// Пароль хранится только в виде bcrypt и показывается один раз.
func (s *Service) EnableFTP(ctx context.Context, userID, id int64) (*Site, string, error) {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, "", err
	}
	pw, err := newFTPPassword()
	if err != nil {
		return nil, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", err
	}
	if err := s.db.WithContext(ctx).Model(site).
		Updates(map[string]any{"ftp_enabled": true, "ftp_password_hash": string(hash)}).Error; err != nil {
		return nil, "", err
	}
	site.FTPEnabled = true
	s.revokeFTP(site.ID) // открытые сессии со старым паролем закрываются
	return site, pw, nil
}

func (s *Service) DisableFTP(ctx context.Context, userID, id int64) (*Site, error) {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(site).
		Updates(map[string]any{"ftp_enabled": false, "ftp_password_hash": ""}).Error; err != nil {
		return nil, err
	}
	site.FTPEnabled = false
	s.revokeFTP(site.ID)
	return site, nil
}

func (s *Service) revokeFTP(siteID int64) {
	s.ftpMu.Lock()
	s.ftpRevoked[siteID] = time.Now()
	s.ftpMu.Unlock()
}

func (s *Service) revokedSince(siteID int64, t time.Time) bool {
	s.ftpMu.Lock()
	defer s.ftpMu.Unlock()
	r, ok := s.ftpRevoked[siteID]
	return ok && !r.Before(t)
}

var ftpUserRe = regexp.MustCompile(`^[a-z0-9-]+\.[a-z0-9-]+$`)

// dummyFTPHash выравнивает время ответа для несуществующих логинов.
var dummyFTPHash, _ = bcrypt.GenerateFromPassword([]byte("vladhost-ftp-dummy"), bcrypt.DefaultCost)

// FTPLogin проверяет логин и пароль и открывает сессию с доступом только к каталогу этого сайта.
func (s *Service) FTPLogin(ctx context.Context, username, password string) (*FTPSession, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	var site Site
	err := gorm.ErrRecordNotFound
	if ftpUserRe.MatchString(username) {
		err = s.db.WithContext(ctx).Where("host = ? AND ftp_enabled = true", username+"."+s.baseDomain).First(&site).Error
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = bcrypt.CompareHashAndPassword(dummyFTPHash, []byte(password))
			return nil, ErrFTPAuth
		}
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(site.FTPPasswordHash), []byte(password)) != nil {
		return nil, ErrFTPAuth
	}
	usage, err := s.acquireUsage(ctx, &site)
	if err != nil {
		return nil, err
	}
	return &FTPSession{s: s, site: site, usage: usage, since: time.Now()}, nil
}

// --- учёт места ---

// siteUsage — общий счётчик занятого места сайта для всех его FTP-сессий: две параллельные загрузки
// не могут вместе превысить квоту, даже если каждая по отдельности укладывается.
type siteUsage struct {
	mu          sync.Mutex
	used, limit int64
	refs        int
}

func (u *siteUsage) reserve(n int64) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.used+n > u.limit {
		return ErrQuota
	}
	u.used += n
	return nil
}

func (u *siteUsage) free(n int64) {
	u.mu.Lock()
	u.used = max(0, u.used-n)
	u.mu.Unlock()
}

func (s *Service) acquireUsage(ctx context.Context, site *Site) (*siteUsage, error) {
	s.ftpMu.Lock()
	defer s.ftpMu.Unlock()
	if u, ok := s.ftpUsage[site.ID]; ok {
		u.refs++
		return u, nil
	}
	used, err := dirSize(s.publicDir(site.Host))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	others, err := s.othersUsage(ctx, site.UserID, site.ID)
	if err != nil {
		return nil, err
	}
	u := &siteUsage{used: used, limit: max(0, s.limits.DiskQuotaBytes-others), refs: 1}
	s.ftpUsage[site.ID] = u
	return u, nil
}

// releaseUsage: когда закрылась последняя сессия, счётчик пересчитывается по диску (он мог разойтись
// с реальностью из-за параллельных правок через веб) и итог записывается в БД.
func (s *Service) releaseUsage(site *Site) {
	s.ftpMu.Lock()
	u, ok := s.ftpUsage[site.ID]
	if !ok {
		s.ftpMu.Unlock()
		return
	}
	u.refs--
	last := u.refs <= 0
	if last {
		delete(s.ftpUsage, site.ID)
	}
	s.ftpMu.Unlock()
	if last {
		if total, err := dirSize(s.publicDir(site.Host)); err == nil {
			s.db.Model(&Site{}).Where("id = ?", site.ID).Update("disk_bytes", total)
		}
	}
}

// --- сессия ---

// FTPSession — доступ одного FTP-клиента к файлам сайта. Пути — относительные, уже очищенные CleanPath.
// Каталог public открывается заново на каждую операцию: после деплоя (он подменяет каталог) сессия
// работает с новой версией сайта.
type FTPSession struct {
	s     *Service
	site  Site
	usage *siteUsage
	since time.Time
	once  sync.Once
}

func (f *FTPSession) Site() Site { return f.site }

func (f *FTPSession) Close() { f.once.Do(func() { f.s.releaseUsage(&f.site) }) }

func (f *FTPSession) withRoot(fn func(*os.Root) error) error {
	if f.s.revokedSince(f.site.ID, f.since) {
		return ErrFTPRevoked
	}
	dir := f.s.publicDir(f.site.Host)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return translate(fn(root))
}

func (f *FTPSession) Stat(rel string) (fs.FileInfo, error) {
	var fi fs.FileInfo
	err := f.withRoot(func(r *os.Root) (err error) { fi, err = r.Lstat(rel); return })
	return fi, err
}

func (f *FTPSession) ReadDir(rel string) ([]fs.FileInfo, error) {
	var out []fs.FileInfo
	err := f.withRoot(func(r *os.Root) error {
		d, err := r.Open(rel)
		if err != nil {
			return err
		}
		defer func() { _ = d.Close() }()
		entries, err := d.ReadDir(-1)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), tmpPrefix) {
				continue
			}
			if info, err := e.Info(); err == nil {
				out = append(out, info)
			}
		}
		slices.SortFunc(out, func(a, b fs.FileInfo) int { return strings.Compare(a.Name(), b.Name()) })
		return nil
	})
	return out, err
}

func (f *FTPSession) Mkdir(rel string) error {
	return f.withRoot(func(r *os.Root) error { return r.Mkdir(rel, 0o755) })
}

// Remove удаляет файл и возвращает его размер в квоту.
func (f *FTPSession) Remove(rel string) error {
	return f.withRoot(func(r *os.Root) error {
		fi, err := r.Lstat(rel)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return ErrIsDir
		}
		if err := r.Remove(rel); err != nil {
			return err
		}
		f.usage.free(fi.Size())
		return nil
	})
}

// RemoveDir удаляет пустую папку (как требует RFC 959; рекурсивно клиенты чистят сами).
func (f *FTPSession) RemoveDir(rel string) error {
	return f.withRoot(func(r *os.Root) error {
		fi, err := r.Lstat(rel)
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return ErrNotDir
		}
		return r.Remove(rel)
	})
}

func (f *FTPSession) Rename(from, to string) error {
	return f.withRoot(func(r *os.Root) error {
		src, err := r.Lstat(from)
		if err != nil {
			return err
		}
		var replaced int64
		if dst, err := r.Lstat(to); err == nil {
			if dst.IsDir() || src.IsDir() {
				return ErrExists // поверх папки и папкой поверх файла не переименовываем
			}
			replaced = dst.Size()
		}
		if err := r.Rename(from, to); err != nil {
			return err
		}
		f.usage.free(replaced)
		return nil
	})
}

// Handle — то, что библиотека FTP читает и пишет при передаче файла.
type Handle interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
}

func (f *FTPSession) OpenRead(rel string, offset int64) (Handle, error) {
	var h Handle
	err := f.withRoot(func(r *os.Root) error {
		file, err := r.Open(rel)
		if err != nil {
			return err
		}
		fi, err := file.Stat()
		if err == nil && !fi.Mode().IsRegular() {
			err = ErrIsDir
		}
		if err == nil && offset > 0 {
			_, err = file.Seek(offset, io.SeekStart)
		}
		if err != nil {
			_ = file.Close()
			return err
		}
		h = readHandle{file}
		return nil
	})
	return h, err
}

// OpenWrite открывает файл на запись. Без offset файл создаётся заново (STOR), с O_APPEND дописывается (APPE),
// с offset > 0 продолжается загрузка с этого места (REST). Каждый записанный байт сверх текущего размера
// резервируется в квоте до записи.
func (f *FTPSession) OpenWrite(rel string, flags int, offset int64) (Handle, error) {
	var h Handle
	err := f.withRoot(func(r *os.Root) error {
		var oldSize int64
		if fi, err := r.Lstat(rel); err == nil {
			if !fi.Mode().IsRegular() {
				return ErrIsDir
			}
			oldSize = fi.Size()
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		appendMode := flags&os.O_APPEND != 0
		openFlags := os.O_WRONLY | os.O_CREATE
		switch {
		case appendMode:
			openFlags |= os.O_APPEND
		case offset == 0:
			openFlags |= os.O_TRUNC
		case offset > oldSize:
			return errBadOffset
		}
		file, err := r.OpenFile(rel, openFlags, 0o644)
		if err != nil {
			return err
		}
		size := oldSize
		if openFlags&os.O_TRUNC != 0 {
			f.usage.free(oldSize)
			size = 0
		}
		pos := offset
		if appendMode {
			pos = size
		} else if offset > 0 {
			if _, err := file.Seek(offset, io.SeekStart); err != nil {
				_ = file.Close()
				return err
			}
		}
		h = &writeHandle{f: f, file: file, size: size, pos: pos, appendMode: appendMode}
		return nil
	})
	return h, err
}

type readHandle struct{ *os.File }

func (readHandle) Write([]byte) (int, error) { return 0, fs.ErrPermission }

type writeHandle struct {
	f          *FTPSession
	file       *os.File
	size, pos  int64
	appendMode bool
}

func (w *writeHandle) Read([]byte) (int, error) { return 0, fs.ErrPermission }

func (w *writeHandle) Write(p []byte) (int, error) {
	if w.f.s.revokedSince(w.f.site.ID, w.f.since) {
		return 0, ErrFTPRevoked
	}
	if w.appendMode {
		w.pos = w.size
	}
	grow := max(0, w.pos+int64(len(p))-w.size)
	if grow > 0 {
		if err := w.f.usage.reserve(grow); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	// Записалось меньше, чем зарезервировано, — лишнее возвращаем в квоту.
	written := max(0, w.pos+int64(n)-w.size)
	if grow > written {
		w.f.usage.free(grow - written)
	}
	w.pos += int64(n)
	w.size += written
	return n, err
}

func (w *writeHandle) Seek(offset int64, whence int) (int64, error) {
	pos, err := w.file.Seek(offset, whence)
	if err == nil {
		w.pos = pos
	}
	return pos, err
}

func (w *writeHandle) Close() error { return w.file.Close() }

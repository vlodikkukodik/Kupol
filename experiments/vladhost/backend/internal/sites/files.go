package sites

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

var (
	ErrBadPath      = errors.New("недопустимый путь")
	ErrFileNotFound = errors.New("файл или папка не найдены")
	ErrExists       = errors.New("такой файл или папка уже есть")
	ErrIsDir        = errors.New("это папка, а нужен файл")
	ErrNotDir       = errors.New("это файл, а нужна папка")
	ErrTooLarge     = errors.New("файл слишком большой для редактора")
	ErrNotText      = errors.New("это не текстовый файл, редактировать нельзя")
)

// MaxEditBytes — предел размера файла, который открывается и сохраняется через редактор.
const MaxEditBytes = 2 << 20

const tmpPrefix = ".vh-tmp-"

type FileEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// CleanPath приводит путь внутри сайта к виду "a/b" (корень — "."). Всё, что может выйти
// за пределы public, отклоняется; от симлинков защищает os.Root.
func CleanPath(p string) (string, error) {
	if strings.ContainsAny(p, "\\\x00") || strings.HasPrefix(p, "/") || len(p) > 1024 {
		return "", ErrBadPath
	}
	segs := strings.Split(p, "/")
	if slices.Contains(segs, "..") {
		return "", ErrBadPath
	}
	for _, s := range segs {
		if len(s) > 255 || strings.HasPrefix(s, tmpPrefix) {
			return "", ErrBadPath
		}
	}
	return path.Clean("/" + p)[1:], nil
}

func cleanOrRoot(p string) (string, error) {
	c, err := CleanPath(p)
	if err != nil {
		return "", err
	}
	if c == "" {
		return ".", nil
	}
	return c, nil
}

func (s *Service) publicDir(host string) string { return filepath.Join(s.siteDir(host), "public") }

// run выполняет операцию над файлами сайта под замком сайта. Для изменяющих операций
// после успеха пересчитывает занятое место.
func (s *Service) run(ctx context.Context, userID, id int64, write bool, fn func(site *Site, root *os.Root, others int64) error) error {
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
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	others, err := s.othersUsage(ctx, userID, site.ID)
	if err != nil {
		return err
	}
	if err := fn(site, root, others); err != nil {
		return translate(err)
	}
	if !write {
		return nil
	}
	total, err := dirSize(dir)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(site).Update("disk_bytes", total).Error
}

func (s *Service) ListDir(ctx context.Context, userID, id int64, rel string) ([]FileEntry, error) {
	rel, err := cleanOrRoot(rel)
	if err != nil {
		return nil, err
	}
	var out []FileEntry
	err = s.run(ctx, userID, id, false, func(_ *Site, root *os.Root, _ int64) error {
		f, err := root.Open(rel)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if fi, err := f.Stat(); err != nil {
			return err
		} else if !fi.IsDir() {
			return ErrNotDir
		}
		entries, err := f.ReadDir(-1)
		if err != nil {
			return err
		}
		out = make([]FileEntry, 0, len(entries))
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), tmpPrefix) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue // файл исчез между чтением каталога и stat
			}
			out = append(out, FileEntry{Name: e.Name(), IsDir: e.IsDir(), Size: info.Size(), ModTime: info.ModTime()})
		}
		slices.SortFunc(out, func(a, b FileEntry) int {
			if a.IsDir != b.IsDir {
				if a.IsDir {
					return -1
				}
				return 1
			}
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		})
		return nil
	})
	return out, err
}

func (s *Service) ReadFile(ctx context.Context, userID, id int64, rel string) ([]byte, error) {
	rel, err := CleanPath(rel)
	if err != nil || rel == "" {
		return nil, ErrBadPath
	}
	var data []byte
	err = s.run(ctx, userID, id, false, func(_ *Site, root *os.Root, _ int64) error {
		f, err := root.Open(rel)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		fi, err := f.Stat()
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return ErrIsDir
		}
		if !fi.Mode().IsRegular() {
			return ErrBadPath
		}
		if fi.Size() > MaxEditBytes {
			return ErrTooLarge
		}
		data, err = io.ReadAll(io.LimitReader(f, MaxEditBytes+1))
		if err != nil {
			return err
		}
		if len(data) > MaxEditBytes {
			return ErrTooLarge
		}
		if slices.Contains(data, 0) || !utf8.Valid(data) {
			return ErrNotText
		}
		return nil
	})
	return data, err
}

// WriteFile создаёт или заменяет файл (недостающие папки создаются). Запись идёт во временный
// файл и подменяется rename — читатели не увидят половину файла. sizeCap > 0 ограничивает размер
// (для редактора); квота диска действует всегда.
func (s *Service) WriteFile(ctx context.Context, userID, id int64, rel string, r io.Reader, sizeCap int64) error {
	rel, err := CleanPath(rel)
	if err != nil || rel == "" {
		return ErrBadPath
	}
	return s.run(ctx, userID, id, true, func(site *Site, root *os.Root, others int64) error {
		used, err := dirSize(s.publicDir(site.Host))
		if err != nil {
			return err
		}
		var old int64
		if fi, err := root.Lstat(rel); err == nil {
			if fi.IsDir() {
				return ErrIsDir
			}
			if !fi.Mode().IsRegular() {
				return ErrBadPath
			}
			old = fi.Size()
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		lim := max(s.limits.DiskQuotaBytes-others-used+old, 0)
		capped := sizeCap > 0 && sizeCap <= lim
		if capped {
			lim = sizeCap
		}

		dir := path.Dir(rel)
		if dir != "." {
			if err := root.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		tmp := path.Join(dir, tmpPrefix+randHex())
		f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		// +1 байт — чтобы отличить «ровно лимит» от «больше лимита».
		n, err := io.Copy(f, io.LimitReader(r, lim+1))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil && n > lim {
			err = ErrQuota
			if capped {
				err = ErrTooLarge
			}
		}
		if err == nil {
			err = root.Rename(tmp, rel)
		}
		if err != nil {
			_ = root.Remove(tmp)
		}
		return err
	})
}

func (s *Service) Mkdir(ctx context.Context, userID, id int64, rel string) error {
	rel, err := CleanPath(rel)
	if err != nil || rel == "" {
		return ErrBadPath
	}
	return s.run(ctx, userID, id, true, func(_ *Site, root *os.Root, _ int64) error {
		if _, err := root.Lstat(rel); err == nil {
			return ErrExists
		}
		return root.MkdirAll(rel, 0o755)
	})
}

func (s *Service) Remove(ctx context.Context, userID, id int64, rel string) error {
	rel, err := CleanPath(rel)
	if err != nil || rel == "" {
		return ErrBadPath // корень сайта удалить нельзя
	}
	return s.run(ctx, userID, id, true, func(_ *Site, root *os.Root, _ int64) error {
		if _, err := root.Lstat(rel); err != nil {
			return err
		}
		return root.RemoveAll(rel)
	})
}

func (s *Service) Rename(ctx context.Context, userID, id int64, from, to string) error {
	from, err1 := CleanPath(from)
	to, err2 := CleanPath(to)
	if err1 != nil || err2 != nil || from == "" || to == "" || to == from || strings.HasPrefix(to, from+"/") {
		return ErrBadPath
	}
	return s.run(ctx, userID, id, true, func(_ *Site, root *os.Root, _ int64) error {
		if _, err := root.Lstat(from); err != nil {
			return err
		}
		if _, err := root.Lstat(to); err == nil {
			return ErrExists
		}
		if dir := path.Dir(to); dir != "." {
			if err := root.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		return root.Rename(from, to)
	})
}

func (s *Service) othersUsage(ctx context.Context, userID, siteID int64) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Site{}).Where("user_id = ? AND id <> ?", userID, siteID).
		Select("COALESCE(SUM(disk_bytes), 0)").Scan(&n).Error
	return n, err
}

// dirSize считает размер обычных файлов; симлинки не разыменовываются.
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && !strings.HasPrefix(d.Name(), tmpPrefix) {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func translate(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ErrFileNotFound
	case errors.Is(err, fs.ErrExist):
		return ErrExists
	case errors.Is(err, syscall.ENOTDIR):
		return ErrNotDir
	case errors.Is(err, syscall.EISDIR):
		return ErrIsDir
	}
	return err
}

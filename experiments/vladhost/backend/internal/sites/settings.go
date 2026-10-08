package sites

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"vladhost/internal/apperr"
	"vladhost/internal/sitecfg"
)

var (
	ErrSettingsRootDir   = apperr.Validation("root_dir", "settings_root_dir", "invalid document root")
	ErrSettingsIndex     = apperr.Validation("index", "settings_index", "invalid index file name")
	ErrSettingsIndexMany = apperr.Validation("index", "settings_index_many", "too many index files").With(sitecfg.MaxIndex)
	ErrSettingsStatus    = apperr.Validation("error_pages", "settings_error_status", "unsupported response code")
	ErrSettingsErrorPath = apperr.Validation("error_pages", "settings_error_path", "invalid error page path")
	ErrSettingsErrorFile = apperr.Validation("error_pages", "settings_error_file", "error page file not found")
	ErrSettingsWWW       = apperr.Validation("www", "settings_www", "invalid www mode")
)

// problemError переводит причину отказа sitecfg в ошибку с кодом для интерфейса.
func problemError(p sitecfg.Problem) error {
	switch p.Field {
	case "root_dir":
		return ErrSettingsRootDir
	case "index":
		if p.Code == "too_many" {
			return ErrSettingsIndexMany
		}
		return ErrSettingsIndex
	case "error_pages":
		if p.Code == "status" {
			return ErrSettingsStatus
		}
		return ErrSettingsErrorPath.With(p.Arg)
	case "www":
		return ErrSettingsWWW
	}
	return p
}

// Settings возвращает настройки сайта (значения по умолчанию, если их ещё не меняли).
func (s *Service) Settings(ctx context.Context, userID, id int64) (sitecfg.Settings, error) {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return sitecfg.Settings{}, err
	}
	return sitecfg.Read(s.siteDir(site.Host))
}

// UpdateSettings проверяет и сохраняет настройки сайта целиком. Корневая папка создаётся, если её нет; файлы страниц
// ошибок обязаны существовать в корне сайта.
func (s *Service) UpdateSettings(ctx context.Context, userID, id int64, in sitecfg.Settings) (sitecfg.Settings, error) {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return sitecfg.Settings{}, err
	}
	set, err := sitecfg.Normalize(in)
	if err != nil {
		var p sitecfg.Problem
		if errors.As(err, &p) {
			return sitecfg.Settings{}, problemError(p)
		}
		return sitecfg.Settings{}, err
	}
	if err := s.ensureSiteDir(site, set.RootDir); err != nil {
		if errors.Is(err, errBadDir) {
			return sitecfg.Settings{}, ErrSettingsRootDir
		}
		return sitecfg.Settings{}, err
	}
	if len(set.ErrorPages) > 0 {
		if err := s.checkErrorPages(site, set); err != nil {
			return sitecfg.Settings{}, err
		}
	}
	mu := s.lock(site.Host)
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(s.siteDir(site.Host), 0o755); err != nil {
		return sitecfg.Settings{}, err
	}
	if err := sitecfg.Write(s.siteDir(site.Host), set); err != nil {
		return sitecfg.Settings{}, err
	}
	return set, nil
}

// checkErrorPages: каждая страница ошибки — существующий обычный файл внутри корня сайта (через os.Root: ссылки наружу не пройдут).
func (s *Service) checkErrorPages(site *Site, set sitecfg.Settings) error {
	pub := s.publicDir(site.Host)
	root, err := os.OpenRoot(pub)
	if err != nil {
		return ErrSettingsErrorFile.With(firstStatus(set))
	}
	defer func() { _ = root.Close() }()
	base := root
	if set.RootDir != "" {
		sub, err := root.OpenRoot(set.RootDir)
		if err != nil {
			return ErrSettingsRootDir
		}
		defer func() { _ = sub.Close() }()
		base = sub
	}
	for _, code := range sitecfg.ErrorStatuses { // порядок кодов стабилен: одна и та же ошибка при одних и тех же данных
		file, ok := set.ErrorPages[strconv.Itoa(code)]
		if !ok {
			continue
		}
		fi, err := base.Lstat(filepath.ToSlash(file))
		if err != nil || !fi.Mode().IsRegular() {
			return ErrSettingsErrorFile.With(code)
		}
	}
	return nil
}

func firstStatus(set sitecfg.Settings) int {
	for _, code := range sitecfg.ErrorStatuses {
		if _, ok := set.ErrorPages[strconv.Itoa(code)]; ok {
			return code
		}
	}
	return 0
}

package sites

import (
	"archive/zip"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"vladhost/internal/apperr"
)

const maxArchiveFiles = 20000

// extractZip распаковывает архив в пустой каталог dst и возвращает размер распакованного.
// Защита: выход за пределы каталога (zip-slip), симлинки и спецфайлы, дубликаты,
// превышение лимита размера — считается по факту чтения, а не по заявленному в заголовке.
func extractZip(zr *zip.Reader, dst string, limit int64) (int64, error) {
	if len(zr.File) > maxArchiveFiles {
		return 0, badArchive("too_many", maxArchiveFiles)
	}
	prefix := commonDir(zr.File)
	var total int64
	hasIndex := false

	for _, f := range zr.File {
		name := strings.TrimPrefix(f.Name, prefix)
		if name == "" || skipEntry(f.Name) {
			continue
		}
		if err := checkName(name); err != nil {
			return 0, err
		}
		target := filepath.Join(dst, filepath.FromSlash(name))
		if !strings.HasPrefix(target, dst+string(filepath.Separator)) {
			return 0, badArchive("bad_path", f.Name)
		}
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return 0, err
			}
		case mode.IsRegular():
			n, err := writeEntry(f, target, limit-total)
			if err != nil {
				return 0, err
			}
			total += n
			if name == "index.html" || name == "index.php" { // сайт на PHP начинается с index.php
				hasIndex = true
			}
		default:
			return 0, badArchive("special_file", f.Name)
		}
	}
	if !hasIndex {
		return 0, badArchive("no_index")
	}
	return total, nil
}

func writeEntry(f *zip.File, target string, remaining int64) (int64, error) {
	if remaining < 0 {
		return 0, ErrQuota
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, err
	}
	rc, err := f.Open()
	if err != nil {
		return 0, badArchive("unreadable", f.Name)
	}
	defer func() { _ = rc.Close() }()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return 0, badArchive("duplicate", f.Name)
		}
		return 0, err
	}
	// +1 байт нужен, чтобы отличить «ровно лимит» от «больше лимита».
	n, err := io.Copy(out, io.LimitReader(rc, remaining+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, badArchive("corrupt", f.Name)
	}
	if n > remaining {
		return 0, ErrQuota
	}
	return n, nil
}

func checkName(name string) error {
	if strings.ContainsAny(name, "\\\x00") || path.IsAbs(name) {
		return badArchive("bad_path", name)
	}
	if slices.Contains(strings.Split(name, "/"), "..") {
		return badArchive("bad_path", name)
	}
	return nil
}

// Служебный мусор, который macOS кладёт в архивы.
func skipEntry(name string) bool {
	return strings.HasPrefix(name, "__MACOSX/") || path.Base(name) == ".DS_Store"
}

// commonDir возвращает "dir/", если весь архив лежит в одной папке (типичный «zip папки»),
// иначе пустую строку.
func commonDir(files []*zip.File) string {
	first := ""
	for _, f := range files {
		if skipEntry(f.Name) {
			continue
		}
		seg, rest, hasSlash := strings.Cut(f.Name, "/")
		if !hasSlash || seg == "" || (rest == "" && !f.FileInfo().IsDir()) {
			return ""
		}
		if first == "" {
			first = seg
		} else if seg != first {
			return ""
		}
	}
	if first == "" {
		return ""
	}
	return first + "/"
}

// badArchive — архив пользователя непригоден. Код — ключ каталога "err.archive.<code>", args подставляются в текст.
func badArchive(code string, args ...any) error {
	return apperr.New(http.StatusUnprocessableEntity, "archive."+code, "bad archive: "+code).With(args...)
}

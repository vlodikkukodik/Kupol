// Package sitecfg — настройки сайта, которые задаются в панели, а применяет веб-шлюз: корневая папка, индексные файлы,
// листинг каталогов, страницы ошибок, редирект www и HSTS. Хранятся файлом settings.json рядом с папкой public
// ({каталог сайтов}/{адрес сайта}/settings.json): шлюз работает без БД и читает только файлы.
// Файл пишет панель, а шлюз ему не доверяет: всё прочитанное проходит Sanitize.
package sitecfg

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	FileName = "settings.json"

	MaxDirLen   = 200 // длина папки и пути к файлу
	MaxDirDepth = 8
	MaxIndex    = 5 // индексных файлов
	maxFileSize = 64 << 10

	WWWNone   = ""       // адреса не переписываются
	WWWAdd    = "add"    // example.com → www.example.com
	WWWRemove = "remove" // www.example.com → example.com
)

// ErrorStatuses — коды, для которых можно задать свою страницу (для них у шлюза есть страницы по умолчанию).
var ErrorStatuses = []int{400, 401, 403, 404, 405, 410, 429, 500, 503}

// DefaultIndex — индексные файлы, когда свои не заданы.
var DefaultIndex = []string{"index.html", "index.htm"}

// Settings — настройки сайта. Нулевое значение — поведение по умолчанию.
type Settings struct {
	// RootDir — папка внутри public, которая служит корнем сайта (например, dist после сборки). Пусто — сам public.
	RootDir string `json:"root_dir"`
	// Index — индексные файлы каталога по порядку. Пусто — DefaultIndex.
	Index []string `json:"index"`
	// Autoindex — показывать список файлов каталога, где нет индексного файла. Директива Options в .htaccess сильнее.
	Autoindex bool `json:"autoindex"`
	// ErrorPages — код ответа → файл внутри корня сайта. ErrorDocument в .htaccess сильнее.
	ErrorPages map[string]string `json:"error_pages"`
	// WWW — редирект между адресом с www и без него (работает, если второй адрес подключён к этому же сайту).
	WWW string `json:"www"`
	// HSTS — добавлять заголовок Strict-Transport-Security (браузеры запомнят, что сайт только по HTTPS).
	HSTS bool `json:"hsts"`
}

// Problem — причина отказа при проверке: поле и код (по ним панель подбирает сообщение).
type Problem struct {
	Field string
	Code  string
	Arg   int // код ответа для страницы ошибки
}

func (p Problem) Error() string { return p.Field + ": " + p.Code }

// CleanDir приводит папку внутри сайта к виду "a/b" (пусто — корень). Только вниз от корня: «..», абсолютные пути,
// обратные слэши, скрытые части (.git, .ssh) и глубина больше MaxDirDepth не допускаются.
func CleanDir(dir string) (string, bool) {
	dir = strings.Trim(strings.TrimSpace(dir), "/")
	if dir == "" {
		return "", true
	}
	if len(dir) > MaxDirLen || strings.ContainsAny(dir, "\\\x00") {
		return "", false
	}
	segs := strings.Split(dir, "/")
	if slices.Contains(segs, "..") {
		return "", false
	}
	c := path.Clean("/" + dir)[1:]
	if c == "" {
		return "", true
	}
	segs = strings.Split(c, "/")
	if len(segs) > MaxDirDepth {
		return "", false
	}
	for _, s := range segs {
		if len(s) > 255 || strings.HasPrefix(s, ".") {
			return "", false
		}
	}
	return c, true
}

// CleanFile приводит путь к файлу внутри сайта к виду "a/b.html". Те же правила, что у CleanDir, плюс путь не пуст.
func CleanFile(p string) (string, bool) {
	c, ok := CleanDir(p)
	if !ok || c == "" || strings.HasSuffix(strings.TrimSpace(p), "/") {
		return "", false
	}
	return c, true
}

var indexNameRe = regexp.MustCompile(`^[^/\\\x00\x01-\x1f]{1,100}$`)

// ValidIndexName: имя файла без путей и скрытых имён.
func ValidIndexName(n string) bool {
	return indexNameRe.MatchString(n) && !strings.HasPrefix(n, ".") && n != ".." && strings.TrimSpace(n) == n
}

func validStatus(code string) (int, bool) {
	n, err := strconv.Atoi(code)
	if err != nil || strconv.Itoa(n) != code || !slices.Contains(ErrorStatuses, n) {
		return 0, false
	}
	return n, true
}

// Normalize строго проверяет настройки, которые прислал пользователь, и приводит их к каноническому виду.
// Первая найденная проблема возвращается как *Problem. Существование папок и файлов проверяет вызывающий код.
func Normalize(s Settings) (Settings, error) {
	var out Settings
	dir, ok := CleanDir(s.RootDir)
	if !ok {
		return out, Problem{Field: "root_dir", Code: "invalid"}
	}
	out.RootDir = dir

	if len(s.Index) > MaxIndex {
		return out, Problem{Field: "index", Code: "too_many", Arg: MaxIndex}
	}
	seen := map[string]bool{}
	for _, n := range s.Index {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if !ValidIndexName(n) {
			return out, Problem{Field: "index", Code: "invalid"}
		}
		if !seen[n] {
			seen[n] = true
			out.Index = append(out.Index, n)
		}
	}
	out.Autoindex = s.Autoindex

	for code, file := range s.ErrorPages {
		st, ok := validStatus(code)
		if !ok {
			return out, Problem{Field: "error_pages", Code: "status"}
		}
		if strings.TrimSpace(file) == "" {
			continue
		}
		c, ok := CleanFile(file)
		if !ok || strings.HasPrefix(path.Base(c), ".ht") {
			return out, Problem{Field: "error_pages", Code: "invalid", Arg: st}
		}
		if out.ErrorPages == nil {
			out.ErrorPages = map[string]string{}
		}
		out.ErrorPages[code] = c
	}

	switch s.WWW {
	case WWWNone, WWWAdd, WWWRemove:
		out.WWW = s.WWW
	default:
		return out, Problem{Field: "www", Code: "invalid"}
	}
	out.HSTS = s.HSTS
	return out, nil
}

// Sanitize оставляет из прочитанного файла только допустимое: недопустимые значения заменяются умолчаниями.
// Шлюз не должен ломаться и открывать лишнее из-за испорченного файла.
func Sanitize(s Settings) Settings {
	var out Settings
	if dir, ok := CleanDir(s.RootDir); ok {
		out.RootDir = dir
	}
	for _, n := range s.Index {
		if ValidIndexName(n) && len(out.Index) < MaxIndex {
			out.Index = append(out.Index, n)
		}
	}
	out.Autoindex = s.Autoindex
	for code, file := range s.ErrorPages {
		if _, ok := validStatus(code); !ok {
			continue
		}
		if c, ok := CleanFile(file); ok && !strings.HasPrefix(path.Base(c), ".ht") {
			if out.ErrorPages == nil {
				out.ErrorPages = map[string]string{}
			}
			out.ErrorPages[code] = c
		}
	}
	if s.WWW == WWWAdd || s.WWW == WWWRemove {
		out.WWW = s.WWW
	}
	out.HSTS = s.HSTS
	return out
}

// IndexFiles — индексные файлы с учётом умолчания.
func (s Settings) IndexFiles() []string {
	if len(s.Index) == 0 {
		return DefaultIndex
	}
	return s.Index
}

// Parse разбирает содержимое settings.json (и санитизирует). Пустой или испорченный файл — настройки по умолчанию.
func Parse(data []byte) Settings {
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}
	}
	return Sanitize(s)
}

// Read читает настройки сайта из его каталога ({siteDir}/settings.json). Нет файла — значения по умолчанию.
func Read(siteDir string) (Settings, error) {
	f, err := os.Open(filepath.Join(siteDir, FileName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Settings{}, nil
		}
		return Settings{}, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize))
	if err != nil {
		return Settings{}, err
	}
	return Parse(data), nil
}

// Write сохраняет настройки атомарно: шлюз читает файл целиком или старую версию, но не половину.
func Write(siteDir string, s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(siteDir, ".settings-*")
	if err != nil {
		return err
	}
	_, werr := io.Copy(tmp, bytes.NewReader(append(data, '\n')))
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp.Name(), 0o644) // читает веб-шлюз под другим пользователем
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
		return werr
	}
	if err := os.Rename(tmp.Name(), filepath.Join(siteDir, FileName)); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

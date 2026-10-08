// Package runtimecfg — среда выполнения сайта: статика, PHP, Node.js или Python. Хранится файлом runtime.json рядом с папкой public
// ({каталог сайтов}/{адрес сайта}/runtime.json), как и settings.json: его пишет панель, а веб-шлюз (работает без БД) читает.
// Шлюз файлу не доверяет: всё прочитанное проходит Sanitize, а пути и порты собираются из проверенных чисел, а не берутся из файла.
package runtimecfg

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// FileName — файл настроек среды рядом с public.
const FileName = "runtime.json"

// Среды выполнения.
const (
	Static = "static"
	PHP    = "php"
	Node   = "node"
	Python = "python"
)

// Runtimes — все допустимые значения.
var Runtimes = []string{Static, PHP, Node, Python}

// Границы порта приложения на 127.0.0.1. Диапазон отделён от системных и временных портов; на весь диапазон стоит правило брандмауэра
// для пользователей приложений (deploy/prepare-runtime.sh).
const (
	PortMin = 20000
	PortMax = 29999
)

// MaxCommand — длина команды запуска приложения.
const MaxCommand = 500

const maxFileSize = 16 << 10

// Config — настройки среды сайта.
type Config struct {
	Runtime string `json:"runtime"`
	// ID — номер сайта: по нему называются системный пользователь, пул PHP-FPM и служба приложения (vhs{ID}).
	ID int64 `json:"id"`
	// Version — версия PHP (8.2, 8.3, 8.4); для остальных сред пусто.
	Version string `json:"version"`
	// Port — порт приложения на 127.0.0.1 (Node.js, Python).
	Port int `json:"port"`
	// Command — команда запуска приложения.
	Command string `json:"command"`
}

// Problem — причина отказа при проверке.
type Problem struct {
	Field string
	Code  string
}

func (p Problem) Error() string { return p.Field + ": " + p.Code }

// ValidCommand: одна строка без управляющих символов. Команда выполняется в песочнице от имени отдельного пользователя сайта.
func ValidCommand(cmd string) bool {
	if cmd == "" || len(cmd) > MaxCommand || strings.TrimSpace(cmd) != cmd {
		return false
	}
	for _, r := range cmd {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// Validate проверяет настройки целиком. versions — версии PHP, установленные на сервере.
func (c Config) Validate(versions []string) *Problem {
	switch c.Runtime {
	case Static:
		return nil
	case PHP:
		if !slices.Contains(versions, c.Version) {
			return &Problem{"version", "version"}
		}
	case Node, Python:
		if !ValidCommand(c.Command) {
			return &Problem{"command", "command"}
		}
		if c.Port < PortMin || c.Port > PortMax {
			return &Problem{"port", "port"}
		}
	default:
		return &Problem{"runtime", "runtime"}
	}
	if c.ID <= 0 {
		return &Problem{"id", "id"}
	}
	return nil
}

// Sanitize приводит прочитанное к безопасному виду; всё сомнительное превращает в статику.
func Sanitize(c Config) Config {
	static := Config{Runtime: Static}
	switch c.Runtime {
	case PHP:
		if c.ID <= 0 || !ValidVersion(c.Version) {
			return static
		}
		return Config{Runtime: PHP, ID: c.ID, Version: c.Version}
	case Node, Python:
		if c.ID <= 0 || c.Port < PortMin || c.Port > PortMax {
			return static
		}
		return Config{Runtime: c.Runtime, ID: c.ID, Port: c.Port}
	}
	return static
}

// ValidVersion: версия вида «8.3» (два числа без ведущих нулей); по ней собираются имена пакетов и путей, поэтому шаблон строгий.
func ValidVersion(v string) bool {
	maj, min, ok := strings.Cut(v, ".")
	if !ok {
		return false
	}
	a, e1 := strconv.Atoi(maj)
	b, e2 := strconv.Atoi(min)
	return e1 == nil && e2 == nil && a >= 5 && a < 20 && b >= 0 && b < 100 && v == strconv.Itoa(a)+"."+strconv.Itoa(b)
}

// Socket — путь сокета пула PHP-FPM сайта внутри каталога dir.
func (c Config) Socket(dir string) string {
	return filepath.Join(dir, "vhs"+strconv.FormatInt(c.ID, 10)+".sock")
}

// Load читает настройки сайта; нет файла или он испорчен — статика. Путь открывается через os.Root, ссылку внутри каталога сайта
// шлюз не разыменует.
func Load(siteDir string) Config {
	root, err := os.OpenRoot(siteDir)
	if err != nil {
		return Config{Runtime: Static}
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(FileName)
	if err != nil {
		return Config{Runtime: Static}
	}
	defer func() { _ = f.Close() }()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() || fi.Size() > maxFileSize {
		return Config{Runtime: Static}
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize))
	if err != nil {
		return Config{Runtime: Static}
	}
	var c Config
	if json.Unmarshal(data, &c) != nil {
		return Config{Runtime: Static}
	}
	return Sanitize(c)
}

// Modified возвращает признак изменения файла для кэша шлюза (время и размер); нет файла — нули.
func Modified(siteDir string) (int64, int64) {
	fi, err := os.Lstat(filepath.Join(siteDir, FileName))
	if err != nil {
		return 0, 0
	}
	return fi.ModTime().UnixNano(), fi.Size()
}

// Save пишет настройки атомарно. Для статики файл удаляется.
func Save(siteDir string, c Config) error {
	path := filepath.Join(siteDir, FileName)
	if c.Runtime == "" || c.Runtime == Static {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(siteDir, ".runtime-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

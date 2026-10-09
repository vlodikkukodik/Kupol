package pdf

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed fonts/*.ttf
var fontFS embed.FS

// ErrNoTypst — typst не найден в PATH (или не задан через KUPOL_TYPST).
var ErrNoTypst = errors.New("typst не найден")

// Runner — компилятор Typst: путь к бинарю, распакованные шрифты и песочница во временном каталоге.
type Runner struct {
	bin     string
	fonts   string
	timeout time.Duration
}

var (
	runnerOnce sync.Once
	runnerVal  *Runner
	runnerErr  error
)

// DefaultRunner находит typst один раз на процесс и распаковывает вшитые шрифты.
// Путь к бинарю задаётся переменной KUPOL_TYPST (по умолчанию ищется в PATH).
func DefaultRunner() (*Runner, error) {
	runnerOnce.Do(func() {
		bin := os.Getenv("KUPOL_TYPST")
		if bin == "" {
			bin = "typst"
		}
		path, err := exec.LookPath(bin)
		if err != nil {
			runnerErr = fmt.Errorf("%w: %s (нужен Typst, см. docs/deploy.md)", ErrNoTypst, bin)
			return
		}
		fonts, err := materializeFonts()
		if err != nil {
			runnerErr = err
			return
		}
		runnerVal = &Runner{bin: path, fonts: fonts, timeout: 45 * time.Second}
	})
	return runnerVal, runnerErr
}

// NewRunner — компилятор с явным путём к typst (очередь приходит со своим).
func NewRunner(bin string) (*Runner, error) {
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("%w: %s (нужен Typst, см. docs/deploy.md)", ErrNoTypst, bin)
	}
	fonts, err := materializeFonts()
	if err != nil {
		return nil, err
	}
	return &Runner{bin: path, fonts: fonts, timeout: 45 * time.Second}, nil
}

var (
	fontsOnce sync.Once
	fontsDir  string
	fontsErr  error
)

// materializeFonts распаковывает вшитые TTF во временный каталог: typst берёт шрифты только с диска.
// Каталог живёт до конца процесса (удаляет система при очистке временных файлов).
func materializeFonts() (string, error) {
	fontsOnce.Do(func() {
		dir, err := os.MkdirTemp("", "kupol-pdf-fonts-*")
		if err != nil {
			fontsErr = err
			return
		}
		entries, err := fontFS.ReadDir("fonts")
		if err != nil {
			fontsErr = err
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".ttf") {
				continue
			}
			raw, err := fontFS.ReadFile("fonts/" + e.Name())
			if err != nil {
				fontsErr = err
				return
			}
			if err := os.WriteFile(filepath.Join(dir, e.Name()), raw, 0o600); err != nil {
				fontsErr = err
				return
			}
		}
		fontsDir = dir
	})
	return fontsDir, fontsErr
}

// Compile — typst compile в песочнице: разметка и подставленные изображения живут во временном каталоге,
// читается только он (--root). Шрифты свои (--font-path + --ignore-system-fonts): результат не зависит от системы.
func (r *Runner) Compile(ctx context.Context, src []byte, images map[string]string) ([]byte, error) {
	if r == nil || r.bin == "" {
		return nil, ErrNoTypst
	}
	dir, err := os.MkdirTemp("", "kupol-pdf-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "doc.typ"), src, 0o600); err != nil {
		return nil, err
	}
	for ref, path := range images {
		dst := filepath.Join(dir, filepath.FromSlash(ref))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if err := copyFile(path, dst); err != nil {
			return nil, fmt.Errorf("изображение %s: %w", ref, err)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.bin, "compile",
		"--root", dir,
		"--font-path", r.fonts,
		"--ignore-system-fonts",
		"--diagnostic-format", "short",
		"doc.typ", "out.pdf",
	)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("typst: %s", diagnose(out))
	}
	return os.ReadFile(filepath.Join(dir, "out.pdf"))
}

// diagnose — первые строки диагностики typst (пути временного каталога выкидываем: они не нужны читателю).
func diagnose(out []byte) string {
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "  ") {
			continue
		}
		lines = append(lines, line)
		if len(lines) >= 3 {
			break
		}
	}
	if len(lines) == 0 {
		return "сборка не удалась"
	}
	return strings.Join(lines, "; ")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

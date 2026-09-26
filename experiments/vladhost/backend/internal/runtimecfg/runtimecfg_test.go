package runtimecfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	versions := []string{"8.2", "8.3"}
	good := []Config{
		{Runtime: Static},
		{Runtime: PHP, ID: 1, Version: "8.3"},
		{Runtime: Node, ID: 1, Port: PortMin, Command: "node server.js"},
		{Runtime: Python, ID: 1, Port: PortMax, Command: "gunicorn -b 127.0.0.1:$PORT app:app"},
	}
	for _, c := range good {
		if p := c.Validate(versions); p != nil {
			t.Errorf("%+v должно проходить: %v", c, p)
		}
	}
	bad := map[string]Config{
		"version":  {Runtime: PHP, ID: 1, Version: "7.4"},
		"version2": {Runtime: PHP, ID: 1, Version: "8.4"}, // не установлена на сервере
		"command":  {Runtime: Node, ID: 1, Port: PortMin, Command: ""},
		"command2": {Runtime: Node, ID: 1, Port: PortMin, Command: "node a.js\nrm -rf /"},
		"command3": {Runtime: Node, ID: 1, Port: PortMin, Command: strings.Repeat("a", MaxCommand+1)},
		"command4": {Runtime: Node, ID: 1, Port: PortMin, Command: " node a.js"},
		"port":     {Runtime: Python, ID: 1, Port: PortMin - 1, Command: "x"},
		"port2":    {Runtime: Python, ID: 1, Port: PortMax + 1, Command: "x"},
		"runtime":  {Runtime: "ruby"},
		"id":       {Runtime: PHP, Version: "8.3"},
	}
	for name, c := range bad {
		if c.Validate(versions) == nil {
			t.Errorf("%s: %+v должно отклоняться", name, c)
		}
	}
}

func TestSanitizeDropsDoubtfulValues(t *testing.T) {
	for _, c := range []Config{
		{Runtime: PHP, ID: 1, Version: "../8.3"}, {Runtime: PHP, ID: 1, Version: "8.03"}, {Runtime: PHP, ID: 1, Version: "8"},
		{Runtime: PHP, ID: -1, Version: "8.3"}, {Runtime: Node, ID: 1, Port: 80}, {Runtime: Node, ID: 0, Port: PortMin}, {Runtime: "x"},
	} {
		if got := Sanitize(c); got.Runtime != Static {
			t.Errorf("%+v → %+v", c, got)
		}
	}
	// Команду шлюз не получает вообще: она нужна только исполнителю.
	if got := Sanitize(Config{Runtime: Node, ID: 2, Port: PortMin, Command: "node x.js"}); got.Command != "" || got.Port != PortMin {
		t.Errorf("%+v", got)
	}
}

func TestSaveLoadAndSocket(t *testing.T) {
	dir := t.TempDir()
	if got := Load(dir); got.Runtime != Static {
		t.Fatal("без файла — статика")
	}
	want := Config{Runtime: PHP, ID: 12, Version: "8.4"}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	if got := Load(dir); got != want {
		t.Fatalf("%+v", got)
	}
	if got := want.Socket("/run/vhphp"); got != "/run/vhphp/vhs12.sock" {
		t.Fatal(got)
	}
	if m, s := Modified(dir); m == 0 || s == 0 {
		t.Fatal("нет признака изменения")
	}
	if err := Save(dir, Config{Runtime: Static}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err == nil {
		t.Fatal("для статики файл удаляется")
	}
	if err := Save(dir, Config{Runtime: Static}); err != nil {
		t.Fatal("повторное удаление не ошибка:", err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".runtime-*")); len(left) != 0 {
		t.Fatalf("временные файлы: %v", left)
	}
}

func TestLoadDoesNotFollowSymlinkOrHugeFile(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	secret := filepath.Join(out, "x.json")
	_ = os.WriteFile(secret, []byte(`{"runtime":"php","id":1,"version":"8.3"}`), 0o644)
	if err := os.Symlink(secret, filepath.Join(dir, FileName)); err != nil {
		t.Fatal(err)
	}
	if got := Load(dir); got.Runtime != Static {
		t.Fatalf("ссылка наружу не читается: %+v", got)
	}
	_ = os.Remove(filepath.Join(dir, FileName))
	_ = os.WriteFile(filepath.Join(dir, FileName), []byte(strings.Repeat(" ", maxFileSize+1)), 0o644)
	if got := Load(dir); got.Runtime != Static {
		t.Fatal("огромный файл игнорируется")
	}
}

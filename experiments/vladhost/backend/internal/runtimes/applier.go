package runtimes

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Действия исполнителя (deploy/bin/runtime.sh).
const (
	ActionApply    = "apply"   // создать пользователя, пул PHP-FPM или службу приложения, права на файлы
	ActionStop     = "stop"    // убрать пул или службу (сайт снова статика)
	ActionRestart  = "restart" // перезапустить приложение / перечитать пул
	ActionStatus   = "status"
	ActionLogs     = "logs"
	ActionPerms    = "perms"     // вернуть панели доступ к файлам, созданным приложением
	ActionPurge    = "purge"     // сайт удалён: убрать всё, включая пользователя
	ActionShellOn  = "shell-on"  // включить доступ к оболочке: пользователь сайта, права на файлы, метка для посредника
	ActionShellOff = "shell-off" // выключить доступ к оболочке
)

// Request — заявка исполнителю среды выполнения.
type Request struct {
	Action  string
	Host    string
	ID      int64
	Runtime string
	Version string
	Port    int
	Command string
}

// Result — ответ исполнителя.
type Result struct {
	OK     bool
	Error  string // короткий код причины отказа
	State  string // active, failed, inactive, unknown
	Output string // журнал или подробности ошибки
}

// Applier передаёт заявку исполнителю и ждёт ответ. Реальная реализация — FileApplier: панель без прав ничего не настраивает сама,
// а кладёт заявку файлом; настройку делает скрипт от root (см. deploy/bin/runtime.sh).
type Applier interface {
	Do(ctx context.Context, r Request, timeout time.Duration) (Result, error)
}

// FileApplier обменивается с runtime.sh через папку Dir: queue/ (пишет панель) и results/ (пишет только root).
type FileApplier struct {
	Dir  string
	Poll time.Duration // как часто смотреть на ответ; по умолчанию 200 мс
}

func newRequestID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + hex.EncodeToString(b[:])
}

func (f FileApplier) Do(ctx context.Context, r Request, timeout time.Duration) (Result, error) {
	rid := newRequestID()
	var b strings.Builder
	fmt.Fprintf(&b, "action=%s\nhost=%s\nid=%d\nruntime=%s\nversion=%s\nport=%d\ncmd=%s\n",
		r.Action, r.Host, r.ID, r.Runtime, r.Version, r.Port, base64.StdEncoding.EncodeToString([]byte(r.Command)))
	queue := filepath.Join(f.Dir, "queue")
	tmp := filepath.Join(queue, "."+rid+".tmp")
	if err := os.WriteFile(tmp, []byte(b.String()), 0o640); err != nil {
		return Result{}, err
	}
	// Исполнитель видит только готовые файлы .req: недописанную заявку он не подхватит.
	if err := os.Rename(tmp, filepath.Join(queue, rid+".req")); err != nil {
		_ = os.Remove(tmp)
		return Result{}, err
	}
	poll := f.Poll
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	t := time.NewTicker(poll)
	defer t.Stop()
	results := filepath.Join(f.Dir, "results")
	for {
		if raw, err := os.ReadFile(filepath.Join(results, rid+".res")); err == nil {
			return parseResult(string(raw), filepath.Join(results, rid+".out")), nil
		}
		select {
		case <-ctx.Done():
			_ = os.Remove(filepath.Join(queue, rid+".req"))
			return Result{}, ctx.Err()
		case <-deadline.C:
			_ = os.Remove(filepath.Join(queue, rid+".req"))
			return Result{}, errors.New("runtime helper did not answer in time")
		case <-t.C:
		}
	}
}

const maxOutput = 64 << 10

func parseResult(raw, outPath string) Result {
	var res Result
	for line := range strings.SplitSeq(raw, "\n") {
		k, v, _ := strings.Cut(line, "=")
		switch k {
		case "ok":
			res.OK = v == "1"
		case "error":
			res.Error = v
		case "state":
			res.State = v
		}
	}
	if data, err := os.ReadFile(outPath); err == nil {
		if len(data) > maxOutput {
			data = data[len(data)-maxOutput:]
		}
		res.Output = strings.ToValidUTF8(string(data), "�")
	}
	return res
}

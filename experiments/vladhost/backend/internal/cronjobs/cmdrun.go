package cronjobs

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CmdResult — итог запуска команды.
type CmdResult struct {
	Exit     int
	TimedOut bool
	Output   string
}

// CmdRunner запускает команду в папке сайта. Реальная реализация — FileRunner: панель работает без прав и сама ничего
// исполнять не может, поэтому кладёт заявку файлом, а песочницу (systemd-run под отдельными ограничениями) поднимает
// скрипт от root, который вызывается по появлению файла (deploy/bin/cron-run.sh).
type CmdRunner interface {
	Run(ctx context.Context, runID int64, siteHost, command string, timeout time.Duration) (CmdResult, error)
}

// FileRunner обменивается с cron-run.sh через каталоги: Queue (пишет панель) и Results (пишет только root).
type FileRunner struct {
	Queue, Results string
	Poll           time.Duration // как часто смотреть на результат; по умолчанию 500 мс
}

const maxOutput = 32 << 10

// Run пишет заявку и ждёт результат. Заявка: строки host=, timeout=, cmd= (base64) — команда попадает в файл как данные.
func (r FileRunner) Run(ctx context.Context, runID int64, siteHost, command string, timeout time.Duration) (CmdResult, error) {
	id := strconv.FormatInt(runID, 10)
	body := fmt.Sprintf("host=%s\ntimeout=%d\ncmd=%s\n", siteHost, int(timeout.Seconds()), base64.StdEncoding.EncodeToString([]byte(command)))
	tmp := filepath.Join(r.Queue, "."+id+".tmp")
	if err := os.WriteFile(tmp, []byte(body), 0o640); err != nil {
		return CmdResult{}, err
	}
	// Готовую заявку скрипт видит только под именем .req: недописанный файл он не подхватит.
	if err := os.Rename(tmp, filepath.Join(r.Queue, id+".req")); err != nil {
		_ = os.Remove(tmp)
		return CmdResult{}, err
	}
	poll := r.Poll
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	// Запас сверх таймаута: очередь заявок, запуск песочницы, остановка процессов.
	deadline := time.NewTimer(timeout + 45*time.Second)
	defer deadline.Stop()
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		if raw, err := os.ReadFile(filepath.Join(r.Results, id+".res")); err == nil {
			return r.parse(id, string(raw))
		}
		select {
		case <-ctx.Done():
			_ = os.Remove(filepath.Join(r.Queue, id+".req")) // ещё не начатая заявка отменяется
			return CmdResult{}, ctx.Err()
		case <-deadline.C:
			_ = os.Remove(filepath.Join(r.Queue, id+".req"))
			return CmdResult{}, errors.New("command runner did not answer in time")
		case <-t.C:
		}
	}
}

func (r FileRunner) parse(id, raw string) (CmdResult, error) {
	var res CmdResult
	for _, line := range strings.Split(raw, "\n") {
		k, v, _ := strings.Cut(line, "=")
		switch k {
		case "exit":
			res.Exit, _ = strconv.Atoi(v)
		case "timeout":
			res.TimedOut = v == "1"
		case "error":
			return res, errors.New(v)
		}
	}
	f, err := os.Open(filepath.Join(r.Results, id+".out"))
	if err == nil {
		defer func() { _ = f.Close() }()
		buf := make([]byte, maxOutput)
		n, _ := f.Read(buf)
		res.Output = string(buf[:n])
	}
	return res, nil
}

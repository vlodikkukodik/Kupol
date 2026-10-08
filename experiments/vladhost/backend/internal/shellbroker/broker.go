// Package shellbroker — посредник оболочек. Работает от root (vladhost shell-broker), слушает unix-сокет, к которому может подключиться
// только панель (пользователь vladhost). По запросу запускает оболочку, команду или SFTP-сервер под системным пользователем сайта
// vhs{ID} в песочнице systemd: видна только папка этого сайта, секреты панели и чужие сайты скрыты, память, процессы и время ограничены.
// Посредник ничему из запроса не доверяет: имя сайта и номер сверяются с меткой, которую ставит исполнитель сред при включении доступа.
package shellbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"vladhost/internal/shellproto"
)

// Config — настройки посредника.
type Config struct {
	// Dir — папка исполнителя сред (в ней shell/{id} — метки «доступ включён» с адресом сайта внутри).
	Dir string
	// Sites — корень сайтов: {Sites}/{адрес}/public и tmp.
	Sites string
	// PanelUID — единственный пользователь, которому разрешено подключаться (vladhost). -1 — не проверять (только тесты).
	PanelUID int
	// SystemdRun и Systemctl — пути к командам; в тестах подменяются.
	SystemdRun string
	Systemctl  string
	// UIDBase — uid пользователя сайта = UIDBase + номер сайта (как в runtime.sh).
	UIDBase int
	// PerSite и Total — сколько сеансов одновременно на сайт и на весь сервер.
	PerSite int
	Total   int
	// MaxRuntime — предел жизни сеанса (systemd остановит службу сам).
	MaxRuntime time.Duration
}

// Broker принимает запросы панели.
type Broker struct {
	cfg Config

	mu       sync.Mutex
	sessions map[int64]map[*session]struct{}
	total    int
}

type session struct {
	unit   string
	cancel context.CancelFunc
}

var (
	hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,60}[a-z0-9])?\.[a-z0-9]([a-z0-9-]{0,60}[a-z0-9])?\.[a-z0-9]([a-z0-9.-]{0,120}[a-z0-9])?$`)
	termRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,30}$`)
)

// New создаёт посредника; нулевые настройки заменяются умолчаниями.
func New(cfg Config) *Broker {
	if cfg.SystemdRun == "" {
		cfg.SystemdRun = "systemd-run"
	}
	if cfg.Systemctl == "" {
		cfg.Systemctl = "systemctl"
	}
	if cfg.UIDBase == 0 {
		cfg.UIDBase = 60000
	}
	if cfg.PerSite <= 0 {
		cfg.PerSite = 3
	}
	if cfg.Total <= 0 {
		cfg.Total = 30
	}
	if cfg.MaxRuntime <= 0 {
		cfg.MaxRuntime = 8 * time.Hour
	}
	return &Broker{cfg: cfg, sessions: map[int64]map[*session]struct{}{}}
}

// Serve принимает соединения, пока не закроют listener.
func (b *Broker) Serve(l net.Listener) error {
	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}
		go b.handle(c)
	}
}

// peerUID возвращает uid процесса на другом конце unix-сокета.
func peerUID(c net.Conn) (int, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return -1, errors.New("not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return -1, err
	}
	uid := -1
	var serr error
	if err := raw.Control(func(fd uintptr) {
		cred, e := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		if e != nil {
			serr = e
			return
		}
		uid = int(cred.Uid)
	}); err != nil {
		return -1, err
	}
	return uid, serr
}

func (b *Broker) handle(nc net.Conn) {
	defer func() { _ = nc.Close() }()
	conn := shellproto.NewConn(nc)
	if b.cfg.PanelUID >= 0 {
		if uid, err := peerUID(nc); err != nil || uid != b.cfg.PanelUID {
			log.Printf("shell-broker: отказ: подключился uid=%d (%v)", uid, err)
			_ = conn.Write(shellproto.FrameFail, []byte("forbidden"))
			return
		}
	}
	_ = nc.SetReadDeadline(time.Now().Add(10 * time.Second))
	typ, payload, err := conn.Read()
	if err != nil || typ != shellproto.FrameHeader || len(payload) > 16<<10 {
		_ = conn.Write(shellproto.FrameFail, []byte("bad_request"))
		return
	}
	_ = nc.SetReadDeadline(time.Time{})
	var h shellproto.Header
	if json.Unmarshal(payload, &h) != nil {
		_ = conn.Write(shellproto.FrameFail, []byte("bad_request"))
		return
	}
	if h.Op == shellproto.OpKill {
		n := b.killSite(h.Site)
		_ = conn.WriteJSON(shellproto.FrameExit, shellproto.Exit{Code: n})
		return
	}
	if code := b.check(&h); code != "" {
		_ = conn.Write(shellproto.FrameFail, []byte(code))
		return
	}
	b.run(nc, conn, h)
}

// check проверяет заявку по строгим правилам. Возвращает код отказа или пустую строку.
func (b *Broker) check(h *shellproto.Header) string {
	if h.Op != shellproto.OpShell && h.Op != shellproto.OpExec && h.Op != shellproto.OpSFTP {
		return "bad_op"
	}
	if h.Site < 1 || h.Site > 9999 || !hostRe.MatchString(h.Host) || strings.Contains(h.Host, "..") {
		return "bad_site"
	}
	if h.Op == shellproto.OpExec && (h.Cmd == "" || len(h.Cmd) > shellproto.MaxCommand || strings.ContainsRune(h.Cmd, 0)) {
		return "bad_command"
	}
	if h.Op == shellproto.OpShell {
		h.PTY = true
	}
	if h.PTY {
		if h.Term == "" {
			h.Term = "xterm"
		}
		if !termRe.MatchString(h.Term) {
			return "bad_term"
		}
		h.Cols, h.Rows = clamp(h.Cols, 80), clamp(h.Rows, 24)
	}
	// Доступ включён, только если исполнитель сред поставил метку с этим же адресом.
	mark, err := os.ReadFile(filepath.Join(b.cfg.Dir, "shell", strconv.FormatInt(h.Site, 10)))
	if err != nil || strings.TrimSpace(string(mark)) != h.Host {
		return "disabled"
	}
	root := filepath.Join(b.cfg.Sites, h.Host)
	if fi, err := os.Lstat(filepath.Join(root, "public")); err != nil || !fi.IsDir() {
		return "no_site"
	}
	if fi, err := os.Lstat(filepath.Join(root, "tmp")); err != nil || !fi.IsDir() {
		return "no_site"
	}
	return ""
}

func clamp(v, def int) int {
	if v < 1 || v > 1000 {
		return def
	}
	return v
}

func unitName(site int64) string {
	return fmt.Sprintf("vhshell-%d-%d", site, time.Now().UnixNano()%1_000_000_000)
}

// props — свойства песочницы: то же, что у приложений сайта (см. deploy/bin/runtime.sh), плюс лимиты для интерактивной работы.
func (b *Broker) props(h shellproto.Header, unit string) []string {
	u := "vhs" + strconv.FormatInt(h.Site, 10)
	root := filepath.Join(b.cfg.Sites, h.Host)
	p := []string{
		"User=" + u, "Group=" + u,
		"NoNewPrivileges=yes", "PrivateTmp=yes", "PrivateDevices=yes", "ProtectSystem=strict", "ProtectHome=yes",
		"ProtectKernelTunables=yes", "ProtectKernelModules=yes", "ProtectControlGroups=yes", "ProtectClock=yes", "ProtectHostname=yes",
		"ProtectProc=invisible", "ProcSubset=pid", "RestrictSUIDSGID=yes", "RestrictNamespaces=yes", "LockPersonality=yes", "RestrictRealtime=yes",
		"RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX",
		"InaccessiblePaths=-/etc/vladhost -/var/lib/vladhost -/var/log/vladhost -/opt/vladhost -/opt/vladadminer -/root -/etc/letsencrypt -/etc/postgresql -/etc/mysql -/etc/ssh",
		"TemporaryFileSystem=/data/vladhost:mode=0755,size=1m,nosuid,nodev",
		"BindPaths=" + filepath.Join(root, "public") + ":/data/vladhost/site/public " + filepath.Join(root, "tmp") + ":/data/vladhost/site/tmp",
		"ReadWritePaths=/data/vladhost/site/public /data/vladhost/site/tmp",
		"WorkingDirectory=/data/vladhost/site/public",
		"MemoryMax=512M", "MemorySwapMax=0", "TasksMax=128", "CPUQuota=100%", "LimitNOFILE=1024", "LimitFSIZE=1073741824",
		fmt.Sprintf("RuntimeMaxSec=%d", int(b.cfg.MaxRuntime.Seconds())),
	}
	_ = unit
	return p
}

func (b *Broker) command(h shellproto.Header, unit string) *exec.Cmd {
	u := "vhs" + strconv.FormatInt(h.Site, 10)
	args := []string{"--quiet", "--wait", "--collect", "--unit=" + unit}
	if h.PTY {
		args = append(args, "--pty")
	} else {
		args = append(args, "--pipe")
	}
	for _, p := range b.props(h, unit) {
		args = append(args, "-p", p)
	}
	env := []string{"HOME=/data/vladhost/site/tmp", "LANG=C.UTF-8", "USER=" + u, "LOGNAME=" + u, "SHELL=/bin/bash",
		"PATH=/usr/local/bin:/usr/bin:/bin", "VLADHOST_SITE=" + h.Host}
	if h.PTY {
		env = append(env, "TERM="+h.Term)
	}
	for _, e := range env {
		args = append(args, "-E", e)
	}
	args = append(args, "--")
	switch h.Op {
	case shellproto.OpShell:
		args = append(args, "/bin/bash", "-l")
	case shellproto.OpExec:
		args = append(args, "/bin/bash", "-c", h.Cmd)
	case shellproto.OpSFTP:
		args = append(args, "/usr/lib/openssh/sftp-server", "-d", "/data/vladhost/site/public")
	}
	cmd := exec.Command(b.cfg.SystemdRun, args...)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "TERM=" + term(h)}
	return cmd
}

func term(h shellproto.Header) string {
	if h.Term != "" {
		return h.Term
	}
	return "dumb"
}

func (b *Broker) register(site int64, s *session) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.total >= b.cfg.Total || len(b.sessions[site]) >= b.cfg.PerSite {
		return false
	}
	if b.sessions[site] == nil {
		b.sessions[site] = map[*session]struct{}{}
	}
	b.sessions[site][s] = struct{}{}
	b.total++
	return true
}

func (b *Broker) unregister(site int64, s *session) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.sessions[site][s]; ok {
		delete(b.sessions[site], s)
		b.total--
	}
}

// killSite закрывает все сеансы сайта и возвращает их число.
func (b *Broker) killSite(site int64) int {
	b.mu.Lock()
	var list []*session
	for s := range b.sessions[site] {
		list = append(list, s)
	}
	b.mu.Unlock()
	for _, s := range list {
		s.cancel()
	}
	return len(list)
}

func (b *Broker) stopUnit(unit string) {
	_ = exec.Command(b.cfg.Systemctl, "stop", unit+".service").Run() // если службы уже нет, это не ошибка
}

func (b *Broker) signalUnit(unit, sig string) {
	_ = exec.Command(b.cfg.Systemctl, "kill", "--signal=SIG"+sig, unit+".service").Run()
}

func (b *Broker) run(nc net.Conn, conn *shellproto.Conn, h shellproto.Header) {
	unit := unitName(h.Site)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess := &session{unit: unit, cancel: cancel}
	if !b.register(h.Site, sess) {
		_ = conn.Write(shellproto.FrameFail, []byte("busy"))
		return
	}
	defer b.unregister(h.Site, sess)
	cmd := b.command(h, unit)

	var (
		stdin  io.Writer
		outs   []io.Reader
		errs   io.Reader
		closer func()
		ptmx   *os.File
	)
	if h.PTY {
		f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(h.Cols), Rows: uint16(h.Rows)})
		if err != nil {
			log.Printf("shell-broker: запуск сеанса сайта %d: %v", h.Site, err)
			_ = conn.Write(shellproto.FrameFail, []byte("start_failed"))
			return
		}
		ptmx = f
		stdin, outs, closer = f, []io.Reader{f}, func() { _ = f.Close() }
	} else {
		// Свои каналы через os.Pipe: cmd.Wait не закрывает наши концы, поэтому вывод, который процесс успел написать, читается до конца.
		inR, inW, err1 := os.Pipe()
		outR, outW, err2 := os.Pipe()
		errR, errW, err3 := os.Pipe()
		if err1 != nil || err2 != nil || err3 != nil {
			_ = conn.Write(shellproto.FrameFail, []byte("start_failed"))
			return
		}
		cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
		err := cmd.Start()
		_ = inR.Close()
		_ = outW.Close()
		_ = errW.Close()
		if err != nil {
			_ = inW.Close()
			_ = outR.Close()
			_ = errR.Close()
			log.Printf("shell-broker: запуск сеанса сайта %d: %v", h.Site, err)
			_ = conn.Write(shellproto.FrameFail, []byte("start_failed"))
			return
		}
		stdin, outs, errs, closer = inW, []io.Reader{outR}, errR, func() { _ = inW.Close(); _ = outR.Close(); _ = errR.Close() }
	}
	log.Printf("shell-broker: сеанс %s (%s) сайта %s начат", unit, h.Op, h.Host)

	// Отмена (панель закрыла соединение или доступ отключили): останавливаем службу и процесс-обёртку.
	go func() {
		<-ctx.Done()
		b.stopUnit(unit)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	var wg sync.WaitGroup
	pump := func(r io.Reader, typ byte) {
		defer wg.Done()
		buf := make([]byte, 32<<10)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				if werr := conn.Write(typ, buf[:n]); werr != nil {
					cancel()
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	for _, r := range outs {
		wg.Add(1)
		go pump(r, shellproto.FrameOut)
	}
	if errs != nil {
		wg.Add(1)
		go pump(errs, shellproto.FrameErr)
	}

	// Ввод, изменение размера, сигналы — от панели; обрыв соединения останавливает сеанс.
	go func() {
		defer cancel()
		for {
			typ, payload, err := conn.Read()
			if err != nil {
				return
			}
			switch typ {
			case shellproto.FrameStdin:
				if _, err := stdin.Write(payload); err != nil {
					return
				}
			case shellproto.FrameEOF:
				if !h.PTY {
					_ = stdin.(io.Closer).Close()
				} else {
					_, _ = stdin.Write([]byte{4}) // Ctrl-D
				}
			case shellproto.FrameResize:
				var r shellproto.Resize
				if json.Unmarshal(payload, &r) == nil && ptmx != nil {
					_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(clamp(r.Cols, 80)), Rows: uint16(clamp(r.Rows, 24))})
				}
			case shellproto.FrameSignal:
				if s := string(payload); s == "INT" || s == "TERM" || s == "HUP" || s == "QUIT" || s == "KILL" {
					b.signalUnit(unit, s)
				}
			}
		}
	}()

	err := cmd.Wait()
	// Вывод, который процесс успел написать, дочитывается до конца.
	wgDone := make(chan struct{})
	go func() { wg.Wait(); close(wgDone) }()
	select {
	case <-wgDone:
	case <-time.After(2 * time.Second):
	}
	closer()
	b.stopUnit(unit)

	code := 0
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		code = ee.ExitCode()
		if code < 0 {
			code = 255
		}
	default:
		code = 255
	}
	_ = conn.WriteJSON(shellproto.FrameExit, shellproto.Exit{Code: code})
	log.Printf("shell-broker: сеанс %s закончен, код %d", unit, code)
	_ = nc.Close()
}

// Listen создаёт сокет посредника с правами только для панели: владелец — root, группа — группа панели, режим 0660.
func Listen(path string, gid int) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	old := syscall.Umask(0o117)
	l, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, err
	}
	if gid >= 0 {
		// Владелец не меняется (root в работе, текущий пользователь в разработке): сокет получает только группу панели.
		if err := os.Chown(path, -1, gid); err != nil {
			_ = l.Close()
			return nil, err
		}
	}
	if err := os.Chmod(path, 0o660); err != nil {
		_ = l.Close()
		return nil, err
	}
	return l, nil
}

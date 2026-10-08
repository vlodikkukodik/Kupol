package shellbroker

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"vladhost/internal/shellproto"
)

const testHost = "blog.vlad.vladinc.ru"

type fixture struct {
	t    *testing.T
	b    *Broker
	sock string
	dir  string
	log  string
}

// newFixture поднимает посредника с подменой systemd-run: скрипт записывает вызов и выполняет команду после «--» как есть.
func newFixture(t *testing.T, mutate func(*Config)) *fixture {
	t.Helper()
	dir, err := os.MkdirTemp("", "vhsb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sites := filepath.Join(dir, "sites")
	for _, d := range []string{filepath.Join(sites, testHost, "public"), filepath.Join(sites, testHost, "tmp"), filepath.Join(dir, "shell")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "shell", "7"), []byte(testHost+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logf := filepath.Join(dir, "calls.log")
	script := filepath.Join(dir, "systemd-run")
	body := "#!/bin/bash\necho \"$@\" >>" + logf + "\nwhile [ \"${1:-}\" != \"--\" ]; do shift; done\nshift\nexec \"$@\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(dir, "systemctl")
	if err := os.WriteFile(stub, []byte("#!/bin/bash\necho \"systemctl $@\" >>"+logf+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Dir: dir, Sites: sites, PanelUID: os.Getuid(), SystemdRun: script, Systemctl: stub}
	if mutate != nil {
		mutate(&cfg)
	}
	b := New(cfg)
	sock := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() { _ = b.Serve(l) }()
	return &fixture{t: t, b: b, sock: sock, dir: dir, log: logf}
}

type client struct {
	t    *testing.T
	conn *shellproto.Conn
	nc   net.Conn
}

func (f *fixture) dial(h shellproto.Header) *client {
	f.t.Helper()
	nc, err := net.Dial("unix", f.sock)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = nc.Close() })
	c := &client{t: f.t, conn: shellproto.NewConn(nc), nc: nc}
	// Посредник мог уже закрыть соединение с отказом (forbidden): запись тогда не удастся, а отказ читается из сокета.
	_ = c.conn.WriteJSON(shellproto.FrameHeader, h)
	return c
}

func header(op string) shellproto.Header {
	return shellproto.Header{Op: op, Site: 7, Host: testHost}
}

type result struct {
	out, err, fail string
	code           int
	exited         bool
}

// collect читает кадры до конца сеанса или отказа.
func (c *client) collect() result {
	c.t.Helper()
	var r result
	_ = c.nc.SetReadDeadline(time.Now().Add(15 * time.Second))
	for {
		typ, payload, err := c.conn.Read()
		if err != nil {
			return r
		}
		switch typ {
		case shellproto.FrameOut:
			r.out += string(payload)
		case shellproto.FrameErr:
			r.err += string(payload)
		case shellproto.FrameFail:
			r.fail = string(payload)
			return r
		case shellproto.FrameExit:
			var e shellproto.Exit
			_ = json.Unmarshal(payload, &e)
			r.code, r.exited = e.Code, true
			return r
		}
	}
}

func TestExecReturnsOutputErrorsAndExitCode(t *testing.T) {
	f := newFixture(t, nil)
	h := header(shellproto.OpExec)
	h.Cmd = "echo out; echo err >&2; exit 3"
	r := f.dial(h).collect()
	if !r.exited || r.code != 3 || strings.TrimSpace(r.out) != "out" || strings.TrimSpace(r.err) != "err" {
		t.Fatalf("%+v", r)
	}
}

func TestExecStdinAndEOF(t *testing.T) {
	f := newFixture(t, nil)
	h := header(shellproto.OpExec)
	h.Cmd = "cat"
	c := f.dial(h)
	_ = c.conn.Write(shellproto.FrameStdin, []byte("hello "))
	_ = c.conn.Write(shellproto.FrameStdin, []byte("world"))
	_ = c.conn.Write(shellproto.FrameEOF, nil)
	if r := c.collect(); !r.exited || r.code != 0 || r.out != "hello world" {
		t.Fatalf("%+v", r)
	}
}

func TestExecLargeOutputIsNotTruncated(t *testing.T) {
	f := newFixture(t, nil)
	h := header(shellproto.OpExec)
	h.Cmd = "head -c 300000 /dev/zero | tr '\\0' x"
	r := f.dial(h).collect()
	if !r.exited || len(r.out) != 300000 {
		t.Fatalf("получено %d байт, код %d", len(r.out), r.code)
	}
}

func TestShellOnPTYWithResize(t *testing.T) {
	f := newFixture(t, nil)
	h := header(shellproto.OpShell)
	h.Term, h.Cols, h.Rows = "xterm-256color", 80, 24
	c := f.dial(h)
	_ = c.conn.WriteJSON(shellproto.FrameResize, shellproto.Resize{Cols: 100, Rows: 30})
	time.Sleep(200 * time.Millisecond)
	_ = c.conn.Write(shellproto.FrameStdin, []byte("echo TERM=$TERM; stty size; exit 5\n"))
	r := c.collect()
	if !r.exited || r.code != 5 || !strings.Contains(r.out, "TERM=xterm-256color") || !strings.Contains(r.out, "30 100") {
		t.Fatalf("%+v", r)
	}
}

func TestSandboxArguments(t *testing.T) {
	f := newFixture(t, nil)
	h := header(shellproto.OpShell)
	h.Term, h.Cols, h.Rows = "xterm", 80, 24
	c := f.dial(h)
	_ = c.conn.Write(shellproto.FrameStdin, []byte("exit\n"))
	c.collect()
	raw, _ := os.ReadFile(f.log)
	args := string(raw)
	for _, want := range []string{
		"--pty", "--wait", "--collect", "--unit=vhshell-7-", "User=vhs7", "Group=vhs7", "NoNewPrivileges=yes", "ProtectSystem=strict", "ProtectProc=invisible",
		"InaccessiblePaths=-/etc/vladhost -/var/lib/vladhost", "TemporaryFileSystem=/data/vladhost:mode=0755,size=1m,nosuid,nodev",
		"BindPaths=" + filepath.Join(f.b.cfg.Sites, testHost, "public") + ":/data/vladhost/site/public " + filepath.Join(f.b.cfg.Sites, testHost, "tmp") + ":/data/vladhost/site/tmp",
		"MemoryMax=512M", "TasksMax=128", "RuntimeMaxSec=28800", "HOME=/data/vladhost/site/tmp", "TERM=xterm", "VLADHOST_SITE=" + testHost, "-- /bin/bash -l",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("в вызове systemd-run нет %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "--pipe") {
		t.Error("у терминала нет --pipe")
	}
}

func TestExecUsesPipeAndSFTPCommandLine(t *testing.T) {
	f := newFixture(t, nil)
	h := header(shellproto.OpExec)
	h.Cmd = "true"
	f.dial(h).collect()
	raw, _ := os.ReadFile(f.log)
	if !strings.Contains(string(raw), "--pipe") || strings.Contains(string(raw), "TERM=") || !strings.Contains(string(raw), "-- /bin/bash -c true") {
		t.Fatalf("%s", raw)
	}
	cmd := f.b.command(shellproto.Header{Op: shellproto.OpSFTP, Site: 7, Host: testHost}, "u")
	if got := strings.Join(cmd.Args, " "); !strings.Contains(got, "-- /usr/lib/openssh/sftp-server -d /data/vladhost/site/public") || !strings.Contains(got, "--pipe") {
		t.Fatalf("sftp: %s", got)
	}
}

func TestRequestsAreRejected(t *testing.T) {
	f := newFixture(t, nil)
	if err := os.WriteFile(filepath.Join(f.dir, "shell", "8"), []byte("other.vlad.vladinc.ru"), 0o644); err != nil {
		t.Fatal(err)
	}
	// у сайта 9 метка есть, но папки tmp нет
	_ = os.WriteFile(filepath.Join(f.dir, "shell", "9"), []byte("nodir.vlad.vladinc.ru"), 0o644)
	_ = os.MkdirAll(filepath.Join(f.b.cfg.Sites, "nodir.vlad.vladinc.ru", "public"), 0o755)
	cases := map[string]shellproto.Header{
		"bad_op":      {Op: "format", Site: 7, Host: testHost},
		"bad_site":    {Op: shellproto.OpShell, Site: 0, Host: testHost},
		"bad_site2":   {Op: shellproto.OpShell, Site: 10000, Host: testHost},
		"bad_site3":   {Op: shellproto.OpShell, Site: 7, Host: "../etc"},
		"bad_site4":   {Op: shellproto.OpShell, Site: 7, Host: "a..b.c.d"},
		"bad_command": {Op: shellproto.OpExec, Site: 7, Host: testHost, Cmd: ""},
		"bad_term":    {Op: shellproto.OpShell, Site: 7, Host: testHost, Term: "x;rm"},
		"disabled":    {Op: shellproto.OpShell, Site: 5, Host: testHost},                // метки нет
		"disabled2":   {Op: shellproto.OpShell, Site: 8, Host: testHost},                // метка про другой сайт
		"no_site":     {Op: shellproto.OpShell, Site: 9, Host: "nodir.vlad.vladinc.ru"}, // нет tmp
	}
	for name, h := range cases {
		want := strings.TrimRight(name, "0123456789")
		r := f.dial(h).collect()
		if r.fail != want || r.exited {
			t.Errorf("%s: %+v", name, r)
		}
	}
	// слишком длинная команда
	long := header(shellproto.OpExec)
	long.Cmd = strings.Repeat("a", shellproto.MaxCommand+1)
	if r := f.dial(long).collect(); r.fail != "bad_command" {
		t.Errorf("длинная команда: %+v", r)
	}
}

func TestOnlyPanelUserMayConnect(t *testing.T) {
	f := newFixture(t, func(c *Config) { c.PanelUID = os.Getuid() + 1 })
	h := header(shellproto.OpExec)
	h.Cmd = "id"
	if r := f.dial(h).collect(); r.fail != "forbidden" || r.out != "" {
		t.Fatalf("%+v", r)
	}
}

func TestSessionLimitsPerSite(t *testing.T) {
	f := newFixture(t, func(c *Config) { c.PerSite = 2 })
	h := header(shellproto.OpExec)
	h.Cmd = "sleep 30"
	a, b := f.dial(h), f.dial(h)
	time.Sleep(300 * time.Millisecond)
	if r := f.dial(h).collect(); r.fail != "busy" {
		t.Fatalf("третий сеанс: %+v", r)
	}
	// другой сайт не страдает от чужого лимита
	_ = os.WriteFile(filepath.Join(f.dir, "shell", "11"), []byte("second.vlad.vladinc.ru"), 0o644)
	_ = os.MkdirAll(filepath.Join(f.b.cfg.Sites, "second.vlad.vladinc.ru", "public"), 0o755)
	_ = os.MkdirAll(filepath.Join(f.b.cfg.Sites, "second.vlad.vladinc.ru", "tmp"), 0o755)
	quick := shellproto.Header{Op: shellproto.OpExec, Site: 11, Host: "second.vlad.vladinc.ru", Cmd: "echo ok"}
	if r := f.dial(quick).collect(); !r.exited || strings.TrimSpace(r.out) != "ok" {
		t.Fatalf("другой сайт: %+v", r)
	}
	_ = a.nc.Close()
	_ = b.nc.Close()
	// после закрытия место освобождается
	deadline := time.Now().Add(5 * time.Second)
	for {
		h2 := header(shellproto.OpExec)
		h2.Cmd = "echo again"
		r := f.dial(h2).collect()
		if r.exited && strings.TrimSpace(r.out) == "again" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("место не освободилось: %+v", r)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestKillClosesSessionsOfSite(t *testing.T) {
	f := newFixture(t, nil)
	h := header(shellproto.OpExec)
	h.Cmd = "sleep 60"
	long := f.dial(h)
	time.Sleep(300 * time.Millisecond)
	k := f.dial(shellproto.Header{Op: shellproto.OpKill, Site: 7})
	r := k.collect()
	if !r.exited || r.code != 1 {
		t.Fatalf("kill должен сообщить число закрытых сеансов: %+v", r)
	}
	done := make(chan result, 1)
	go func() { done <- long.collect() }()
	select {
	case got := <-done:
		if got.code == 0 && got.exited {
			t.Fatalf("сеанс должен был завершиться принудительно: %+v", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("сеанс не остановился")
	}
	if raw, _ := os.ReadFile(f.log); !strings.Contains(string(raw), "systemctl stop vhshell-7-") {
		t.Fatalf("служба должна быть остановлена:\n%s", raw)
	}
}

func TestDroppedConnectionKillsProcess(t *testing.T) {
	f := newFixture(t, nil)
	pidFile := filepath.Join(f.dir, "pid")
	h := header(shellproto.OpExec)
	h.Cmd = "echo $$ >" + pidFile + "; exec sleep 60"
	c := f.dial(h)
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
			if pid > 0 {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("процесс не запустился")
	}
	_ = c.nc.Close()
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); err != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("после обрыва соединения процесс должен быть убит")
}

func TestFrameLimitsAndHeaderMustComeFirst(t *testing.T) {
	f := newFixture(t, nil)
	nc, _ := net.Dial("unix", f.sock)
	defer func() { _ = nc.Close() }()
	c := shellproto.NewConn(nc)
	_ = c.Write(shellproto.FrameStdin, []byte("x")) // не заголовок
	typ, payload, err := c.Read()
	if err != nil || typ != shellproto.FrameFail || string(payload) != "bad_request" {
		t.Fatalf("%c %q %v", typ, payload, err)
	}
	// кадр больше предела отвергается при чтении
	var buf bytes.Buffer
	buf.Write([]byte{'H', 0xff, 0xff, 0xff, 0xff})
	if _, _, err := shellproto.NewConn(&buf).Read(); err == nil {
		t.Fatal("огромный кадр должен отклоняться")
	}
}

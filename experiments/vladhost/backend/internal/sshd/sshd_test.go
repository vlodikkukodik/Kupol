package sshd

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"vladhost/internal/shellaccess"
	"vladhost/internal/shellbroker"
	"vladhost/internal/shellclient"
	"vladhost/internal/shellproto"
)

const testHost = "blog.vlad.vladinc.ru"

// fakeAccess: один ключ, один сайт.
type fakeAccess struct {
	key     ssh.PublicKey
	login   string
	site    int64
	host    string
	touched chan int64
}

func (f *fakeAccess) Authenticate(_ context.Context, login string, key ssh.PublicKey) (*shellaccess.Grant, error) {
	if login != f.login || string(key.Marshal()) != string(f.key.Marshal()) {
		return nil, shellaccess.ErrAuth
	}
	return &shellaccess.Grant{UserID: 1, Username: "vlad", SiteID: f.site, Host: f.host, KeyID: 42}, nil
}

func (f *fakeAccess) Touch(_ context.Context, id int64) {
	select {
	case f.touched <- id:
	default:
	}
}

type recordingOpener struct {
	inner shellclient.Client
	mu    sync.Mutex
	ops   []string
	// ifSFTP: вместо sftp-server запускаем cat, чтобы проверить поток данных без настоящего SFTP.
	ifSFTP bool
}

func (r *recordingOpener) Open(ctx context.Context, h shellproto.Header) (*shellclient.Session, error) {
	r.mu.Lock()
	r.ops = append(r.ops, h.Op)
	r.mu.Unlock()
	if r.ifSFTP && h.Op == shellproto.OpSFTP {
		h.Op, h.Cmd = shellproto.OpExec, "cat"
	}
	return r.inner.Open(ctx, h)
}

type env struct {
	t      *testing.T
	srv    *Server
	addr   string
	access *fakeAccess
	signer ssh.Signer
	rec    *recordingOpener
	dir    string
}

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newEnv(t *testing.T, mutate func(*Config), marked bool) *env {
	t.Helper()
	dir, err := os.MkdirTemp("", "vhssh")
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
	if marked {
		_ = os.WriteFile(filepath.Join(dir, "shell", "7"), []byte(testHost), 0o644)
	}
	script := filepath.Join(dir, "systemd-run")
	_ = os.WriteFile(script, []byte("#!/bin/bash\nwhile [ \"${1:-}\" != \"--\" ]; do shift; done\nshift\nexec \"$@\"\n"), 0o755)
	stub := filepath.Join(dir, "systemctl")
	_ = os.WriteFile(stub, []byte("#!/bin/bash\n"), 0o755)
	broker := shellbroker.New(shellbroker.Config{Dir: dir, Sites: sites, PanelUID: os.Getuid(), SystemdRun: script, Systemctl: stub})
	sock := filepath.Join(dir, "broker.sock")
	bl, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bl.Close() })
	go func() { _ = broker.Serve(bl) }()

	signer := newSigner(t)
	access := &fakeAccess{key: signer.PublicKey(), login: "blog.vlad", site: 7, host: testHost, touched: make(chan int64, 4)}
	rec := &recordingOpener{inner: shellclient.Client{Socket: sock}}
	cfg := Config{HostKeyPath: filepath.Join(dir, "host_key"), Access: access, Broker: rec}
	if mutate != nil {
		mutate(&cfg)
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, addr: l.Addr().String(), access: access, signer: signer, rec: rec, dir: dir}
}

func (e *env) connect(user string, auth ssh.AuthMethod) (*ssh.Client, error) {
	return ssh.Dial("tcp", e.addr, &ssh.ClientConfig{
		User: user, Auth: []ssh.AuthMethod{auth}, Timeout: 5 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintSHA256(key) != e.srv.HostFingerprint() {
				return errors.New("host key mismatch")
			}
			return nil
		},
	})
}

func (e *env) client() *ssh.Client {
	e.t.Helper()
	c, err := e.connect("blog.vlad", ssh.PublicKeys(e.signer))
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestExecOutputStatusAndStderr(t *testing.T) {
	e := newEnv(t, nil, true)
	c := e.client()
	s, _ := c.NewSession()
	defer func() { _ = s.Close() }()
	var out, errb strings.Builder
	s.Stdout, s.Stderr = &out, &errb
	err := s.Run("echo out; echo err >&2; exit 3")
	var ee *ssh.ExitError
	if !errors.As(err, &ee) || ee.ExitStatus() != 3 || strings.TrimSpace(out.String()) != "out" || strings.TrimSpace(errb.String()) != "err" {
		t.Fatalf("%v %q %q", err, out.String(), errb.String())
	}
	select {
	case id := <-e.access.touched:
		if id != 42 {
			t.Fatal(id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("вход ключа должен быть отмечен")
	}
}

func TestInteractiveShellWithPTYAndWindowChange(t *testing.T) {
	e := newEnv(t, nil, true)
	c := e.client()
	s, _ := c.NewSession()
	defer func() { _ = s.Close() }()
	if err := s.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{ssh.ECHO: 0}); err != nil {
		t.Fatal(err)
	}
	in, _ := s.StdinPipe()
	out, _ := s.StdoutPipe()
	if err := s.Shell(); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	var mu sync.Mutex
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := out.Read(b)
			mu.Lock()
			buf.Write(b[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	get := func() string { mu.Lock(); defer mu.Unlock(); return buf.String() }
	_, _ = io.WriteString(in, "stty size; echo T=$TERM\n")
	waitFor(t, func() bool { return strings.Contains(get(), "24 80") && strings.Contains(get(), "T=xterm-256color") })
	if err := s.WindowChange(40, 120); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	_, _ = io.WriteString(in, "stty size; exit 4\n")
	waitFor(t, func() bool { return strings.Contains(get(), "40 120") })
	err := s.Wait()
	var ee *ssh.ExitError
	if !errors.As(err, &ee) || ee.ExitStatus() != 4 {
		t.Fatalf("код выхода: %v", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	end := time.Now().Add(8 * time.Second)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("не дождались условия")
}

func TestShellWithoutPTYUsesBash(t *testing.T) {
	e := newEnv(t, nil, true)
	c := e.client()
	s, _ := c.NewSession()
	defer func() { _ = s.Close() }()
	in, _ := s.StdinPipe()
	var out strings.Builder
	s.Stdout = &out
	if err := s.Shell(); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(in, "echo without-pty\nexit\n")
	if err := s.Wait(); err != nil || !strings.Contains(out.String(), "without-pty") {
		t.Fatalf("%v %q", err, out.String())
	}
}

func TestOnlyKeyAuthenticationForTheRightSite(t *testing.T) {
	e := newEnv(t, nil, true)
	if _, err := e.connect("blog.vlad", ssh.Password("secret")); err == nil {
		t.Fatal("пароль не должен приниматься")
	}
	if _, err := e.connect("other.vlad", ssh.PublicKeys(e.signer)); err == nil {
		t.Fatal("чужой сайт не должен приниматься")
	}
	if _, err := e.connect("blog.vlad", ssh.PublicKeys(newSigner(t))); err == nil {
		t.Fatal("чужой ключ не должен приниматься")
	}
	if _, err := e.connect("root", ssh.PublicKeys(e.signer)); err == nil {
		t.Fatal("root — не сайт")
	}
}

func TestForwardingAndOtherChannelsAreRefused(t *testing.T) {
	e := newEnv(t, nil, true)
	c := e.client()
	if conn, err := c.Dial("tcp", "127.0.0.1:22"); err == nil {
		_ = conn.Close()
		t.Fatal("пересылка портов должна быть запрещена")
	}
	s, _ := c.NewSession()
	defer func() { _ = s.Close() }()
	if ok, _ := s.SendRequest("auth-agent-req@openssh.com", true, nil); ok {
		t.Fatal("агент не поддерживается")
	}
	if ok, _ := s.SendRequest("x11-req", true, ssh.Marshal(struct {
		S bool
		P string
		C string
		N uint32
	}{false, "MIT-MAGIC-COOKIE-1", "00", 0})); ok {
		t.Fatal("X11 не поддерживается")
	}
	if err := s.RequestSubsystem("netconf"); err == nil {
		t.Fatal("неизвестная подсистема отклоняется")
	}
}

func TestSFTPSubsystemIsPassedToBroker(t *testing.T) {
	e := newEnv(t, nil, true)
	e.rec.ifSFTP = true
	c := e.client()
	s, _ := c.NewSession()
	defer func() { _ = s.Close() }()
	in, _ := s.StdinPipe()
	out, _ := s.StdoutPipe()
	if err := s.RequestSubsystem("sftp"); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(in, "ping-through-sftp\n")
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "ping-through-sftp\n" {
		t.Fatalf("%q %v", line, err)
	}
	e.rec.mu.Lock()
	defer e.rec.mu.Unlock()
	if len(e.rec.ops) != 1 || e.rec.ops[0] != shellproto.OpSFTP {
		t.Fatalf("операции: %v", e.rec.ops)
	}
}

func TestBrokerRefusalIsExplained(t *testing.T) {
	e := newEnv(t, nil, false) // метки «доступ включён» нет
	c := e.client()
	s, _ := c.NewSession()
	defer func() { _ = s.Close() }()
	var errb strings.Builder
	s.Stderr = &errb
	err := s.Run("echo hi")
	var ee *ssh.ExitError
	if !errors.As(err, &ee) || ee.ExitStatus() != 1 || !strings.Contains(errb.String(), "shell access is disabled") {
		t.Fatalf("%v %q", err, errb.String())
	}
}

func TestBrokerDownIsReported(t *testing.T) {
	e := newEnv(t, func(c *Config) {
		c.Broker = &recordingOpener{inner: shellclient.Client{Socket: "/nonexistent/broker.sock"}}
	}, true)
	c := e.client()
	s, _ := c.NewSession()
	defer func() { _ = s.Close() }()
	var errb strings.Builder
	s.Stderr = &errb
	if err := s.Run("echo hi"); err == nil || !strings.Contains(errb.String(), "not available") {
		t.Fatalf("%v %q", err, errb.String())
	}
}

func TestIdleConnectionIsClosed(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.Idle = 800 * time.Millisecond }, true)
	c := e.client()
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("молчащее соединение должно закрываться")
	}
}

func TestFailedLoginsBlockTheAddress(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.MaxFails = 3; c.FailWindow = time.Minute }, true)
	for i := 0; i < 3; i++ {
		if _, err := e.connect("blog.vlad", ssh.PublicKeys(newSigner(t))); err == nil {
			t.Fatal("чужой ключ")
		}
	}
	// Неудача записывается сервером асинхронно, после того как клиент уже получил отказ.
	waitFor(t, func() bool { return e.srv.blocked("127.0.0.1") })
	if _, err := e.connect("blog.vlad", ssh.PublicKeys(e.signer)); err == nil {
		t.Fatal("после серии неудач адрес блокируется, даже с верным ключом")
	}
}

func TestPerIPConnectionLimit(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.MaxPerIP = 2 }, true)
	a, b := e.client(), e.client()
	if _, err := e.connect("blog.vlad", ssh.PublicKeys(e.signer)); err == nil {
		t.Fatal("третье соединение с одного адреса отклоняется")
	}
	_ = a.Close()
	_ = b.Close()
}

func TestHostKeyIsPersistedAndPrivate(t *testing.T) {
	e := newEnv(t, nil, true)
	path := filepath.Join(e.dir, "host_key")
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", fi, err)
	}
	again, err := New(Config{HostKeyPath: path, Access: e.access, Broker: e.rec})
	if err != nil || again.HostFingerprint() != e.srv.HostFingerprint() || !strings.HasPrefix(again.HostFingerprint(), "SHA256:") {
		t.Fatalf("ключ сервера должен сохраняться между запусками: %v", err)
	}
}

func TestClosingClientKillsRemoteProcess(t *testing.T) {
	e := newEnv(t, nil, true)
	pidFile := filepath.Join(e.dir, "pid")
	c := e.client()
	s, _ := c.NewSession()
	go func() { _ = s.Run("echo $$ >" + pidFile + "; exec sleep 60") }()
	var pid string
	waitFor(t, func() bool {
		raw, err := os.ReadFile(pidFile)
		pid = strings.TrimSpace(string(raw))
		return err == nil && pid != ""
	})
	_ = c.Close()
	waitFor(t, func() bool { _, err := os.Stat("/proc/" + pid); return err != nil })
}

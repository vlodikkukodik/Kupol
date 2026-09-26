// Package sshd — SSH-сервер панели (порт 2222; системный sshd на 22 не трогаем). Вход только по ключам аккаунта, логин — «сайт.пользователь»
// (как у FTP). Сам сервер ничего не запускает: оболочку, команды и SFTP он просит у посредника оболочек, который поднимает их в песочнице
// сайта. Пересылка портов, агент и X11 не поддерживаются.
package sshd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"vladhost/internal/shellaccess"
	"vladhost/internal/shellclient"
	"vladhost/internal/shellproto"
)

// Access проверяет вход (реализует *shellaccess.Service).
type Access interface {
	Authenticate(ctx context.Context, login string, key ssh.PublicKey) (*shellaccess.Grant, error)
	Touch(ctx context.Context, keyID int64)
}

// Opener открывает сеанс у посредника оболочек (реализует shellclient.Client).
type Opener interface {
	Open(ctx context.Context, h shellproto.Header) (*shellclient.Session, error)
}

// Config — настройки сервера.
type Config struct {
	HostKeyPath string
	Access      Access
	Broker      Opener
	// Idle — сколько соединение может молчать, прежде чем его закроют (по умолчанию 30 минут).
	Idle time.Duration
	// MaxFails и FailWindow — сколько неудачных входов с одного адреса терпим за окно.
	MaxFails   int
	FailWindow time.Duration
	// MaxPerIP — одновременных соединений с одного адреса.
	MaxPerIP int
}

// Server — SSH-сервер.
type Server struct {
	cfg  Config
	ssh  *ssh.ServerConfig
	host ssh.Signer

	mu    sync.Mutex
	fails map[string]*failure
	conns map[string]int
	wg    sync.WaitGroup
	quit  atomic.Bool
	l     net.Listener
}

type failure struct {
	n     int
	since time.Time
}

// New загружает ключ сервера (при первом запуске создаёт) и готовит настройки протокола.
func New(cfg Config) (*Server, error) {
	if cfg.Idle <= 0 {
		cfg.Idle = 30 * time.Minute
	}
	if cfg.MaxFails <= 0 {
		cfg.MaxFails = 10
	}
	if cfg.FailWindow <= 0 {
		cfg.FailWindow = 10 * time.Minute
	}
	if cfg.MaxPerIP <= 0 {
		cfg.MaxPerIP = 10
	}
	signer, err := loadHostKey(cfg.HostKeyPath)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, host: signer, fails: map[string]*failure{}, conns: map[string]int{}}
	s.ssh = &ssh.ServerConfig{
		ServerVersion: "SSH-2.0-Vladhost",
		MaxAuthTries:  3,
		// Пароли выключены: PasswordCallback не задан. Только открытый ключ, и только проверенный по подписи.
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			g, err := cfg.Access.Authenticate(ctx, c.User(), key)
			if err != nil {
				return nil, errors.New("access denied")
			}
			return &ssh.Permissions{Extensions: map[string]string{
				"site": strconv.FormatInt(g.SiteID, 10), "host": g.Host, "user": g.Username, "key": strconv.FormatInt(g.KeyID, 10),
			}}, nil
		},
	}
	s.ssh.AddHostKey(signer)
	return s, nil
}

// HostFingerprint — отпечаток ключа сервера (SHA256:…): его сверяют пользователи при первом подключении.
func (s *Server) HostFingerprint() string { return ssh.FingerprintSHA256(s.host.PublicKey()) }

func loadHostKey(path string) (ssh.Signer, error) {
	if data, err := os.ReadFile(path); err == nil {
		return ssh.ParsePrivateKey(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "vladhost-ssh-host")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}

// Serve принимает соединения, пока listener не закроют.
func (s *Server) Serve(l net.Listener) error {
	s.mu.Lock()
	s.l = l
	s.mu.Unlock()
	for {
		nc, err := l.Accept()
		if err != nil {
			if s.quit.Load() {
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serveConn(nc)
		}()
	}
}

// Close останавливает приём и закрывает открытые соединения.
func (s *Server) Close() {
	s.quit.Store(true)
	s.mu.Lock()
	if s.l != nil {
		_ = s.l.Close()
	}
	s.mu.Unlock()
}

func remoteIP(a net.Addr) string {
	h, _, err := net.SplitHostPort(a.String())
	if err != nil {
		return a.String()
	}
	return h
}

func (s *Server) blocked(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fails[ip]
	if f == nil {
		return false
	}
	if time.Since(f.since) > s.cfg.FailWindow {
		delete(s.fails, ip)
		return false
	}
	return f.n >= s.cfg.MaxFails
}

func (s *Server) failed(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.fails) > 20000 {
		s.fails = map[string]*failure{}
	}
	f := s.fails[ip]
	if f == nil || time.Since(f.since) > s.cfg.FailWindow {
		f = &failure{since: time.Now()}
		s.fails[ip] = f
	}
	f.n++
}

func (s *Server) enter(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns[ip] >= s.cfg.MaxPerIP {
		return false
	}
	s.conns[ip]++
	return true
}

func (s *Server) leave(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns[ip]--; s.conns[ip] <= 0 {
		delete(s.conns, ip)
	}
}

// activity — соединение с отметкой последней активности (для закрытия по бездействию).
type activity struct {
	net.Conn
	last atomic.Int64
}

func (a *activity) Read(p []byte) (int, error) {
	n, err := a.Conn.Read(p)
	if n > 0 {
		a.last.Store(time.Now().UnixNano())
	}
	return n, err
}

func (a *activity) Write(p []byte) (int, error) {
	n, err := a.Conn.Write(p)
	if n > 0 {
		a.last.Store(time.Now().UnixNano())
	}
	return n, err
}

func (s *Server) serveConn(nc net.Conn) {
	ip := remoteIP(nc.RemoteAddr())
	if s.blocked(ip) || !s.enter(ip) {
		_ = nc.Close()
		return
	}
	defer s.leave(ip)
	act := &activity{Conn: nc}
	act.last.Store(time.Now().UnixNano())
	_ = nc.SetDeadline(time.Now().Add(30 * time.Second)) // на рукопожатие и вход
	sc, chans, reqs, err := ssh.NewServerConn(act, s.ssh)
	if err != nil {
		s.failed(ip)
		_ = nc.Close()
		return
	}
	_ = nc.SetDeadline(time.Time{})
	defer func() { _ = sc.Close() }()
	if k, err := strconv.ParseInt(sc.Permissions.Extensions["key"], 10, 64); err == nil {
		s.cfg.Access.Touch(context.Background(), k)
	}
	log.Printf("ssh: вход %s на сайт %s с %s", sc.Permissions.Extensions["user"], sc.Permissions.Extensions["host"], ip)

	// Бездействие: клиент, который молчит дольше предела, отключается.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(min(s.cfg.Idle/4, 30*time.Second))
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if time.Since(time.Unix(0, act.last.Load())) > s.cfg.Idle {
					_ = sc.Close()
					return
				}
			}
		}
	}()
	go ssh.DiscardRequests(reqs)

	site, _ := strconv.ParseInt(sc.Permissions.Extensions["site"], 10, 64)
	host := sc.Permissions.Extensions["host"]
	var wg sync.WaitGroup
	for nch := range chans {
		if nch.ChannelType() != "session" {
			_ = nch.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		ch, requests, err := nch.Accept()
		if err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.session(site, host, ch, requests)
		}()
	}
	wg.Wait()
}

type ptyReq struct {
	Term          string
	Cols, Rows    uint32
	Width, Height uint32
	Modes         string
}

type execReq struct{ Command string }
type subsystemReq struct{ Name string }
type windowReq struct {
	Cols, Rows    uint32
	Width, Height uint32
}
type signalReq struct{ Name string }

// session обслуживает один канал: запросы pty, env, shell, exec, subsystem, window-change, signal.
func (s *Server) session(site int64, host string, ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer func() { _ = ch.Close() }()
	var (
		pty     *ptyReq
		started bool
		sess    *shellclient.Session
		done    = make(chan struct{})
	)
	reply := func(r *ssh.Request, ok bool) {
		if r.WantReply {
			_ = r.Reply(ok, nil)
		}
	}
	start := func(h shellproto.Header) bool {
		h.Site, h.Host = site, host
		if pty != nil {
			h.PTY, h.Term, h.Cols, h.Rows = true, pty.Term, int(pty.Cols), int(pty.Rows)
		}
		ss, err := s.cfg.Broker.Open(context.Background(), h)
		if err != nil {
			log.Printf("ssh: посредник оболочек недоступен: %v", err)
			return false
		}
		sess = ss
		started = true
		go s.pump(ch, sess, done)
		return true
	}
	for {
		select {
		case r, ok := <-reqs:
			if !ok {
				if sess != nil {
					sess.Close()
				}
				return
			}
			switch r.Type {
			case "pty-req":
				var p ptyReq
				if started || ssh.Unmarshal(r.Payload, &p) != nil {
					reply(r, false)
					continue
				}
				pty = &p
				reply(r, true)
			case "env":
				reply(r, true) // переменные клиента не передаём в песочницу
			case "shell":
				if started {
					reply(r, false)
					continue
				}
				h := shellproto.Header{Op: shellproto.OpShell}
				if pty == nil {
					h = shellproto.Header{Op: shellproto.OpExec, Cmd: "exec /bin/bash -l"}
				}
				ok := start(h)
				reply(r, true) // клиент должен увидеть сообщение об ошибке, а не безликий отказ
				if !ok {
					s.fail(ch, "shell service is not available")
					return
				}
			case "exec":
				var e execReq
				if started || ssh.Unmarshal(r.Payload, &e) != nil || e.Command == "" {
					reply(r, false)
					continue
				}
				ok := start(shellproto.Header{Op: shellproto.OpExec, Cmd: e.Command})
				reply(r, true)
				if !ok {
					s.fail(ch, "shell service is not available")
					return
				}
			case "subsystem":
				var sub subsystemReq
				if started || ssh.Unmarshal(r.Payload, &sub) != nil || sub.Name != "sftp" {
					reply(r, false)
					continue
				}
				pty = nil
				ok := start(shellproto.Header{Op: shellproto.OpSFTP})
				reply(r, true)
				if !ok {
					s.fail(ch, "shell service is not available")
					return
				}
			case "window-change":
				var w windowReq
				if ssh.Unmarshal(r.Payload, &w) == nil {
					if pty != nil {
						pty.Cols, pty.Rows = w.Cols, w.Rows
					}
					if sess != nil {
						_ = sess.Resize(int(w.Cols), int(w.Rows))
					}
				}
			case "signal":
				var sg signalReq
				if sess != nil && ssh.Unmarshal(r.Payload, &sg) == nil {
					_ = sess.Signal(sg.Name)
				}
			default: // агент, X11 и прочее не поддерживается
				reply(r, false)
			}
		case <-done:
			return
		}
	}
}

func (s *Server) fail(ch ssh.Channel, msg string) {
	_, _ = fmt.Fprintf(ch.Stderr(), "vladhost: %s\r\n", msg)
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{1}))
}

// pump соединяет канал SSH с сеансом посредника: ввод клиента — в сеанс, вывод сеанса — клиенту.
func (s *Server) pump(ch ssh.Channel, sess *shellclient.Session, done chan struct{}) {
	defer close(done)
	defer sess.Close()
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := ch.Read(buf)
			if n > 0 {
				if sess.Stdin(buf[:n]) != nil {
					return
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					_ = sess.CloseStdin()
				} else {
					sess.Close()
				}
				return
			}
		}
	}()
	for {
		ev, err := sess.Recv()
		if err != nil {
			var ref *shellclient.RefusedError
			if errors.As(err, &ref) {
				s.fail(ch, refusal(ref.Code))
			}
			return
		}
		switch ev.Type {
		case shellproto.FrameOut:
			if _, err := ch.Write(ev.Data); err != nil {
				return
			}
		case shellproto.FrameErr:
			if _, err := ch.Stderr().Write(ev.Data); err != nil {
				return
			}
		case shellproto.FrameExit:
			_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{uint32(ev.Exit)}))
			return
		}
	}
}

// refusal переводит код отказа посредника в понятную строку.
func refusal(code string) string {
	switch code {
	case "disabled":
		return "shell access is disabled for this site"
	case "busy":
		return "too many sessions for this site"
	default:
		return "cannot start the session (" + code + ")"
	}
}

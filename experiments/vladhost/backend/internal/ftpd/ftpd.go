// Package ftpd: FTP-сервер Vladhost (только FTPS). Один логин — один сайт, доступ ограничен каталогом
// этого сайта, квота диска проверяется на каждой записи.
package ftpd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"

	"vladhost/internal/apperr"
	"vladhost/internal/config"
	"vladhost/internal/i18n"
	"vladhost/internal/sites"
)

const (
	maxFailures  = 10 // неудачных входов с одного IP...
	failWindow   = 10 * time.Minute
	idleTimeout  = 300 // секунд
	maxPerIPConn = 10
)

type Server struct {
	svc *sites.Service
	cfg config.FTPConfig
	srv *ftpserver.FtpServer

	tlsMu    sync.Mutex
	cert     *tls.Certificate
	certMod  time.Time
	keyMod   time.Time
	selfCert *tls.Certificate // только когда файлы сертификата не заданы (dev)

	mu       sync.Mutex
	failures map[string]*failRecord
	conns    map[string]int
	sessions map[uint32]*sites.FTPSession
	onLogin  func(userID int64, host, ip string)
}

type failRecord struct {
	count int
	since time.Time
}

func New(svc *sites.Service, cfg config.FTPConfig) (*Server, error) {
	s := &Server{svc: svc, cfg: cfg, failures: map[string]*failRecord{}, conns: map[string]int{}, sessions: map[uint32]*sites.FTPSession{}}
	if _, err := s.certificate(); err != nil {
		return nil, fmt.Errorf("ftp certificate: %w", err)
	}
	s.srv = ftpserver.NewFtpServer(s)
	return s, nil
}

// SetLoginHook задаёт функцию, которую вызывают после каждого успешного входа (для журнала действий): номер владельца, адрес сайта, адрес клиента.
func (s *Server) SetLoginHook(f func(userID int64, host, ip string)) {
	s.mu.Lock()
	s.onLogin = f
	s.mu.Unlock()
}

func (s *Server) loginHook() func(userID int64, host, ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.onLogin
}

// Listen занимает порт; Serve принимает подключения. Раздельно — чтобы тест мог узнать адрес.
func (s *Server) Listen() error { return s.srv.Listen() }
func (s *Server) Serve() error  { return s.srv.Serve() }
func (s *Server) Addr() string  { return s.srv.Addr() }
func (s *Server) Stop() error   { return s.srv.Stop() }
func (s *Server) ListenAndServe() error {
	if err := s.Listen(); err != nil {
		return err
	}
	return s.Serve()
}

// --- ftpserver.MainDriver ---

func (s *Server) GetSettings() (*ftpserver.Settings, error) {
	st := &ftpserver.Settings{
		ListenAddr:          s.cfg.Addr,
		PublicHost:          s.cfg.PublicIP,
		Banner:              "Vladhost FTP",
		IdleTimeout:         idleTimeout,
		ConnectionTimeout:   30,
		TLSRequired:         s.tlsRequirement(),
		DisableActiveMode:   true, // активный режим открывает соединения на адрес клиента
		DisableSite:         true,
		DisableMFMT:         true,
		DefaultTransferType: ftpserver.TransferTypeBinary,
	}
	if s.cfg.PassiveStart > 0 {
		st.PassiveTransferPortRange = &ftpserver.PortRange{Start: s.cfg.PassiveStart, End: s.cfg.PassiveEnd}
	}
	return st, nil
}

func (s *Server) ClientConnected(cc ftpserver.ClientContext) (string, error) {
	ip := remoteIP(cc)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns[ip] >= maxPerIPConn {
		return "", errors.New(i18n.Bi("ftp.too_many_conns"))
	}
	s.conns[ip]++
	if s.cfg.AllowPlain {
		return i18n.Bi("ftp.banner_plain"), nil
	}
	return i18n.Bi("ftp.banner"), nil
}

func (s *Server) ClientDisconnected(cc ftpserver.ClientContext) {
	ip := remoteIP(cc)
	s.mu.Lock()
	if s.conns[ip] > 0 {
		s.conns[ip]--
	}
	if s.conns[ip] == 0 {
		delete(s.conns, ip)
	}
	s.mu.Unlock()
	s.mu.Lock()
	sess := s.sessions[cc.ID()]
	delete(s.sessions, cc.ID())
	s.mu.Unlock()
	if sess != nil {
		sess.Close()
	}
}

func (s *Server) AuthUser(cc ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	ip := remoteIP(cc)
	if s.blocked(ip) {
		return nil, errors.New(i18n.Bi("ftp.too_many_failures"))
	}
	sess, err := s.svc.FTPLogin(context.Background(), user, pass)
	if err != nil {
		if errors.Is(err, sites.ErrFTPAuth) {
			s.fail(ip)
		} else {
			log.Printf("ftp: вход %q: %v", user, err)
			err = apperr.New(0, "internal", "internal error")
		}
		return nil, wrap(err)
	}
	s.reset(ip)
	if hook := s.loginHook(); hook != nil {
		site := sess.Site()
		go hook(site.UserID, site.Host, ip) // запись в журнал не задерживает вход
	}
	s.mu.Lock()
	if old := s.sessions[cc.ID()]; old != nil {
		old.Close() // повторный вход на том же соединении (USER/PASS ещё раз)
	}
	s.sessions[cc.ID()] = sess
	s.mu.Unlock()
	return &driver{sess: sess}, nil
}

func (s *Server) GetTLSConfig() (*tls.Config, error) {
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return s.certificate() },
	}, nil
}

// tlsRequirement: FTPS всегда доступен; обычный FTP принимается, если это не запрещено настройкой.
func (s *Server) tlsRequirement() ftpserver.TLSRequirement {
	if s.cfg.AllowPlain {
		return ftpserver.ClearOrEncrypted
	}
	return ftpserver.MandatoryEncryption
}

// --- защита от подбора ---

func (s *Server) blocked(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.failures[ip]
	if !ok {
		return false
	}
	if time.Since(r.since) > failWindow {
		delete(s.failures, ip)
		return false
	}
	return r.count >= maxFailures
}

func (s *Server) fail(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.failures[ip]
	if !ok || time.Since(r.since) > failWindow {
		r = &failRecord{since: time.Now()}
		s.failures[ip] = r
	}
	r.count++
}

func (s *Server) reset(ip string) {
	s.mu.Lock()
	delete(s.failures, ip)
	s.mu.Unlock()
}

func remoteIP(cc ftpserver.ClientContext) string {
	host, _, err := net.SplitHostPort(cc.RemoteAddr().String())
	if err != nil {
		return cc.RemoteAddr().String()
	}
	return host
}

// --- сертификат ---

// certificate отдаёт сертификат из файлов, перечитывая их при смене: certbot обновляет сертификат раз
// в ~60 дней, перезапуск сервера не нужен. Без файлов (только dev) используется временный самоподписанный.
func (s *Server) certificate() (*tls.Certificate, error) {
	s.tlsMu.Lock()
	defer s.tlsMu.Unlock()
	if s.cfg.CertFile == "" {
		if s.selfCert == nil {
			c, err := selfSigned(s.cfg.Host)
			if err != nil {
				return nil, err
			}
			s.selfCert = c
		}
		return s.selfCert, nil
	}
	ci, err1 := os.Stat(s.cfg.CertFile)
	ki, err2 := os.Stat(s.cfg.KeyFile)
	if err := errors.Join(err1, err2); err != nil {
		if s.cert != nil {
			return s.cert, nil // временная ошибка чтения при обновлении — остаёмся на прежнем сертификате
		}
		return nil, err
	}
	if s.cert == nil || !ci.ModTime().Equal(s.certMod) || !ki.ModTime().Equal(s.keyMod) {
		c, err := tls.LoadX509KeyPair(s.cfg.CertFile, s.cfg.KeyFile)
		if err != nil {
			if s.cert != nil {
				return s.cert, nil // файлы могут быть обновлены не одновременно
			}
			return nil, err
		}
		s.cert, s.certMod, s.keyMod = &c, ci.ModTime(), ki.ModTime()
	}
	return s.cert, nil
}

func selfSigned(host string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host, "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// --- ftpserver.ClientDriver: файловая система сайта ---

type driver struct{ sess *sites.FTPSession }

// rel переводит FTP-путь ("/a/b") в путь внутри сайта ("a/b"; корень — ".").
func rel(name string) (string, error) {
	c, err := sites.CleanPath(strings.TrimPrefix(name, "/"))
	if err != nil {
		return "", wrap(err)
	}
	if c == "" {
		return ".", nil
	}
	return c, nil
}

// wrap переводит ошибку домена в ту, которую понимает библиотека (код ответа FTP), с текстом на двух языках:
// у FTP нет заголовка Accept-Language, поэтому язык клиента неизвестен.
func wrap(err error) error {
	ae, ok := errors.AsType[*apperr.Error](err)
	if !ok {
		return err
	}
	msg := i18n.Bi("err."+ae.Code, ae.Args...)
	if errors.Is(err, sites.ErrQuota) {
		return fmt.Errorf("%w: %s", ftpserver.ErrStorageExceeded, msg)
	}
	return errors.New(msg)
}

func (d *driver) Stat(name string) (os.FileInfo, error) {
	p, err := rel(name)
	if err != nil {
		return nil, err
	}
	fi, err := d.sess.Stat(p)
	return fi, wrap(err)
}

func (d *driver) ReadDir(name string) ([]os.FileInfo, error) {
	p, err := rel(name)
	if err != nil {
		return nil, err
	}
	l, err := d.sess.ReadDir(p)
	return l, wrap(err)
}

func (d *driver) GetHandle(name string, flags int, offset int64) (ftpserver.FileTransfer, error) {
	p, err := rel(name)
	if err != nil || p == "." {
		return nil, wrap(sites.ErrBadPath)
	}
	var h sites.Handle
	if flags&(os.O_WRONLY|os.O_RDWR) != 0 {
		h, err = d.sess.OpenWrite(p, flags, offset)
	} else {
		h, err = d.sess.OpenRead(p, offset)
	}
	if err != nil {
		return nil, wrap(err)
	}
	return h, nil
}

func (d *driver) Mkdir(name string, _ os.FileMode) error {
	p, err := rel(name)
	if err != nil {
		return err
	}
	return wrap(d.sess.Mkdir(p))
}

// MkdirAll нужен библиотеке для расширения MKD с вложенными путями; создаём цепочку по одной папке.
func (d *driver) MkdirAll(name string, perm os.FileMode) error {
	p, err := rel(name)
	if err != nil {
		return err
	}
	if p == "." {
		return nil
	}
	cur := ""
	for seg := range strings.SplitSeq(p, "/") {
		if cur != "" {
			cur += "/"
		}
		cur += seg
		if fi, err := d.sess.Stat(cur); err == nil && fi.IsDir() {
			continue
		}
		if err := d.sess.Mkdir(cur); err != nil {
			return wrap(err)
		}
	}
	return nil
}

func (d *driver) Remove(name string) error {
	p, err := rel(name)
	if err != nil || p == "." {
		return wrap(sites.ErrBadPath)
	}
	return wrap(d.sess.Remove(p))
}

func (d *driver) RemoveDir(name string) error {
	p, err := rel(name)
	if err != nil || p == "." {
		return wrap(sites.ErrBadPath)
	}
	return wrap(d.sess.RemoveDir(p))
}

// RemoveAll библиотека вызывает для RMD, когда расширение RemoveDir не найдено; мы его реализуем,
// поэтому сюда попадать не должны. На случай прямого вызова удаляем только пустую папку.
func (d *driver) RemoveAll(name string) error { return d.RemoveDir(name) }

func (d *driver) Rename(oldname, newname string) error {
	from, err1 := rel(oldname)
	to, err2 := rel(newname)
	if err := errors.Join(err1, err2); err != nil {
		return err
	}
	if from == "." || to == "." {
		return wrap(sites.ErrBadPath)
	}
	return wrap(d.sess.Rename(from, to))
}

// Управление правами, владельцем и временем файлов не поддерживается: каталог сайта общий для веб-сервера.
var errNotSupported = errors.New(i18n.Bi("err.not_supported"))

func (d *driver) Chmod(string, os.FileMode) error            { return errNotSupported }
func (d *driver) Chown(string, int, int) error               { return errNotSupported }
func (d *driver) Chtimes(string, time.Time, time.Time) error { return errNotSupported }
func (d *driver) Name() string                               { return "vladhost" }

// Create, Open и OpenFile нужны интерфейсу afero.Fs, но все передачи идут через GetHandle.
func (d *driver) Create(string) (afero.File, error)                     { return nil, errNotSupported }
func (d *driver) Open(string) (afero.File, error)                       { return nil, errNotSupported }
func (d *driver) OpenFile(string, int, os.FileMode) (afero.File, error) { return nil, errNotSupported }

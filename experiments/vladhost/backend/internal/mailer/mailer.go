// Package mailer отправляет письма по SMTP (например, через smtp.majordomo.ru): STARTTLS или неявный TLS, авторизация
// PLAIN или LOGIN по тому, что объявил сервер. Сам по себе ничего не хранит: очередь и повторы — в пакете notify.
package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Message — письмо: один получатель, текстовая и (необязательно) HTML-версии.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Sender отправляет письмо. Ошибка Temporary сообщает, что стоит повторить позже.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Error — ошибка отправки; Temporary — временная (сеть, 4xx ответ), повтор имеет смысл.
type Error struct {
	Temporary bool
	Err       error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// IsTemporary сообщает, что отправку стоит повторить позже.
func IsTemporary(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Temporary
}

const (
	TLSStartTLS = "starttls" // порт 587: открытое соединение, затем STARTTLS (обязательно)
	TLSImplicit = "tls"      // порт 465: TLS с первого байта
	TLSNone     = "none"     // без шифрования: только для локального сервера (проверка при создании)
)

// Config — настройки SMTP-сервера.
type Config struct {
	Host      string
	Port      int
	User      string
	Password  string
	From      string // адрес отправителя
	FromName  string
	TLS       string // TLSStartTLS (по умолчанию), TLSImplicit, TLSNone
	Timeout   time.Duration
	HelloName string // имя в EHLO; по умолчанию localhost
	RootCAs   *x509.CertPool
}

// SMTP — отправитель по SMTP.
type SMTP struct{ cfg Config }

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func cleanHeader(s string) bool { return !strings.ContainsAny(s, "\r\n\x00") }

// NewSMTP проверяет настройки и создаёт отправитель. Пароль по сети без шифрования не уходит:
// TLSNone разрешён только для локального сервера.
func NewSMTP(cfg Config) (*SMTP, error) {
	if cfg.Host == "" || !cleanHeader(cfg.Host) {
		return nil, errors.New("mailer: не задан адрес SMTP-сервера")
	}
	if cfg.TLS == "" {
		cfg.TLS = TLSStartTLS
	}
	switch cfg.TLS {
	case TLSStartTLS, TLSImplicit:
	case TLSNone:
		if !isLoopback(cfg.Host) {
			return nil, errors.New("mailer: отправка без шифрования разрешена только на локальный сервер")
		}
	default:
		return nil, fmt.Errorf("mailer: неизвестный режим TLS %q (starttls, tls или none)", cfg.TLS)
	}
	if cfg.Port == 0 {
		cfg.Port = map[string]int{TLSStartTLS: 587, TLSImplicit: 465, TLSNone: 25}[cfg.TLS]
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil || !cleanHeader(cfg.From) {
		return nil, fmt.Errorf("mailer: недопустимый адрес отправителя %q", cfg.From)
	}
	cfg.From = from.Address
	if !cleanHeader(cfg.FromName) || !cleanHeader(cfg.User) || !cleanHeader(cfg.HelloName) {
		return nil, errors.New("mailer: управляющие символы в настройках")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.HelloName == "" {
		cfg.HelloName = "localhost"
	}
	return &SMTP{cfg: cfg}, nil
}

// Send отправляет письмо. Соединение открывается на каждое письмо: писем мало, зато нет «залежавшихся» сессий.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	rcpt, err := mail.ParseAddress(m.To)
	if err != nil || !cleanHeader(m.To) {
		return &Error{Err: fmt.Errorf("недопустимый адрес получателя %q", m.To)}
	}
	if !cleanHeader(m.Subject) {
		return &Error{Err: errors.New("недопустимая тема письма")}
	}
	body, err := s.build(rcpt.Address, m)
	if err != nil {
		return &Error{Err: err}
	}
	deadline := time.Now().Add(s.cfg.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	dialer := &net.Dialer{Deadline: deadline}
	tlsCfg := &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12, RootCAs: s.cfg.RootCAs}

	var conn net.Conn
	if s.cfg.TLS == TLSImplicit {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return &Error{Temporary: true, Err: fmt.Errorf("подключение к %s: %w", addr, err)}
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(deadline)

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return classify(err, "приветствие сервера")
	}
	defer func() { _ = c.Close() }()
	if err := c.Hello(s.cfg.HelloName); err != nil {
		return classify(err, "EHLO")
	}
	if s.cfg.TLS == TLSStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return &Error{Err: errors.New("сервер не поддерживает STARTTLS: пароль не отправляется открытым текстом")}
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return classify(err, "STARTTLS")
		}
	}
	if s.cfg.User != "" {
		auth, err := s.authFor(c)
		if err != nil {
			return &Error{Err: err}
		}
		if err := c.Auth(auth); err != nil {
			return classify(err, "авторизация")
		}
	}
	if err := c.Mail(s.cfg.From); err != nil {
		return classify(err, "MAIL FROM")
	}
	if err := c.Rcpt(rcpt.Address); err != nil {
		return classify(err, "RCPT TO")
	}
	w, err := c.Data()
	if err != nil {
		return classify(err, "DATA")
	}
	if _, err := w.Write(body); err != nil {
		return classify(err, "передача письма")
	}
	if err := w.Close(); err != nil {
		return classify(err, "завершение письма")
	}
	_ = c.Quit()
	return nil
}

// classify: 4xx-ответы и сетевые сбои временные, 5xx — окончательные.
func classify(err error, stage string) error {
	var tp *textproto.Error
	if errors.As(err, &tp) {
		return &Error{Temporary: tp.Code >= 400 && tp.Code < 500, Err: fmt.Errorf("%s: %w", stage, err)}
	}
	return &Error{Temporary: true, Err: fmt.Errorf("%s: %w", stage, err)}
}

func (s *SMTP) authFor(c *smtp.Client) (smtp.Auth, error) {
	ok, mechs := c.Extension("AUTH")
	if !ok {
		return nil, errors.New("сервер не предлагает авторизацию")
	}
	have := map[string]bool{}
	for _, m := range strings.Fields(strings.ToUpper(mechs)) {
		have[m] = true
	}
	switch {
	case have["PLAIN"]:
		return &plainAuth{user: s.cfg.User, pass: s.cfg.Password}, nil
	case have["LOGIN"]:
		return &loginAuth{user: s.cfg.User, pass: s.cfg.Password}, nil
	}
	return nil, fmt.Errorf("сервер не поддерживает PLAIN и LOGIN (объявлено: %s)", strings.TrimSpace(mechs))
}

// plainAuth и loginAuth не проверяют TLS сами: это делает NewSMTP (без шифрования — только loopback).
type plainAuth struct{ user, pass string }

func (a *plainAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.pass), nil
}

func (a *plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("неожиданный запрос сервера при PLAIN")
	}
	return nil, nil
}

type loginAuth struct{ user, pass string }

func (a *loginAuth) Start(*smtp.ServerInfo) (string, []byte, error) { return "LOGIN", nil, nil }

func (a *loginAuth) Next(prompt []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(prompt))) {
	case "username:", "user name:", "user:":
		return []byte(a.user), nil
	case "password:":
		return []byte(a.pass), nil
	}
	return nil, fmt.Errorf("неожиданный запрос LOGIN: %q", prompt)
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // источник случайности недоступен: продолжать небезопасно
	}
	return hex.EncodeToString(b)
}

// build собирает письмо: multipart/alternative (текст и HTML), заголовки в UTF-8, тело quoted-printable.
func (s *SMTP) build(to string, m Message) ([]byte, error) {
	from := (&mail.Address{Name: s.cfg.FromName, Address: s.cfg.From}).String()
	domain := s.cfg.From[strings.LastIndex(s.cfg.From, "@")+1:]
	var b bytes.Buffer
	h := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	h("From", from)
	h("To", (&mail.Address{Address: to}).String())
	h("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	h("Date", time.Now().Format(time.RFC1123Z))
	h("Message-ID", "<"+randHex(12)+"@"+domain+">")
	h("MIME-Version", "1.0")
	h("Auto-Submitted", "auto-generated")
	h("X-Auto-Response-Suppress", "All")

	if m.HTML == "" {
		h("Content-Type", `text/plain; charset="utf-8"`)
		h("Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		if err := writeQP(&b, m.Text); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}
	mw := multipart.NewWriter(&b)
	h("Content-Type", `multipart/alternative; boundary="`+mw.Boundary()+`"`)
	b.WriteString("\r\n")
	for _, part := range []struct{ ctype, body string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		pw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.ctype + `; charset="utf-8"`},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		if err := writeQP(pw, part.body); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeQP(w interface{ Write([]byte) (int, error) }, s string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(s)); err != nil {
		return err
	}
	return qp.Close()
}

// ---- отправитель в папку (разработка и тесты) ----

// Spool «отправляет» письма файлами в каталог: для разработки и сквозных тестов, когда настоящего SMTP нет.
// Каждое письмо — отдельный файл .eml (то, что ушло бы в DATA).
type Spool struct {
	Dir  string
	From string
	s    *SMTP
}

// NewSpool создаёт отправитель в папку.
func NewSpool(dir, from string) (*Spool, error) {
	s, err := NewSMTP(Config{Host: "localhost", TLS: TLSNone, From: from})
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Spool{Dir: dir, From: from, s: s}, nil
}

func (p *Spool) Send(_ context.Context, m Message) error {
	rcpt, err := mail.ParseAddress(m.To)
	if err != nil || !cleanHeader(m.To) {
		return &Error{Err: fmt.Errorf("недопустимый адрес получателя %q", m.To)}
	}
	body, err := p.s.build(rcpt.Address, m)
	if err != nil {
		return &Error{Err: err}
	}
	// Папку могли удалить, пока сервер работает (например, очистка перед прогоном тестов): создаём заново при каждой записи.
	if err := os.MkdirAll(p.Dir, 0o750); err != nil {
		return err
	}
	name := fmt.Sprintf("%d-%s.eml", time.Now().UnixNano(), randHex(3))
	return os.WriteFile(filepath.Join(p.Dir, name), body, 0o640)
}

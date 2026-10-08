package mailer

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTP — минимальный SMTP-сервер для проверки клиента: запоминает, что ему прислали.
type fakeSMTP struct {
	t        *testing.T
	ln       net.Listener
	cert     tls.Certificate
	pool     *x509.CertPool
	implicit bool     // TLS с первого байта
	starttls bool     // объявлять STARTTLS
	auth     []string // объявляемые механизмы AUTH
	failAuth bool
	rcptCode string // ответ на RCPT TO (по умолчанию 250)
	dataCode string // ответ после DATA
	greeting string

	mu       sync.Mutex
	mails    []received
	cleartxt bool // пароль пришёл до шифрования
}

type received struct {
	from, to string
	data     string
	user     string
	pass     string
	mech     string
	tls      bool
}

func selfSigned(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

func newFake(t *testing.T, mut func(*fakeSMTP)) *fakeSMTP {
	t.Helper()
	cert, pool := selfSigned(t)
	f := &fakeSMTP{t: t, cert: cert, pool: pool, starttls: true, auth: []string{"PLAIN", "LOGIN"}, greeting: "220 fake ESMTP"}
	if mut != nil {
		mut(f)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.ln = ln
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) config(tlsMode string) Config {
	return Config{Host: "127.0.0.1", Port: f.port(), User: "robot", Password: "s3cret", From: "noreply@vladinc.ru", FromName: "Vladhost",
		TLS: tlsMode, RootCAs: f.pool, Timeout: 5 * time.Second}
}

func (f *fakeSMTP) serve(raw net.Conn) {
	defer func() { _ = raw.Close() }()
	conn := raw
	secure := false
	if f.implicit {
		conn = tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{f.cert}})
		secure = true
	}
	r := bufio.NewReader(conn)
	say := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
	say(f.greeting)
	var cur received
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		up := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
			say("250-fake")
			if f.starttls && !secure {
				say("250-STARTTLS")
			}
			if len(f.auth) > 0 {
				say("250-AUTH " + strings.Join(f.auth, " "))
			}
			say("250 8BITMIME")
		case up == "STARTTLS":
			say("220 go ahead")
			conn = tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{f.cert}})
			r = bufio.NewReader(conn)
			secure = true
		case strings.HasPrefix(up, "AUTH PLAIN"):
			payload := strings.TrimSpace(line[len("AUTH PLAIN"):])
			if payload == "" {
				say("334 ")
				payload, _ = r.ReadString('\n')
				payload = strings.TrimSpace(payload)
			}
			dec, _ := base64.StdEncoding.DecodeString(payload)
			parts := strings.Split(string(dec), "\x00")
			if len(parts) == 3 {
				cur.user, cur.pass = parts[1], parts[2]
			}
			cur.mech = "PLAIN"
			f.finishAuth(say, &cur, secure)
		case up == "AUTH LOGIN":
			say("334 " + base64.StdEncoding.EncodeToString([]byte("Username:")))
			u, _ := r.ReadString('\n')
			say("334 " + base64.StdEncoding.EncodeToString([]byte("Password:")))
			p, _ := r.ReadString('\n')
			ud, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(u))
			pd, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(p))
			cur.user, cur.pass, cur.mech = string(ud), string(pd), "LOGIN"
			f.finishAuth(say, &cur, secure)
		case strings.HasPrefix(up, "MAIL FROM:"):
			cur.from = strings.Trim(strings.SplitN(line[len("MAIL FROM:"):], ">", 2)[0], "<> ") // после адреса могут идти параметры (BODY=8BITMIME)
			say("250 ok")
		case strings.HasPrefix(up, "RCPT TO:"):
			cur.to = strings.Trim(line[len("RCPT TO:"):], "<> ")
			if f.rcptCode != "" {
				say(f.rcptCode)
			} else {
				say("250 ok")
			}
		case up == "DATA":
			say("354 go")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			if f.dataCode != "" {
				say(f.dataCode)
				continue
			}
			cur.data, cur.tls = b.String(), secure
			f.mu.Lock()
			f.mails = append(f.mails, cur)
			f.mu.Unlock()
			cur = received{user: cur.user, pass: cur.pass, mech: cur.mech}
			say("250 queued")
		case up == "QUIT":
			say("221 bye")
			return
		default:
			say("502 unknown")
		}
	}
}

func (f *fakeSMTP) finishAuth(say func(string), cur *received, secure bool) {
	if !secure {
		f.mu.Lock()
		f.cleartxt = true
		f.mu.Unlock()
	}
	if f.failAuth || cur.pass != "s3cret" {
		say("535 authentication failed")
		return
	}
	say("235 ok")
}

func (f *fakeSMTP) got() []received {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]received(nil), f.mails...)
}

// parse разбирает то, что ушло в DATA.
func parse(t *testing.T, data string) (*mail.Message, string, string) {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(data))
	if err != nil {
		t.Fatalf("письмо не разбирается: %v\n%s", err, data)
	}
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	if mt == "multipart/alternative" {
		var text, html string
		mr := multipart.NewReader(msg.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(quotedprintable.NewReader(p))
			if strings.HasPrefix(p.Header.Get("Content-Type"), "text/html") {
				html = string(b)
			} else {
				text = string(b)
			}
		}
		return msg, text, html
	}
	b, _ := io.ReadAll(quotedprintable.NewReader(msg.Body))
	return msg, string(b), ""
}

func send(t *testing.T, cfg Config, m Message) error {
	t.Helper()
	s, err := NewSMTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s.Send(context.Background(), m)
}

var hello = Message{To: "Иван <ivan@example.com>", Subject: "Проверка связи: тема с кириллицей", Text: "Привет!\nВторая строка\n.точка в начале", HTML: "<p>Привет, <b>мир</b></p>"}

func TestSendStartTLSWithPlainAuth(t *testing.T) {
	f := newFake(t, nil)
	if err := send(t, f.config(TLSStartTLS), hello); err != nil {
		t.Fatal(err)
	}
	got := f.got()
	if len(got) != 1 {
		t.Fatalf("писем %d", len(got))
	}
	r := got[0]
	if r.from != "noreply@vladinc.ru" || r.to != "ivan@example.com" || r.user != "robot" || r.pass != "s3cret" || r.mech != "PLAIN" || !r.tls {
		t.Fatalf("%+v", r)
	}
	if f.cleartxt {
		t.Fatal("пароль ушёл до включения шифрования")
	}
	msg, text, html := parse(t, r.data)
	subj, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subj != hello.Subject {
		t.Fatalf("тема: %q", subj)
	}
	if !strings.Contains(text, "Привет!") || !strings.Contains(text, "\n.точка в начале") || !strings.Contains(html, "<b>мир</b>") {
		t.Fatalf("тексты: %q %q", text, html)
	}
	from, _ := mail.ParseAddress(msg.Header.Get("From"))
	if from.Address != "noreply@vladinc.ru" || from.Name != "Vladhost" || msg.Header.Get("Message-ID") == "" || msg.Header.Get("Date") == "" {
		t.Fatalf("заголовки: %v", msg.Header)
	}
	if msg.Header.Get("Auto-Submitted") != "auto-generated" {
		t.Fatal("автоматическое письмо должно быть помечено")
	}
}

func TestSendImplicitTLSAndLoginAuth(t *testing.T) {
	f := newFake(t, func(f *fakeSMTP) { f.implicit, f.starttls, f.auth = true, false, []string{"LOGIN"} })
	if err := send(t, f.config(TLSImplicit), hello); err != nil {
		t.Fatal(err)
	}
	got := f.got()
	if len(got) != 1 || got[0].mech != "LOGIN" || got[0].user != "robot" || got[0].pass != "s3cret" || !got[0].tls {
		t.Fatalf("%+v", got)
	}
}

func TestPlainTextOnlyMessage(t *testing.T) {
	f := newFake(t, nil)
	if err := send(t, f.config(TLSStartTLS), Message{To: "a@example.com", Subject: "s", Text: "только текст"}); err != nil {
		t.Fatal(err)
	}
	_, text, html := parse(t, f.got()[0].data)
	if strings.TrimSpace(text) != "только текст" || html != "" { // SMTP добавляет перевод строки в конце
		t.Fatalf("%q %q", text, html)
	}
}

func TestRefusesToSendPasswordWithoutTLS(t *testing.T) {
	f := newFake(t, func(f *fakeSMTP) { f.starttls = false })
	err := send(t, f.config(TLSStartTLS), hello)
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("сервер без STARTTLS должен отклоняться: %v", err)
	}
	if f.cleartxt || len(f.got()) != 0 {
		t.Fatal("пароль или письмо ушли без шифрования")
	}
	if IsTemporary(err) {
		t.Fatal("отсутствие STARTTLS — не временная ошибка")
	}
}

func TestNoneTLSOnlyForLoopback(t *testing.T) {
	if _, err := NewSMTP(Config{Host: "smtp.majordomo.ru", TLS: TLSNone, From: "a@b.ru"}); err == nil {
		t.Fatal("отправка без шифрования на внешний сервер должна быть запрещена")
	}
	f := newFake(t, func(f *fakeSMTP) { f.starttls = false })
	if err := send(t, f.config(TLSNone), hello); err != nil {
		t.Fatalf("локальный сервер без шифрования: %v", err)
	}
}

func TestConfigValidation(t *testing.T) {
	bad := []Config{
		{}, {Host: "h"}, {Host: "h", From: "not an address"}, {Host: "h\r\nX: y", From: "a@b.ru"},
		{Host: "h", From: "a@b.ru", TLS: "ssl3"}, {Host: "h", From: "a@b.ru", FromName: "x\r\nBcc: e@e.ru"}, {Host: "h", From: "a@b.ru", User: "u\nx"},
	}
	for i, c := range bad {
		if _, err := NewSMTP(c); err == nil {
			t.Errorf("%d: %+v принят", i, c)
		}
	}
	s, err := NewSMTP(Config{Host: "smtp.majordomo.ru", From: "Robot <noreply@vladinc.ru>"})
	if err != nil || s.cfg.Port != 587 || s.cfg.TLS != TLSStartTLS || s.cfg.From != "noreply@vladinc.ru" {
		t.Fatalf("умолчания: %+v %v", s, err)
	}
	if s, _ := NewSMTP(Config{Host: "h", From: "a@b.ru", TLS: TLSImplicit}); s.cfg.Port != 465 {
		t.Fatal("порт по умолчанию для tls")
	}
}

func TestHeaderInjectionIsRejected(t *testing.T) {
	f := newFake(t, nil)
	for _, m := range []Message{
		{To: "a@example.com\r\nBcc: evil@example.com", Subject: "s", Text: "t"},
		{To: "a@example.com", Subject: "s\r\nBcc: evil@example.com", Text: "t"},
		{To: "a@example.com\nRCPT TO:<evil@example.com>", Subject: "s", Text: "t"},
		{To: "not-an-address", Subject: "s", Text: "t"},
		{To: "", Subject: "s", Text: "t"},
	} {
		if err := send(t, f.config(TLSStartTLS), m); err == nil {
			t.Errorf("%+v принято", m)
		}
	}
	if len(f.got()) != 0 {
		t.Fatalf("письма ушли: %+v", f.got())
	}
}

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*fakeSMTP)
		temp bool
	}{
		{"неверный пароль", func(f *fakeSMTP) { f.failAuth = true }, false},
		{"получатель отклонён окончательно", func(f *fakeSMTP) { f.rcptCode = "550 no such user" }, false},
		{"получатель временно отклонён", func(f *fakeSMTP) { f.rcptCode = "451 try later" }, true},
		{"сервер перегружен", func(f *fakeSMTP) { f.greeting = "421 busy" }, true},
		{"письмо не принято", func(f *fakeSMTP) { f.dataCode = "554 spam" }, false},
		{"нет механизмов авторизации", func(f *fakeSMTP) { f.auth = []string{"CRAM-MD5"} }, false},
	}
	for _, c := range cases {
		f := newFake(t, c.mut)
		err := send(t, f.config(TLSStartTLS), hello)
		if err == nil {
			t.Errorf("%s: ошибки нет", c.name)
			continue
		}
		if IsTemporary(err) != c.temp {
			t.Errorf("%s: temporary=%v, ожидали %v (%v)", c.name, IsTemporary(err), c.temp, err)
		}
		if len(f.got()) != 0 {
			t.Errorf("%s: письмо ушло", c.name)
		}
	}
	// Сервера нет: сеть — ошибка временная.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	err := send(t, Config{Host: "127.0.0.1", Port: port, From: "a@b.ru", TLS: TLSNone, Timeout: time.Second}, hello)
	if err == nil || !IsTemporary(err) {
		t.Fatalf("недоступный сервер: %v", err)
	}
}

func TestTimeoutDoesNotHang(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c // молчит: сервер принял соединение и ничего не отвечает
		}
	}()
	s, _ := NewSMTP(Config{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, From: "a@b.ru", TLS: TLSNone, Timeout: 300 * time.Millisecond})
	start := time.Now()
	err = s.Send(context.Background(), hello)
	if err == nil || time.Since(start) > 3*time.Second || !IsTemporary(err) {
		t.Fatalf("молчащий сервер: %v за %v", err, time.Since(start))
	}
}

func TestContextCancelStopsSending(t *testing.T) {
	f := newFake(t, nil)
	s, _ := NewSMTP(f.config(TLSStartTLS))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Send(ctx, hello); err == nil {
		t.Fatal("отменённый контекст должен прерывать отправку")
	}
}

func TestSpoolWritesEml(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mail")
	sp, err := NewSpool(dir, "noreply@vladinc.ru")
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Send(context.Background(), hello); err != nil {
		t.Fatal(err)
	}
	if err := sp.Send(context.Background(), Message{To: "bad\r\naddr", Subject: "s", Text: "t"}); err == nil {
		t.Fatal("недопустимый адрес в папку не пишется")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || !strings.HasSuffix(ents[0].Name(), ".eml") {
		t.Fatalf("%v", ents)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ents[0].Name()))
	if _, text, _ := parse(t, string(data)); !strings.Contains(text, "Привет!") {
		t.Fatalf("%q", data)
	}
}

func TestSpoolRecreatesRemovedFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mail")
	sp, err := NewSpool(dir, "noreply@vladinc.ru")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := sp.Send(context.Background(), hello); err != nil {
		t.Fatalf("папку удалили после старта: %v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("%v", ents)
	}
}

package mail

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
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"kupol/internal/i18n"
)

// fakeSMTP — минимальный SMTP-сервер (EHLO, STARTTLS с самоподписанным сертификатом, AUTH PLAIN/LOGIN, MAIL/RCPT/DATA,
// QUIT) для проверки SMTPSender без сети: настоящий протокол на настоящем сокете, а не мок клиента.
type fakeSMTP struct {
	ln       net.Listener
	cert     tls.Certificate
	authList string // что сервер объявляет в "250-AUTH …" после STARTTLS
	received chan string
}

func generateTestCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func startFakeSMTP(t *testing.T, authList string) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{ln: ln, cert: generateTestCert(t), authList: authList, received: make(chan string, 4)}
	go s.serve(t)
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeSMTP) addr() (host, port string) {
	host, port, err := net.SplitHostPort(s.ln.Addr().String())
	if err != nil {
		panic(err)
	}
	return host, port
}

func (s *fakeSMTP) serve(t *testing.T) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(t, conn)
	}
}

func (s *fakeSMTP) handle(t *testing.T, conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	send := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	send("220 fake.smtp ready")
	tlsOn := false
	authingLogin := 0 // 0 — нет, 1 — ждём Username, 2 — ждём Password
	inData := false
	var body strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if inData {
			if line == "." {
				inData = false
				s.received <- body.String()
				send("250 OK: queued")
				continue
			}
			body.WriteString(line)
			body.WriteString("\n")
			continue
		}
		if authingLogin > 0 {
			decoded, _ := base64.StdEncoding.DecodeString(line)
			_ = decoded // фальшивому серверу не важно, что там: PlainAuth/loginAuth это уже проверяют на клиенте
			if authingLogin == 1 {
				send("334 " + base64.StdEncoding.EncodeToString([]byte("Password:")))
				authingLogin = 2
			} else {
				send("235 OK")
				authingLogin = 0
			}
			continue
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			send("250-fake.smtp")
			if !tlsOn {
				send("250-STARTTLS")
				send("250 8BITMIME")
			} else {
				send("250-AUTH " + s.authList)
				send("250 8BITMIME")
			}
		case upper == "STARTTLS":
			send("220 go ahead")
			tconn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}})
			if err := tconn.Handshake(); err != nil {
				return
			}
			conn = tconn
			r = bufio.NewReader(conn)
			tlsOn = true
		case strings.HasPrefix(upper, "AUTH LOGIN"):
			send("334 " + base64.StdEncoding.EncodeToString([]byte("Username:")))
			authingLogin = 1
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			send("235 OK")
		case strings.HasPrefix(upper, "MAIL FROM"):
			send("250 OK")
		case strings.HasPrefix(upper, "RCPT TO"):
			send("250 OK")
		case upper == "DATA":
			send("354 send it")
			inData = true
		case upper == "QUIT":
			send("221 bye")
			return
		default:
			send("500 unrecognized")
		}
	}
}

func TestSMTPSenderDeliversMultipartMessage(t *testing.T) {
	fake := startFakeSMTP(t, "PLAIN LOGIN")
	host, port := fake.addr()
	sender := &SMTPSender{Host: host, Port: port, Username: "user", Password: "pass", From: "kupol@example.org", FromName: "КУПОЛ", insecureSkipVerify: true}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msg := Message{To: "reader@example.org", Subject: "Тест", HTML: "<p>привет</p>", Text: "привет"}
	if err := sender.Send(ctx, msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case raw := <-fake.received:
		if !strings.Contains(raw, "multipart/alternative") {
			t.Errorf("нет multipart/alternative: %s", raw)
		}
		if !strings.Contains(raw, "text/plain") || !strings.Contains(raw, "text/html") {
			t.Errorf("нет обеих частей: %s", raw)
		}
		if !strings.Contains(raw, "=D0=BF=D1=80=D0=B8=D0=B2=D0=B5=D1=82") { // «привет» в quoted-printable
			t.Errorf("тело не в quoted-printable или потеряно: %s", raw)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("письмо не дошло до сервера")
	}
}

// TestSMTPSenderFallsBackToLoginAuth — сервер, объявляющий только LOGIN (не PLAIN), как в тикете, вызвавшем эту правку
// (Gmail ответил «504 … PLAIN authentication mechanism not supported»): раньше SMTPSender всегда пытался PLAIN и падал.
func TestSMTPSenderFallsBackToLoginAuth(t *testing.T) {
	fake := startFakeSMTP(t, "LOGIN")
	host, port := fake.addr()
	sender := &SMTPSender{Host: host, Port: port, Username: "user", Password: "pass", From: "kupol@example.org", FromName: "КУПОЛ", insecureSkipVerify: true}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sender.Send(ctx, Message{To: "reader@example.org", Subject: "Тест", HTML: "<p>x</p>", Text: "x"}); err != nil {
		t.Fatalf("Send с сервером, объявляющим только LOGIN: %v", err)
	}
	select {
	case <-fake.received:
	case <-time.After(3 * time.Second):
		t.Fatal("письмо не дошло до сервера")
	}
}

// TestSMTPSenderRequiresSTARTTLS — сервер без STARTTLS отклоняется до попытки авторизации (не шлём пароль открытым текстом).
func TestSMTPSenderRequiresSTARTTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("220 fake.smtp ready\r\n"))
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(strings.ToUpper(line), "EHLO") {
				_, _ = conn.Write([]byte("250-fake.smtp\r\n250 8BITMIME\r\n")) // без STARTTLS
				continue
			}
			_, _ = conn.Write([]byte("221 bye\r\n"))
			return
		}
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	sender := &SMTPSender{Host: host, Port: port, Username: "user", Password: "pass", From: "a@example.org"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = sender.Send(ctx, Message{To: "reader@example.org", Subject: "x", HTML: "x", Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("ожидалась ошибка про отсутствие STARTTLS, получено: %v", err)
	}
}

func TestChooseMechanism(t *testing.T) {
	cases := []struct{ advertised, want string }{
		{"", "PLAIN"},
		{"PLAIN LOGIN", "PLAIN"},
		{"LOGIN PLAIN", "PLAIN"},
		{"LOGIN", "LOGIN"},
		{"LOGIN XOAUTH2", "LOGIN"},
		{"XOAUTH2", ""},
	}
	for _, c := range cases {
		if got := chooseMechanism(c.advertised); got != c.want {
			t.Errorf("chooseMechanism(%q) = %q, хотели %q", c.advertised, got, c.want)
		}
	}
}

func TestEmailConfirmationTemplate(t *testing.T) {
	msg := EmailConfirmation(i18n.RU, "vladik", "https://kupol.vladinc.ru/email-confirm?token=abc")
	if !strings.Contains(msg.HTML, "https://kupol.vladinc.ru/email-confirm?token=abc") {
		t.Errorf("нет ссылки подтверждения в HTML: %s", msg.HTML)
	}
	if !strings.Contains(msg.HTML, "vladik") {
		t.Errorf("нет логина в HTML: %s", msg.HTML)
	}
	if !strings.Contains(msg.Text, "https://kupol.vladinc.ru/email-confirm?token=abc") {
		t.Errorf("нет ссылки в текстовой версии: %s", msg.Text)
	}
	if msg.Subject == "" {
		t.Error("пустая тема письма")
	}

	it := EmailConfirmation(i18n.IT, "vladik", "https://kupol.vladinc.ru/email-confirm?token=abc")
	if it.Subject == msg.Subject {
		t.Error("итальянская тема совпадает с русской")
	}
}

func TestLevelUpTemplate(t *testing.T) {
	msg := LevelUp(i18n.RU, "vladik", "Стажёр", 2)
	if !strings.Contains(msg.HTML, "Стажёр") || !strings.Contains(msg.HTML, "vladik") {
		t.Errorf("нет звания или логина: %s", msg.HTML)
	}
	if strings.Contains(msg.HTML, "ButtonURL") || strings.Contains(msg.Text, "href") {
		t.Error("кнопки быть не должно (нет ссылки)")
	}
}

package mail_test

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"kupol/internal/i18n"
	"kupol/internal/mail"
)

// fakeSMTP — минимальный SMTP-сервер (EHLO, STARTTLS, AUTH PLAIN, MAIL/RCPT/DATA, QUIT) для проверки SMTPSender
// без сети: настоящее поведение реального сервера нам не нужно, нужен настоящий протокол на настоящем сокете.
type fakeSMTP struct {
	ln       net.Listener
	received chan string // сырое тело письма (после DATA)
}

func startFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{ln: ln, received: make(chan string, 4)}
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
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			send("250-fake.smtp")
			send("250-AUTH PLAIN")
			send("250 8BITMIME")
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
	fake := startFakeSMTP(t)
	host, port := fake.addr()
	sender := mail.NewSMTPSender(host, port, "user", "pass", "kupol@example.org", "КУПОЛ")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msg := mail.Message{To: "reader@example.org", Subject: "Тест", HTML: "<p>привет</p>", Text: "привет"}
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

func TestEmailConfirmationTemplate(t *testing.T) {
	msg := mail.EmailConfirmation(i18n.RU, "vladik", "https://kupol.vladinc.ru/email-confirm?token=abc")
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

	it := mail.EmailConfirmation(i18n.IT, "vladik", "https://kupol.vladinc.ru/email-confirm?token=abc")
	if it.Subject == msg.Subject {
		t.Error("итальянская тема совпадает с русской")
	}
}

func TestLevelUpTemplate(t *testing.T) {
	msg := mail.LevelUp(i18n.RU, "vladik", "Стажёр", 2)
	if !strings.Contains(msg.HTML, "Стажёр") || !strings.Contains(msg.HTML, "vladik") {
		t.Errorf("нет звания или логина: %s", msg.HTML)
	}
	if strings.Contains(msg.HTML, "ButtonURL") || strings.Contains(msg.Text, "href") {
		t.Error("кнопки быть не должно (нет ссылки)")
	}
}

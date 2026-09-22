// Package mail — письма читателям по SMTP: подтверждение почты, уведомления (повышение уровня).
//
// Без сторонних зависимостей — стандартный net/smtp с STARTTLS, тело — multipart/alternative
// (текст и HTML вместе: клиент сам выбирает, что показать). Текст письма собирает template.go
// через internal/i18n — тем же способом, что и остальные тексты сервера для читателя.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"time"
)

// Message — письмо одному получателю.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// Sender — отправка письма. SMTPSender — по-настоящему; вызывающий код сам решает, что делать,
// если почта не настроена (accounts.Service просто не отправляет и пишет в лог).
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// SMTPSender — отправка через SMTP-сервер с STARTTLS (порт 587) или прямым TLS (порт 465).
type SMTPSender struct {
	Host, Port         string
	Username, Password string
	From, FromName     string
}

// NewSMTPSender собирает отправителя из настроек (config.SMTP).
func NewSMTPSender(host, port, username, password, from, fromName string) *SMTPSender {
	return &SMTPSender{Host: host, Port: port, Username: username, Password: password, From: from, FromName: fromName}
}

func (s *SMTPSender) dial(ctx context.Context) (*smtp.Client, error) {
	addr := net.JoinHostPort(s.Host, s.Port)
	d := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("mail: подключение к %s: %w", addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	// Порт 465 — TLS сразу; остальные (587, 25) — STARTTLS после EHLO, если сервер его предлагает.
	if s.Port == "465" {
		conn = tls.Client(conn, &tls.Config{ServerName: s.Host})
	}
	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("mail: SMTP-приветствие: %w", err)
	}
	if s.Port != "465" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
				_ = client.Close()
				return nil, fmt.Errorf("mail: STARTTLS: %w", err)
			}
		}
	}
	if s.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("mail: авторизация: %w", err)
		}
	}
	return client, nil
}

// Send отправляет письмо; ctx задаёт таймаут всей операции (соединение, TLS, авторизация, передача).
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	client, err := s.dial(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Mail(s.From); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("mail: RCPT TO: %w", err)
	}
	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if _, err := wc.Write(buildRaw(s.From, s.FromName, msg)); err != nil {
		_ = wc.Close()
		return fmt.Errorf("mail: передача письма: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("mail: завершение письма: %w", err)
	}
	return client.Quit()
}

const boundary = "kupol-mail-boundary"

// buildRaw собирает письмо: заголовки (RFC 2047 для нелатинских Subject/From) и multipart/alternative
// тело (text/plain и text/html, каждая часть — quoted-printable, чтобы кириллица не терялась и не раздувала размер).
func buildRaw(from, fromName string, msg Message) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s <%s>\r\n", mime.QEncoding.Encode("utf-8", fromName), from)
	fmt.Fprintf(&b, "To: %s\r\n", msg.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", msg.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-Id: <%d.%s@kupol>\r\n", time.Now().UnixNano(), randomToken())
	b.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)

	writePart := func(contentType, body string) {
		fmt.Fprintf(&b, "--%s\r\n", boundary)
		fmt.Fprintf(&b, "Content-Type: %s; charset=utf-8\r\n", contentType)
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		qp := quotedprintable.NewWriter(&b)
		_, _ = qp.Write([]byte(body))
		_ = qp.Close()
		b.WriteString("\r\n")
	}
	writePart("text/plain", msg.Text)
	writePart("text/html", msg.HTML)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes()
}

func randomToken() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

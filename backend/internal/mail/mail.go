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
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"slices"
	"strings"
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
	// insecureSkipVerify — только для тестов (свой сертификат фальшивого сервера): наружу не выставлено,
	// NewSMTPSender его не задаёт, поэтому боевой код сертификат всегда проверяет.
	insecureSkipVerify bool
}

func (s *SMTPSender) tlsConfig() *tls.Config {
	return &tls.Config{ServerName: s.Host, InsecureSkipVerify: s.insecureSkipVerify} //nolint:gosec // см. поле выше
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
		conn = tls.Client(conn, s.tlsConfig())
	}
	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("mail: SMTP-приветствие: %w", err)
	}
	if s.Port != "465" {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			_ = client.Close()
			return nil, fmt.Errorf("mail: сервер %s не предлагает STARTTLS — почту нельзя отправить без шифрования, проверьте KUPOL_SMTP_PORT", s.Host)
		}
		if err := client.StartTLS(s.tlsConfig()); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("mail: STARTTLS: %w", err)
		}
	}
	if s.Username != "" {
		auth, err := s.chooseAuth(client)
		if err != nil {
			_ = client.Close()
			return nil, err
		}
		if err := client.Auth(auth); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("mail: авторизация: %w", err)
		}
	}
	return client, nil
}

// chooseAuth строит smtp.Auth по тому, что сервер объявил в EHLO (см. chooseMechanism).
func (s *SMTPSender) chooseAuth(client *smtp.Client) (smtp.Auth, error) {
	_, mechanisms := client.Extension("AUTH")
	switch chooseMechanism(mechanisms) {
	case "PLAIN":
		return smtp.PlainAuth("", s.Username, s.Password, s.Host), nil
	case "LOGIN":
		return loginAuth{username: s.Username, password: s.Password}, nil
	default:
		return nil, fmt.Errorf("mail: сервер %s поддерживает только %s — ни PLAIN, ни LOGIN не в списке", s.Host, mechanisms)
	}
}

// chooseMechanism решает, каким способом авторизоваться, по строке AUTH из EHLO ("PLAIN LOGIN", "LOGIN XOAUTH2"…):
// PLAIN, если он в списке, иначе LOGIN (net/smtp своей реализации LOGIN не даёт — некоторые серверы вроде Gmail на
// части путей объявляют только его; PLAIN на них отвечает «504 … not supported», как в тикете, вызвавшем эту правку).
// Пустой список (сервер не объявил AUTH явно, что иногда бывает до STARTTLS у почтовых релеев) — тоже PLAIN: так было
// раньше и большинство серверов его принимают.
func chooseMechanism(advertised string) string {
	list := strings.Fields(strings.ToUpper(advertised))
	switch {
	case len(list) == 0, slices.Contains(list, "PLAIN"):
		return "PLAIN"
	case slices.Contains(list, "LOGIN"):
		return "LOGIN"
	default:
		return ""
	}
}

// loginAuth — AUTH LOGIN (RFC 4954), которого нет в net/smtp: некоторые серверы предлагают только его, не PLAIN.
type loginAuth struct{ username, password string }

func (a loginAuth) Start(*smtp.ServerInfo) (string, []byte, error) { return "LOGIN", nil, nil }

func (a loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.TrimSuffix(string(fromServer), ":") {
	case "Username":
		return []byte(a.username), nil
	case "Password":
		return []byte(a.password), nil
	default:
		return nil, errors.New("mail: неожиданный запрос сервера при AUTH LOGIN: " + string(fromServer))
	}
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

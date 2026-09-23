package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTPSender sends email via a standard SMTP server — Hostinger, Gmail,
// Zoho, Amazon SES SMTP, or any other provider that speaks plain SMTP
// with STARTTLS (typically port 587) or implicit TLS (typically port
// 465). Only the Go standard library is used here (net/smtp, crypto/tls)
// so this needs no extra dependency for whichever mailbox is configured.
type SMTPSender struct {
	host     string
	port     string
	username string
	password string
	fromAddr string
	fromName string
}

// NewSMTPSender constructs a Sender backed by a standard SMTP server.
// fromAddr is also used as the SMTP envelope sender (MAIL FROM); most
// providers, Hostinger included, require it to match the authenticated
// mailbox.
func NewSMTPSender(host, port, username, password, fromAddr, fromName string) *SMTPSender {
	return &SMTPSender{
		host:     host,
		port:     port,
		username: username,
		password: password,
		fromAddr: fromAddr,
		fromName: fromName,
	}
}

func (s *SMTPSender) Send(ctx context.Context, to, subject, body string) error {
	addr := net.JoinHostPort(s.host, s.port)
	auth := smtp.PlainAuth("", s.username, s.password, s.host)

	from := s.fromAddr
	if s.fromName != "" {
		from = fmt.Sprintf("%s <%s>", s.fromName, s.fromAddr)
	}
	msg := []byte(buildMessage(from, to, subject, body))

	timeout := 15 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 {
			timeout = remaining
		}
	}

	// Port 465 is implicit TLS (the connection is TLS from the first
	// byte); everything else (587, 25, custom ports) is plaintext with an
	// opportunistic STARTTLS upgrade, which is what almost every provider
	// — Hostinger included — expects on 587.
	if s.port == "465" {
		return s.sendImplicitTLS(addr, auth, to, msg, timeout)
	}
	return s.sendStartTLS(addr, auth, to, msg, timeout)
}

func (s *SMTPSender) sendStartTLS(addr string, auth smtp.Auth, to string, msg []byte, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return fmt.Errorf("email: failed to dial smtp server: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("email: failed to initialize smtp client: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: s.host}); err != nil {
			return fmt.Errorf("email: starttls upgrade failed: %w", err)
		}
	}

	return s.deliver(client, auth, to, msg)
}

func (s *SMTPSender) sendImplicitTLS(addr string, auth smtp.Auth, to string, msg []byte, timeout time.Duration) error {
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.host})
	if err != nil {
		return fmt.Errorf("email: failed to dial smtp server over tls: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("email: failed to initialize smtp client: %w", err)
	}
	defer client.Close()

	return s.deliver(client, auth, to, msg)
}

func (s *SMTPSender) deliver(client *smtp.Client, auth smtp.Auth, to string, msg []byte) error {
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("email: smtp authentication failed: %w", err)
	}
	if err := client.Mail(s.fromAddr); err != nil {
		return fmt.Errorf("email: MAIL FROM rejected: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("email: RCPT TO rejected: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: DATA command failed: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("email: failed to write message body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: failed to finalize message: %w", err)
	}
	return client.Quit()
}

func buildMessage(from, to, subject, body string) string {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	b.WriteString("\r\n")
	return b.String()
}

// Package mail sends transactional email over SMTP (or logs it when SMTP is not configured).
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"time"

	"github.com/authguard/backend/internal/config"
)

type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

func New(cfg config.SMTP, log *slog.Logger) Mailer {
	if cfg.Host == "" {
		log.Warn("SMTP_HOST not set: emails will be written to the log instead of sent")
		return &logMailer{log: log}
	}
	return &smtpMailer{cfg: cfg}
}

type logMailer struct{ log *slog.Logger }

func (m *logMailer) Send(_ context.Context, msg Message) error {
	m.log.Info("email (not sent: SMTP disabled)", "to", msg.To, "subject", msg.Subject, "body", msg.Text)
	return nil
}

type smtpMailer struct{ cfg config.SMTP }

func (m *smtpMailer) Send(ctx context.Context, msg Message) error {
	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))
	var auth smtp.Auth
	if m.cfg.Username != "" {
		// PlainAuth refuses to send credentials over an unencrypted non-localhost link.
		auth = smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	}
	from, err := parseAddress(m.cfg.From)
	if err != nil {
		return err
	}
	body, err := build(m.cfg.From, msg)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- smtp.SendMail(addr, auth, from, []string{msg.To}, body) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func build(from string, msg Message) ([]byte, error) {
	rb := make([]byte, 12)
	if _, err := rand.Read(rb); err != nil {
		return nil, err
	}
	boundary := "ag-" + hex.EncodeToString(rb)
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", msg.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", msg.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@authguard>\r\n", hex.EncodeToString(rb))
	b.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", boundary, msg.Text)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=utf-8\r\n\r\n%s\r\n", boundary, msg.HTML)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes(), nil
}

func parseAddress(s string) (string, error) {
	// Accept both "Name <addr>" and bare "addr".
	if i := bytes.IndexByte([]byte(s), '<'); i >= 0 {
		if j := bytes.IndexByte([]byte(s), '>'); j > i {
			return s[i+1 : j], nil
		}
		return "", fmt.Errorf("invalid SMTP_FROM %q", s)
	}
	return s, nil
}

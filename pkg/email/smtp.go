package email

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SendSMTP sends an email via SMTP using the given config and message.
// The extraHeaders map is used for Message-ID, In-Reply-To, References, etc.
func SendSMTP(cfg *Config, msg OutgoingMessage, extraHeaders map[string]string) error {
	if cfg.SMTPServer == "" {
		return fmt.Errorf("SMTP server not configured")
	}
	if len(msg.To) == 0 {
		return fmt.Errorf("no recipients specified")
	}

	// Build the email
	var buf strings.Builder

	// Headers
	from := cfg.Address
	buf.WriteString(fmt.Sprintf("From: %s\r\n", from))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(msg.To, ", ")))
	if len(msg.CC) > 0 {
		buf.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(msg.CC, ", ")))
	}
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", encodeSubject(msg.Subject)))
	buf.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z)))
	buf.WriteString("MIME-Version: 1.0\r\n")

	// Extra headers (Message-ID, In-Reply-To, References, Reply-To)
	for key, value := range extraHeaders {
		buf.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
	}

	// Body
	if msg.HTML != "" {
		// Multipart alternative: text + HTML
		boundary := fmt.Sprintf("==astonish_%d==", time.Now().UnixNano())
		buf.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
		buf.WriteString("\r\n")

		// Text part
		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(encodeQuotedPrintable(msg.Body))
		buf.WriteString("\r\n")

		// HTML part
		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		buf.WriteString("Content-Type: text/html; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(encodeQuotedPrintable(msg.HTML))
		buf.WriteString("\r\n")

		buf.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else {
		// Plain text only
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(encodeQuotedPrintable(msg.Body))
	}

	// Collect all recipients
	recipients := make([]string, 0, len(msg.To)+len(msg.CC))
	recipients = append(recipients, msg.To...)
	recipients = append(recipients, msg.CC...)

	// Determine auth credentials
	username := cfg.Username
	if username == "" {
		username = cfg.Address
	}
	password := cfg.Password

	// Send via SMTP
	host, port, err := net.SplitHostPort(cfg.SMTPServer)
	if err != nil {
		host = cfg.SMTPServer
		port = "587"
	}

	addr := net.JoinHostPort(host, port)

	if port == "465" {
		// Implicit TLS (SMTPS)
		return sendSMTPImplicitTLS(addr, host, username, password, from, recipients, buf.String())
	}

	// STARTTLS (port 587 or 25)
	return sendSMTPStartTLS(addr, host, username, password, from, recipients, buf.String())
}

// sendSMTPStartTLS sends email using STARTTLS on port 587/25.
func sendSMTPStartTLS(addr, host, username, password, from string, to []string, body string) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("SMTP dial failed: %w", err)
	}
	defer c.Close()

	// EHLO
	if err := c.Hello("localhost"); err != nil {
		return fmt.Errorf("SMTP EHLO failed: %w", err)
	}

	// STARTTLS
	tlsCfg := &tls.Config{ServerName: host}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS failed: %w", err)
		}
	}

	// Auth
	if username != "" && password != "" {
		auth := smtp.PlainAuth("", username, password, host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth failed: %w", err)
		}
	}

	// Send
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL FROM failed: %w", err)
	}
	for _, addr := range to {
		if err := c.Rcpt(addr); err != nil {
			return fmt.Errorf("SMTP RCPT TO <%s> failed: %w", addr, err)
		}
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA failed: %w", err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		return fmt.Errorf("SMTP write body failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("SMTP close body failed: %w", err)
	}

	return c.Quit()
}

// sendSMTPImplicitTLS sends email over an implicit TLS connection (port 465).
func sendSMTPImplicitTLS(addr, host, username, password, from string, to []string, body string) error {
	tlsCfg := &tls.Config{ServerName: host}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("TLS dial failed: %w", err)
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer c.Close()

	// Auth
	if username != "" && password != "" {
		auth := smtp.PlainAuth("", username, password, host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth failed: %w", err)
		}
	}

	// Send
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL FROM failed: %w", err)
	}
	for _, addr := range to {
		if err := c.Rcpt(addr); err != nil {
			return fmt.Errorf("SMTP RCPT TO <%s> failed: %w", addr, err)
		}
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA failed: %w", err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		return fmt.Errorf("SMTP write body failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("SMTP close body failed: %w", err)
	}

	return c.Quit()
}

// encodeSubject encodes the subject line for email headers, handling non-ASCII characters.
func encodeSubject(subject string) string {
	// Check if subject is plain ASCII
	for _, r := range subject {
		if r > 127 {
			return mime.QEncoding.Encode("utf-8", subject)
		}
	}
	return subject
}

// encodeQuotedPrintable encodes text using quoted-printable encoding (RFC 2045).
// This ensures long lines are wrapped at 76 characters and non-ASCII bytes are
// properly encoded, which is required when Content-Transfer-Encoding is set to
// quoted-printable in the MIME headers.
func encodeQuotedPrintable(text string) string {
	var buf bytes.Buffer
	w := quotedprintable.NewWriter(&buf)
	_, _ = w.Write([]byte(text))
	_ = w.Close()
	return buf.String()
}

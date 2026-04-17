package email

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
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

	// Body — with optional file attachments.
	// When attachments are present, the MIME structure is:
	//   multipart/mixed
	//     multipart/alternative (text + HTML body)
	//     application/octet-stream (attachment 1)
	//     application/octet-stream (attachment 2)
	// Without attachments, the structure is the same as before:
	//   multipart/alternative (text + HTML) or plain text.
	hasAttachments := len(msg.Attachments) > 0

	if hasAttachments {
		// Outer boundary: multipart/mixed
		mixedBoundary := fmt.Sprintf("==astonish_mixed_%d==", time.Now().UnixNano())
		buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", mixedBoundary))
		buf.WriteString("\r\n")

		// First part: the message body (text or text+HTML)
		buf.WriteString(fmt.Sprintf("--%s\r\n", mixedBoundary))
		writeBodyPart(&buf, msg)

		// Attachment parts
		for _, att := range msg.Attachments {
			buf.WriteString(fmt.Sprintf("--%s\r\n", mixedBoundary))
			writeAttachmentPart(&buf, att)
		}

		buf.WriteString(fmt.Sprintf("--%s--\r\n", mixedBoundary))
	} else {
		writeBodyPart(&buf, msg)
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

// writeBodyPart writes the email body (text-only or multipart/alternative)
// as a MIME part. When used inside a multipart/mixed envelope, this becomes
// one part of the mixed message. When used standalone, it writes the
// Content-Type header directly.
func writeBodyPart(buf *strings.Builder, msg OutgoingMessage) {
	if msg.HTML != "" {
		// Multipart alternative: text + HTML
		altBoundary := fmt.Sprintf("==astonish_alt_%d==", time.Now().UnixNano())
		buf.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", altBoundary))
		buf.WriteString("\r\n")

		// Text part
		buf.WriteString(fmt.Sprintf("--%s\r\n", altBoundary))
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(encodeQuotedPrintable(msg.Body))
		buf.WriteString("\r\n")

		// HTML part
		buf.WriteString(fmt.Sprintf("--%s\r\n", altBoundary))
		buf.WriteString("Content-Type: text/html; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(encodeQuotedPrintable(msg.HTML))
		buf.WriteString("\r\n")

		buf.WriteString(fmt.Sprintf("--%s--\r\n", altBoundary))
	} else {
		// Plain text only
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(encodeQuotedPrintable(msg.Body))
	}
}

// writeAttachmentPart writes a single file attachment as a base64-encoded
// MIME part with Content-Disposition: attachment.
func writeAttachmentPart(buf *strings.Builder, att Attachment) {
	contentType := att.ContentType
	if contentType == "" {
		contentType = http.DetectContentType(att.Data)
	}

	// Encode filename for non-ASCII characters (RFC 2231)
	encodedName := mime.QEncoding.Encode("utf-8", att.Filename)

	buf.WriteString(fmt.Sprintf("Content-Type: %s; name=\"%s\"\r\n", contentType, encodedName))
	buf.WriteString("Content-Transfer-Encoding: base64\r\n")
	buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", encodedName))
	buf.WriteString("\r\n")

	// Base64-encode in 76-char lines per RFC 2045
	encoded := base64.StdEncoding.EncodeToString(att.Data)
	for len(encoded) > 76 {
		buf.WriteString(encoded[:76])
		buf.WriteString("\r\n")
		encoded = encoded[76:]
	}
	if len(encoded) > 0 {
		buf.WriteString(encoded)
		buf.WriteString("\r\n")
	}
}

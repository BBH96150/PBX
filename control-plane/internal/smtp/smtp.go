// Package smtp is a thin wrapper around net/smtp that builds multipart MIME
// messages with optional attachments. Used by the ESL voicemail handler to
// deliver recorded messages as email.
package smtp

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
)

// Mailer sends MIME messages via an SMTP relay.
//
// Auth: PlainAuth when Username is set, otherwise no auth (common for
// localhost relays).
//
// TLS: UseSTARTTLS=true issues STARTTLS after EHLO (port 587 pattern).
// Implicit-TLS (port 465) isn't supported yet — add when we hit a relay
// that requires it.
type Mailer struct {
	Host        string
	Port        int
	Username    string
	Password    string
	From        string // RFC5322 From header (e.g. "PBX <pbx@example.com>")
	UseSTARTTLS bool
}

type Attachment struct {
	Filename    string
	ContentType string // e.g. "audio/wav"
	Data        []byte
}

// Configured reports whether enough is set to attempt a send. If false,
// callers typically skip email delivery silently.
func (m Mailer) Configured() bool {
	return m.Host != "" && m.Port != 0 && m.From != ""
}

// Send delivers one message. Blocking; callers should use a goroutine.
func (m Mailer) Send(to, subject, bodyText string, attachments []Attachment) error {
	if !m.Configured() {
		return errors.New("smtp not configured")
	}
	if to == "" {
		return errors.New("empty recipient")
	}
	msg, err := buildMessage(m.From, to, subject, bodyText, attachments)
	if err != nil {
		return fmt.Errorf("build message: %w", err)
	}
	return m.send(to, msg)
}

func (m Mailer) send(to string, msg []byte) error {
	addr := net.JoinHostPort(m.Host, fmt.Sprintf("%d", m.Port))

	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer c.Close()

	if err := c.Hello("sip-platform"); err != nil {
		return fmt.Errorf("ehlo: %w", err)
	}

	if m.UseSTARTTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("starttls requested but server does not advertise it")
		}
		if err := c.StartTLS(&tls.Config{ServerName: m.Host}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if m.Username != "" {
		auth := smtp.PlainAuth("", m.Username, m.Password, m.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	if err := c.Mail(stripDisplayName(m.From)); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("RCPT TO: %w", err)
	}

	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := wc.Write(msg); err != nil {
		_ = wc.Close()
		return fmt.Errorf("write body: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("close DATA: %w", err)
	}
	return c.Quit()
}

// buildMessage assembles a multipart/mixed MIME message with a UTF-8 text
// body and the attachments. Exported only for tests (via the helper below).
func buildMessage(from, to, subject, bodyText string, attachments []Attachment) ([]byte, error) {
	var buf strings.Builder
	mw := multipart.NewWriter(&truncateWriter{w: &buf})

	// Headers
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\r\n", mw.Boundary())
	fmt.Fprintf(&buf, "\r\n")

	// Text part
	tw, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": {"7bit"},
	})
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(tw, bodyText); err != nil {
		return nil, err
	}

	// Attachments
	for _, att := range attachments {
		hdr := textproto.MIMEHeader{
			"Content-Type":              {att.ContentType + "; name=\"" + att.Filename + "\""},
			"Content-Transfer-Encoding": {"base64"},
			"Content-Disposition":       {"attachment; filename=\"" + att.Filename + "\""},
		}
		aw, err := mw.CreatePart(hdr)
		if err != nil {
			return nil, err
		}
		enc := base64.NewEncoder(base64.StdEncoding, &lineWrapWriter{w: aw, lineLen: 76})
		if _, err := enc.Write(att.Data); err != nil {
			return nil, err
		}
		if err := enc.Close(); err != nil {
			return nil, err
		}
	}

	if err := mw.Close(); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// stripDisplayName extracts the bare address from a "Name <addr@host>" form.
// Used because net/smtp's MAIL FROM wants just the address.
func stripDisplayName(s string) string {
	if i := strings.LastIndexByte(s, '<'); i >= 0 {
		if j := strings.LastIndexByte(s, '>'); j > i {
			return s[i+1 : j]
		}
	}
	return strings.TrimSpace(s)
}

// truncateWriter forwards writes to a strings.Builder; exists so that
// multipart.NewWriter can hold a stable io.Writer reference.
type truncateWriter struct{ w *strings.Builder }

func (t *truncateWriter) Write(p []byte) (int, error) { return t.w.Write(p) }

// lineWrapWriter wraps base64 output to <= lineLen bytes per line.
type lineWrapWriter struct {
	w       io.Writer
	lineLen int
	col     int
}

func (l *lineWrapWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		remain := l.lineLen - l.col
		n := len(p)
		if n > remain {
			n = remain
		}
		nn, err := l.w.Write(p[:n])
		written += nn
		if err != nil {
			return written, err
		}
		l.col += nn
		p = p[nn:]
		if l.col >= l.lineLen {
			if _, err := l.w.Write([]byte("\r\n")); err != nil {
				return written, err
			}
			l.col = 0
		}
	}
	return written, nil
}

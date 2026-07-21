package reports

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nextmap-io/as-stats/internal/config"
)

// Sender delivers rendered reports over SMTP using only the standard library.
// STARTTLS is negotiated when configured and advertised by the server; PLAIN
// auth is used when a username is set.
type Sender struct {
	cfg config.SMTPConfig
}

// NewSender builds a Sender from SMTP config.
func NewSender(cfg config.SMTPConfig) *Sender {
	return &Sender{cfg: cfg}
}

// Send delivers a rendered report to the given recipients. The `format` selects
// which MIME parts are included: "html" → HTML body only; "csv" → text body +
// CSV attachment; "both" → HTML body + CSV attachment.
func (s *Sender) Send(recipients []string, r Rendered, format string) error {
	if s.cfg.Host == "" || s.cfg.From == "" {
		return fmt.Errorf("smtp not configured (SMTP_HOST/SMTP_FROM required)")
	}
	if len(recipients) == 0 {
		return fmt.Errorf("no recipients")
	}

	msg, err := s.buildMessage(recipients, r, format)
	if err != nil {
		return fmt.Errorf("build message: %w", err)
	}

	envelopeFrom := s.cfg.From
	if addr, perr := mail.ParseAddress(s.cfg.From); perr == nil {
		envelopeFrom = addr.Address
	}

	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Hello(smtpHeloName()); err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}

	if s.cfg.STARTTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		} else {
			return fmt.Errorf("smtp server does not advertise STARTTLS but SMTP_STARTTLS=true")
		}
	}

	if s.cfg.User != "" {
		auth := smtp.PlainAuth("", s.cfg.User, s.cfg.Password, s.cfg.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := c.Mail(envelopeFrom); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	for _, rcpt := range recipients {
		to := rcpt
		if addr, perr := mail.ParseAddress(rcpt); perr == nil {
			to = addr.Address
		}
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("smtp rcpt %s: %w", to, err)
		}
	}

	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := wc.Write(msg); err != nil {
		_ = wc.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp data close: %w", err)
	}
	return c.Quit()
}

// smtpHeloName returns a best-effort EHLO/HELO name (the local hostname).
func smtpHeloName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "localhost"
}

// buildMessage assembles a multipart/mixed MIME message by hand: an HTML (or
// text) body part plus, for csv/both formats, a base64-encoded CSV attachment.
func (s *Sender) buildMessage(recipients []string, r Rendered, format string) ([]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	includeHTML := format == "html" || format == "both"
	includeCSV := format == "csv" || format == "both"
	if !includeHTML && !includeCSV {
		// Defensive: unknown format → send HTML.
		includeHTML = true
	}

	// Body part.
	if includeHTML {
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Content-Transfer-Encoding", "base64")
		pw, err := mw.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if _, err := pw.Write(base64Wrap(r.HTML)); err != nil {
			return nil, err
		}
	} else {
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Content-Transfer-Encoding", "base64")
		pw, err := mw.CreatePart(h)
		if err != nil {
			return nil, err
		}
		text := fmt.Sprintf("AS-Stats %s report. Data attached as CSV.\r\n", r.Subject)
		if _, err := pw.Write(base64Wrap([]byte(text))); err != nil {
			return nil, err
		}
	}

	// CSV attachment.
	if includeCSV {
		filename := fmt.Sprintf("as-stats-report-%s.csv", r.From.UTC().Format("20060102"))
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", "text/csv; charset=utf-8")
		h.Set("Content-Transfer-Encoding", "base64")
		h.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		pw, err := mw.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if _, err := pw.Write(base64Wrap(r.CSV)); err != nil {
			return nil, err
		}
	}

	if err := mw.Close(); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	fmt.Fprintf(&out, "From: %s\r\n", s.cfg.From)
	fmt.Fprintf(&out, "To: %s\r\n", strings.Join(recipients, ", "))
	fmt.Fprintf(&out, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", r.Subject))
	fmt.Fprintf(&out, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	out.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&out, "Content-Type: multipart/mixed; boundary=%q\r\n", mw.Boundary())
	out.WriteString("\r\n")
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

// base64Wrap base64-encodes b and wraps it at 76 columns with CRLF, as required
// for MIME base64 transfer encoding.
func base64Wrap(b []byte) []byte {
	enc := base64.StdEncoding.EncodeToString(b)
	var out bytes.Buffer
	for len(enc) > 76 {
		out.WriteString(enc[:76])
		out.WriteString("\r\n")
		enc = enc[76:]
	}
	out.WriteString(enc)
	out.WriteString("\r\n")
	return out.Bytes()
}

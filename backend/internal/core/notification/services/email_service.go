package services

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// encodeQuotedPrintable encodes s with RFC 2045 quoted-printable so that
// '=' bytes (and any non-printable / >0x7E) become '=XX' hex sequences
// and long lines are wrapped at 76 chars with soft '=' line endings.
//
// Without this, declaring Content-Transfer-Encoding: quoted-printable
// while writing the body verbatim produces invalid QP whenever the body
// contains a literal '=' (e.g. "?token=ABC"). Strict decoders — Stalwart
// among them — treat the unescaped '=' as a malformed escape and drop
// it, mangling URLs in flight.
func encodeQuotedPrintable(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.String()
}

var ErrEmailNotConfigured = errors.New("notification: email sender not configured")

// EmailSettings bundles the SMTP configuration used by the email service.
// Values come from the ConfigService (DB) with env var fallback.
type EmailSettings struct {
	Provider    string // "smtp" | "noop"
	Host        string
	Port        int
	Username    string
	Password    string
	FromAddress string
	FromName    string
	ReplyTo     string
	TLSMode     string // "starttls" | "tls" | "none"
}

// EmailMessage is a fully rendered message ready to hand to the transport.
type EmailMessage struct {
	To       string
	ToName   string
	Subject  string
	BodyText string
	BodyHTML string
}

// EmailSender is the low-level transport interface.
type EmailSender interface {
	IsConfigured() bool
	Send(ctx context.Context, msg EmailMessage) error
	ProviderName() string
}

// SettingsLoader supplies the current email settings at send time so that
// admin UI changes take effect without a restart.
type SettingsLoader func(ctx context.Context) EmailSettings

type emailService struct {
	loader SettingsLoader
	logger *slog.Logger
}

func NewEmailService(loader SettingsLoader, logger *slog.Logger) EmailSender {
	return &emailService{loader: loader, logger: logger}
}

func (s *emailService) ProviderName() string {
	if s.loader == nil {
		return "noop"
	}
	cfg := s.loader(context.Background())
	if cfg.Provider == "" {
		return "noop"
	}
	return cfg.Provider
}

func (s *emailService) IsConfigured() bool {
	cfg := s.loader(context.Background())
	return isSMTPConfigured(cfg) || cfg.Provider == "noop"
}

func isSMTPConfigured(cfg EmailSettings) bool {
	if cfg.Provider != "smtp" {
		return false
	}
	if cfg.Host == "" || cfg.Port == 0 {
		return false
	}
	if cfg.FromAddress == "" {
		return false
	}
	return true
}

func (s *emailService) Send(ctx context.Context, msg EmailMessage) error {
	cfg := s.loader(ctx)

	// Dev / bootstrap path: log and return success.
	if cfg.Provider == "noop" || cfg.Provider == "" {
		s.logger.Info("notification.email noop send",
			slog.String("to", msg.To),
			slog.String("subject", msg.Subject),
		)
		s.logger.Debug("notification.email body",
			slog.String("text", truncate(msg.BodyText, 500)),
		)
		return nil
	}

	if !isSMTPConfigured(cfg) {
		return ErrEmailNotConfigured
	}

	return s.sendSMTP(ctx, cfg, msg)
}

func (s *emailService) sendSMTP(ctx context.Context, cfg EmailSettings, msg EmailMessage) error {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error

	if cfg.TLSMode == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: cfg.Host})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Quit()

	if cfg.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		}
	}

	if cfg.Username != "" {
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(cfg.FromAddress); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}

	payload := buildMIMEMessage(cfg, msg)
	if _, err := wc.Write([]byte(payload)); err != nil {
		wc.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}

	s.logger.Info("notification.email sent",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
		slog.String("provider", "smtp"),
	)
	return nil
}

// buildMIMEMessage formats the message as multipart/alternative when both
// text and HTML bodies are provided, or text/plain when only text is.
func buildMIMEMessage(cfg EmailSettings, msg EmailMessage) string {
	var b strings.Builder

	from := cfg.FromAddress
	if cfg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", cfg.FromName, cfg.FromAddress)
	}
	to := msg.To
	if msg.ToName != "" {
		to = fmt.Sprintf("%s <%s>", msg.ToName, msg.To)
	}

	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", msg.Subject)
	if cfg.ReplyTo != "" {
		fmt.Fprintf(&b, "Reply-To: %s\r\n", cfg.ReplyTo)
	}
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")

	if msg.BodyHTML != "" {
		boundary := "orkestra_boundary_" + fmt.Sprint(time.Now().UnixNano())
		fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)

		fmt.Fprintf(&b, "--%s\r\n", boundary)
		b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		b.WriteString(encodeQuotedPrintable(msg.BodyText))
		b.WriteString("\r\n")

		fmt.Fprintf(&b, "--%s\r\n", boundary)
		b.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		b.WriteString(encodeQuotedPrintable(msg.BodyHTML))
		b.WriteString("\r\n")

		fmt.Fprintf(&b, "--%s--\r\n", boundary)
	} else {
		b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		b.WriteString(encodeQuotedPrintable(msg.BodyText))
	}

	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

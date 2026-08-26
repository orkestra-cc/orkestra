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
	"net/textproto"
	"strings"
	"time"
)

const (
	smtpDialTimeout = 15 * time.Second
	// smtpIOTimeout bounds the whole exchange when the caller's context has
	// no deadline. A relay that accepts the connection and never answers
	// would otherwise hold the send goroutine forever.
	smtpIOTimeout = 30 * time.Second
)

// SMTP steps, named by us. They are the only "op" values a diagnostic carries.
const (
	smtpOpDial     = "dial"
	smtpOpGreeting = "greeting"
	smtpOpStartTLS = "starttls"
	smtpOpAuth     = "auth"
	smtpOpMailFrom = "mail_from"
	smtpOpRcptTo   = "rcpt_to"
	smtpOpData     = "data"
	smtpOpWrite    = "write"
	smtpOpClose    = "close"
)

// smtpDriver is the pre-ADR-0019 emailService, retired inside the driver
// seam: sendSMTP, the TLS-mode handling and the quoted-printable encoding
// are unchanged; only the source of the credentials moved to the profile.
type smtpDriver struct{ logger *slog.Logger }

func NewSMTPDriver(logger *slog.Logger) EmailDriver {
	if logger == nil {
		logger = slog.Default()
	}
	return &smtpDriver{logger: logger}
}

func (d *smtpDriver) Name() string { return "smtp" }

// Requires reproduces isSMTPConfigured exactly: host, port and from — and
// NOT credentials. An unauthenticated internal relay is a supported
// configuration (D3/D6); sendSMTP authenticates only when a username is set.
func (d *smtpDriver) Requires() []ProfileRequirement {
	return []ProfileRequirement{{Key: SubSMTPHost}, {Key: SubSMTPPort}, {Key: SubFromAddress}}
}

func (d *smtpDriver) Send(ctx context.Context, p SenderProfile, msg EmailMessage) error {
	if err := ValidateProfile(d, p, RuntimeView); err != nil {
		return err
	}
	return d.sendSMTP(ctx, p, msg)
}

func (d *smtpDriver) sendSMTP(ctx context.Context, p SenderProfile, msg EmailMessage) error {
	addr := fmt.Sprintf("%s:%d", p.SMTPHost, p.SMTPPort)

	dialer := &net.Dialer{Timeout: smtpDialTimeout}
	var conn net.Conn
	var err error
	if p.SMTPTLSMode == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: p.SMTPHost})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return smtpError(smtpOpDial, err)
	}
	defer conn.Close()

	deadline := time.Now().Add(smtpIOTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	client, err := smtp.NewClient(conn, p.SMTPHost)
	if err != nil {
		return smtpError(smtpOpGreeting, err)
	}
	defer client.Quit()

	if p.SMTPTLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: p.SMTPHost}); err != nil {
				return smtpError(smtpOpStartTLS, err)
			}
		}
	}

	if p.SMTPUsername != "" {
		auth := smtp.PlainAuth("", p.SMTPUsername, p.SMTPPassword, p.SMTPHost)
		if err := client.Auth(auth); err != nil {
			return smtpError(smtpOpAuth, err)
		}
	}

	if err := client.Mail(p.FromAddress); err != nil {
		return smtpError(smtpOpMailFrom, err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return smtpError(smtpOpRcptTo, err)
	}

	wc, err := client.Data()
	if err != nil {
		return smtpError(smtpOpData, err)
	}
	if _, err := wc.Write([]byte(buildMIMEMessage(p, msg))); err != nil {
		wc.Close()
		return smtpError(smtpOpWrite, err)
	}
	if err := wc.Close(); err != nil {
		return smtpError(smtpOpClose, err)
	}

	d.logger.Info("notification.email sent",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
		slog.String("provider", "smtp"),
	)
	return nil
}

// smtpError keeps the numeric reply code of a server rejection and drops its
// text: a hostile or merely broken MTA may echo the AUTH argument —
// base64(\0user\0pass) — into a 5xx line, and *textproto.Error.Msg IS that
// line. A local failure keeps only a kind from failureKind's fixed set.
func smtpError(op string, err error) error {
	var tp *textproto.Error
	if errors.As(err, &tp) {
		return rejectionError("smtp", op, tp.Code, err)
	}
	return transportError("smtp", op, err)
}

// buildMIMEMessage formats the message as multipart/alternative when both
// text and HTML bodies are provided, or text/plain when only text is.
func buildMIMEMessage(p SenderProfile, msg EmailMessage) string {
	return buildMIMEMessageAt(p, msg, time.Now())
}

// buildMIMEMessageAt is buildMIMEMessage with the clock injected, so the
// wire output can be pinned byte for byte in tests.
func buildMIMEMessageAt(p SenderProfile, msg EmailMessage, now time.Time) string {
	var b strings.Builder

	from := p.FromAddress
	if p.FromName != "" {
		from = fmt.Sprintf("%s <%s>", p.FromName, p.FromAddress)
	}
	to := msg.To
	if msg.ToName != "" {
		to = fmt.Sprintf("%s <%s>", msg.ToName, msg.To)
	}

	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", msg.Subject)
	if p.ReplyTo != "" {
		fmt.Fprintf(&b, "Reply-To: %s\r\n", p.ReplyTo)
	}
	fmt.Fprintf(&b, "Date: %s\r\n", now.UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")

	if msg.BodyHTML != "" {
		boundary := "orkestra_boundary_" + fmt.Sprint(now.UnixNano())
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

// encodeQuotedPrintable encodes s with RFC 2045 quoted-printable so that
// '=' bytes (and any non-printable / >0x7E) become '=XX' hex sequences
// and long lines are wrapped at 76 chars with soft '=' line endings.
// Strict decoders (Stalwart among them) drop an unescaped '=' and mangle
// URLs in flight.
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

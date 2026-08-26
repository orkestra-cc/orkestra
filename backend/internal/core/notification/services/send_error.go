package services

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
)

// SendError is the only error shape a driver may return for a transport or
// vendor failure. It carries TYPED fields — the step that failed, a numeric
// reply code, a failure kind, an HTTP status, a vendor status/code — and no
// free text. The diagnostic is rendered by Error() from those fields, every
// string through an allowlist, so the chokepoint never copies a string a
// driver produced and a driver cannot hand it a remote body even by writing
// &SendError{Status: body}. Cause is for errors.Is/As and is never persisted
// or logged. Drivers use the constructors below rather than the literal.
type SendError struct {
	Driver string // registry name
	Op     string // the step that failed — a constant the driver owns; "" when unknown
	Kind   string // local failure kind, one of failureKinds; anything else renders "io"
	Code   int    // numeric server reply code (SMTP); 0 when none
	HTTP   int    // HTTP status of a vendor response; 0 when none
	Status string // vendor envelope status — allowlisted on render
	Vendor string // vendor envelope code — allowlisted on render
	Body   string // bodyTooLarge | bodyUnparseable; any other value renders the envelope shape
	Bytes  int    // bytes read of an unparseable body
	Media  string // Content-Type of an unparseable body — allowlisted on render
	Cause  error
}

const (
	bodyTooLarge    = "too_large"
	bodyUnparseable = "unparseable"
)

// failureKinds is the closed set a local failure may be reported as.
var failureKinds = map[string]bool{"dial": true, "tls": true, "timeout": true, "canceled": true, "io": true}

func safeKind(k string) string {
	if failureKinds[k] {
		return k
	}
	return "io"
}

// Error renders the diagnostic. Shapes, all tokens allowlisted, ints formatted:
//
//	vendor envelope    http=<n> status=<tok> code=<tok>
//	oversized body     http=<n> body=too_large
//	unparseable body   http=<n> body=unparseable bytes=<n> type=<media>
//	server rejection   <driver> op=<op> code=<nnn>
//	local failure      <driver> op=<op> err=<kind>   or   <driver> err=<kind>
func (e *SendError) Error() string {
	driver := safeToken(e.Driver)
	if driver == "" {
		driver = "driver"
	}
	switch {
	case e.HTTP > 0:
		switch e.Body {
		case bodyTooLarge:
			return fmt.Sprintf("http=%d body=too_large", e.HTTP)
		case bodyUnparseable:
			return fmt.Sprintf("http=%d body=unparseable bytes=%d type=%s", e.HTTP, e.Bytes, safeMediaType(e.Media))
		default:
			return fmt.Sprintf("http=%d status=%s code=%s", e.HTTP, safeToken(e.Status), safeToken(e.Vendor))
		}
	case e.Op != "" && e.Code > 0:
		return fmt.Sprintf("%s op=%s code=%d", driver, safeToken(e.Op), e.Code)
	case e.Op != "":
		return fmt.Sprintf("%s op=%s err=%s", driver, safeToken(e.Op), safeKind(e.Kind))
	default:
		return fmt.Sprintf("%s err=%s", driver, safeKind(e.Kind))
	}
}

func (e *SendError) Unwrap() error { return e.Cause }

// transportError reports a local failure (dial, TLS, timeout, …) at step op
// of driver; op may be "" for a driver whose exchange has no steps.
func transportError(driver, op string, err error) *SendError {
	return &SendError{Driver: driver, Op: op, Kind: failureKind(err), Cause: err}
}

// rejectionError keeps only the numeric reply code of a server rejection.
func rejectionError(driver, op string, code int, err error) *SendError {
	return &SendError{Driver: driver, Op: op, Code: code, Cause: err}
}

// vendorEnvelopeError records a parsed vendor envelope by its status and code.
func vendorEnvelopeError(driver string, httpStatus int, status, code string) *SendError {
	return &SendError{Driver: driver, HTTP: httpStatus, Status: status, Vendor: code}
}

// vendorBodyError records a body that was not parsed: body is bodyTooLarge
// (bytes and media ignored) or bodyUnparseable.
func vendorBodyError(driver string, httpStatus int, body string, bytes int, media string) *SendError {
	return &SendError{Driver: driver, HTTP: httpStatus, Body: body, Bytes: bytes, Media: media}
}

const (
	maxTokenLen   = 64
	maxErrorValue = 512
)

var (
	tokenAllow     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	mediaTypeAllow = regexp.MustCompile(`^[A-Za-z0-9._/+-]+$`)
)

// safeToken admits [A-Za-z0-9._-] up to 64 chars. Anything else becomes the
// marker "invalid" — the allowlist is the protection; the cap is a backstop.
func safeToken(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > maxTokenLen || !tokenAllow.MatchString(s) {
		return "invalid"
	}
	return s
}

// safeMediaType is safeToken with '/' and '+' admitted so real media types
// survive; parameters ("; charset=") do not.
func safeMediaType(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > maxTokenLen || !mediaTypeAllow.MatchString(s) {
		return "invalid"
	}
	return s
}

// safeTokens allowlists each element (a fork's driver may name its own
// requirement keys) and joins them; the count is capped so a hostile list
// cannot pad the diagnostic to the value cap.
func safeTokens(keys []string) string {
	if len(keys) > 16 {
		keys = keys[:16]
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, safeToken(k))
	}
	return strings.Join(out, ",")
}

func capString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// failureKind classifies a local or transport failure into a fixed set.
// The Go error string never leaves this function.
func failureKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	var (
		recHdr   tls.RecordHeaderError
		certErr  *tls.CertificateVerificationError
		unknown  x509.UnknownAuthorityError
		hostname x509.HostnameError
	)
	if errors.As(err, &recHdr) || errors.As(err, &certErr) || errors.As(err, &unknown) || errors.As(err, &hostname) {
		return "tls"
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return "dial"
	}
	return "io"
}

// describeSendError renders what the chokepoint stores for a failed send.
// Every branch is built from constants; an error of unknown shape — a fork's
// driver returning fmt.Errorf with a vendor body — is recorded as "unknown",
// not persisted. The sender slug is included so the delivery log names the
// profile before PR 4 adds a dedicated field.
func describeSendError(p SenderProfile, err error) string {
	slug := p.Slug
	if slug == "" {
		slug = "-"
	}
	prefix := "sender=" + safeToken(slug)
	var (
		se  *SendError
		inc *ProfileIncompleteError
		out string
	)
	switch {
	case errors.Is(err, ErrNoSenderForCategory):
		out = prefix + " err=no_sender_for_category"
	case errors.Is(err, ErrSenderConfigUnavailable):
		out = prefix + " err=config_unavailable"
	case errors.Is(err, ErrUnknownDriver):
		out = prefix + " driver=" + safeToken(p.Provider) + " err=unknown_driver"
	case errors.As(err, &inc):
		out = prefix + " driver=" + safeToken(p.Provider) + " err=not_configured missing=" + safeTokens(inc.Missing)
	case errors.As(err, &se):
		out = prefix + " " + se.Error() // rendered from typed fields through the allowlists — never a driver's string
	case errors.Is(err, context.DeadlineExceeded):
		out = prefix + " err=timeout"
	case errors.Is(err, context.Canceled):
		out = prefix + " err=canceled"
	default:
		out = prefix + " err=unknown"
	}
	return capString(out, maxErrorValue)
}

// maxResponseBody bounds every vendor response — success path included —
// before any parse. Generous for a JSON acknowledgement, harmless if a proxy
// answers with a page of HTML.
const maxResponseBody = 64 << 10

// readBounded reads at most max+1 bytes. Reading the one extra byte is what
// distinguishes "exactly at the cap" from "over it"; an oversized body costs
// max bytes of memory rather than an allocation the size of whatever was
// sent, and is never handed to a decoder.
func readBounded(r io.Reader, max int) (body []byte, tooLarge bool, err error) {
	b, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return nil, false, err
	}
	if len(b) > max {
		return nil, true, nil
	}
	return b, false, nil
}

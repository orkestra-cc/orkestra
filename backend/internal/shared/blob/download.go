package blob

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ObjectDownloadPresigner is an OPTIONAL capability a Store may implement: a
// presigned GET whose response forces a browser "save as" with a specific
// filename (via a signed response-content-disposition). Consumers type-assert
// for it and fall back to a plain PresignGet when the store lacks it, so adding
// it never breaks an existing Store implementation.
type ObjectDownloadPresigner interface {
	// PresignGetDownload returns a short-lived GET URL that downloads as an
	// attachment named downloadAs (e.g. "Atlante Perugia 2025.pdf"). A blank
	// downloadAs behaves like a plain presigned GET.
	PresignGetDownload(ctx context.Context, key, downloadAs string, ttl time.Duration) (string, error)
}

// ContentDispositionAttachment builds an RFC 6266 Content-Disposition value that
// makes a browser save the response as an attachment named filename. It emits an
// ASCII-sanitized `filename="..."` fallback plus a UTF-8 `filename*=UTF-8”...`
// (RFC 5987 percent-encoded) variant, so accented names (Italian, etc.) survive
// on modern browsers while legacy clients still get a safe ASCII name. Returns
// "" for a blank filename (the caller then presigns without a disposition).
func ContentDispositionAttachment(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return ""
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ASCIIFilenameFallback(filename), RFC5987Encode(filename))
}

// ASCIIFilenameFallback is the legacy `filename="..."` form of filename: printable
// ASCII is kept, quotes/backslash/control/non-ASCII become '_' so the
// quoted-string is always well-formed. Exported so the SMTP MIME builder writes
// the same Content-Disposition parameters as the HTTP download path.
func ASCIIFilenameFallback(filename string) string {
	var ascii strings.Builder
	for _, r := range filename {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			ascii.WriteByte('_')
		} else {
			ascii.WriteRune(r)
		}
	}
	return ascii.String()
}

// RFC5987Encode percent-encodes s's UTF-8 bytes per RFC 5987 (the value form for
// `filename*=UTF-8”`, also RFC 2231 in MIME): attr-chars pass through, every
// other byte becomes %XX.
func RFC5987Encode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '!', c == '#', c == '$', c == '&', c == '+', c == '-',
			c == '.', c == '^', c == '_', c == '`', c == '|', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}

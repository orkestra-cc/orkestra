package services

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// MaxAttachmentBytes caps the RAW (pre-base64) total per message.
const MaxAttachmentBytes = 5 << 20

// ErrAttachmentRejected is returned by drivers when the provider refuses the
// message because of its attachments. The service maps it to
// iface.FailureAttachmentRejected; it never crosses the SDK boundary.
var ErrAttachmentRejected = errors.New("attachment rejected")

var allowedAttachmentTypes = map[string]bool{"application/pdf": true}

// Bounds of SanitizeAttachmentFilename, in runes.
const (
	maxAttachmentStemRunes = 150
	// maxAttachmentExtRunes caps what counts as an extension (dot excluded):
	// a longer dotted tail is part of the stem, so the stem cap bounds it.
	maxAttachmentExtRunes = 10
)

// SanitizeAttachmentFilename keeps a display name safe for MIME headers and
// vendor payloads: base name only, no control chars, no Unicode format
// characters (bidi overrides, zero-width joiners), no quotes, ≤150 runes of
// stem, and an extension of ≤10 runes kept.
func SanitizeAttachmentFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '"' || r == ':' || r == '/' {
			continue
		}
		b.WriteRune(r)
	}
	clean := strings.TrimSpace(b.String())
	if clean == "" || clean == "." || clean == ".." {
		return "attachment.pdf"
	}
	ext := path.Ext(clean)
	if len([]rune(ext))-1 > maxAttachmentExtRunes {
		ext = ""
	}
	stem := []rune(strings.TrimSuffix(clean, ext))
	if len(stem) > maxAttachmentStemRunes {
		stem = stem[:maxAttachmentStemRunes]
	}
	return string(stem) + ext
}

// prepareAttachments validates and normalizes the request attachments. The
// returned error wraps ErrAttachmentRejected and carries no caller text
// beyond the (bounded-rendered) content type; describeSendError never
// persists it anyway.
func prepareAttachments(in []iface.Attachment) ([]iface.Attachment, []models.AttachmentMeta, error) {
	if len(in) == 0 {
		return nil, nil, nil
	}
	total := 0
	out := make([]iface.Attachment, 0, len(in))
	meta := make([]models.AttachmentMeta, 0, len(in))
	for _, a := range in {
		if !allowedAttachmentTypes[a.ContentType] {
			return nil, nil, fmt.Errorf("%w: content type %q not allowed", ErrAttachmentRejected, a.ContentType)
		}
		total += len(a.Data)
		name := SanitizeAttachmentFilename(a.Filename)
		out = append(out, iface.Attachment{Filename: name, ContentType: a.ContentType, Data: a.Data})
		meta = append(meta, models.AttachmentMeta{Filename: name, ContentType: a.ContentType, SizeBytes: len(a.Data)})
	}
	if total > MaxAttachmentBytes {
		return nil, meta, fmt.Errorf("%w: %d bytes over the %d cap", ErrAttachmentRejected, total, MaxAttachmentBytes)
	}
	return out, meta, nil
}

// Package emailhtml keeps HTML comments alive across html/template.
//
// html/template strips HTML comments from the parsed source, which kills the
// Outlook conditional comments (<!--[if mso]>…<![endif]-->, VML buttons,
// downlevel-revealed markers) every email template relies on. The fix is a
// stateless round trip: before Parse, the conditional DELIMITERS become inert
// marker elements (static text html/template emits verbatim) so the markup
// inside stays visible to the engine and gets contextual escaping; after
// Execute the markers become delimiters again. Ordinary comments are
// protected whole (base64) — nothing inside them is a template action.
//
// Markers: <x-emailhtml-cc data-c="B64"> (open, data-r="1" when
// downlevel-revealed), <x-emailhtml-ce> (close, data-r="1" idem),
// <x-emailhtml-cm data-b="B64"> (ordinary comment).
package emailhtml

import (
	"encoding/base64"
	"regexp"
)

var (
	// <!--[if COND]> optionally followed by <!--> (downlevel-revealed open).
	ccOpen = regexp.MustCompile(`(?s)<!--\[if\s([^\]]*)\]>(<!-->)?`)
	// <![endif]--> optionally preceded by <!-- (downlevel-revealed close).
	ccClose = regexp.MustCompile(`(?s)(<!--)?<!\[endif\]-->`)
	// Any comment left after the two above: ordinary.
	ordinary = regexp.MustCompile(`(?s)<!--.*?-->`)

	mOpen  = regexp.MustCompile(`<x-emailhtml-cc data-c="([A-Za-z0-9_\-=]*)"( data-r="1")?>`)
	mClose = regexp.MustCompile(`<x-emailhtml-ce( data-r="1")?>`)
	mCm    = regexp.MustCompile(`<x-emailhtml-cm data-b="([A-Za-z0-9_\-=]*)">`)

	// mAny finds exactly one marker element per match against the ORIGINAL
	// (pre-restore) text, so RestoreComments can run as a single pass: see
	// its doc comment for why that matters.
	mAny = regexp.MustCompile(mCm.String() + `|` + mOpen.String() + `|` + mClose.String())
)

func enc(s string) string { return base64.URLEncoding.EncodeToString([]byte(s)) }

// decodeB64 decodes s (URL alphabet) and reports whether it succeeded.
func decodeB64(s string) (string, bool) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// dec decodes s, returning fallback unchanged when s is not valid base64
// instead of silently discarding the malformed content.
func dec(s, fallback string) string {
	if v, ok := decodeB64(s); ok {
		return v
	}
	return fallback
}

// ProtectComments rewrites conditional delimiters into marker elements and
// ordinary comments into opaque markers. Safe to call on HTML without
// comments (returns the input unchanged).
func ProtectComments(src string) string {
	src = ccOpen.ReplaceAllStringFunc(src, func(m string) string {
		sub := ccOpen.FindStringSubmatch(m)
		r := ""
		if sub[2] != "" {
			r = ` data-r="1"`
		}
		return `<x-emailhtml-cc data-c="` + enc(sub[1]) + `"` + r + `>`
	})
	src = ccClose.ReplaceAllStringFunc(src, func(m string) string {
		if ccClose.FindStringSubmatch(m)[1] != "" {
			return `<x-emailhtml-ce data-r="1">`
		}
		return `<x-emailhtml-ce>`
	})
	src = ordinary.ReplaceAllStringFunc(src, func(m string) string {
		return `<x-emailhtml-cm data-b="` + enc(m) + `">`
	})
	return src
}

// RestoreComments is the exact inverse of ProtectComments.
//
// It runs as a SINGLE pass over out (mAny.ReplaceAllStringFunc): every
// marker element is located against the ORIGINAL out, and Go's
// ReplaceAllStringFunc never re-scans replacement text it has already
// produced. That matters because the base64 alphabet cannot spell '<', '>',
// '"' or a space, so a genuine marker tag can never hide inside another
// marker's data-* attribute value — but an ordinary HTML comment's AUTHORED
// text can legitimately contain a substring that merely LOOKS like one of
// our markers (e.g. a comment documenting this package's own tag syntax). A
// two-pass restore (decode ordinary comments, then re-scan the result for
// delimiter markers) would reinterpret that authored text as a live
// conditional and splice attacker- or author-controlled content into the
// document after html/template has already finished escaping it. Decoding
// ordinary-comment markers and delimiter markers in the same pass, against
// the same original string, means a decoded comment's content is emitted
// once, verbatim, and never fed back through the regexes.
//
// Known trade-off (accepted; see TestRawEndifFragmentInsideOrdinaryCommentIsNotRestored):
// an ordinary comment whose AUTHORED text contains a raw
// "<![endif]-->"-shaped substring with no matching "<!--[if" (e.g.
// `<!-- random text <![endif]--> more -->`) does not round-trip
// byte-for-byte. ProtectComments' ccClose pass runs before the ordinary
// pass and has no way to know that substring belongs to an otherwise-
// ordinary comment, so it consumes it into a close marker before that
// marker gets base64-wrapped along with the rest of the comment; restoring
// it verbatim (this function's job) necessarily leaves that embedded marker
// as literal text rather than decoding it back to "<![endif]-->". This is
// inert — the difference lives inside a comment body no renderer executes,
// and a downstream HTML sanitizer that drops comments removes it outright —
// and closing it would require re-scanning decoded content, which is
// exactly the spoofing path this single-pass design exists to shut off.
func RestoreComments(out string) string {
	return mAny.ReplaceAllStringFunc(out, func(m string) string {
		if sub := mCm.FindStringSubmatch(m); sub != nil {
			return dec(sub[1], m)
		}
		if sub := mOpen.FindStringSubmatch(m); sub != nil {
			cond, ok := decodeB64(sub[1])
			if !ok {
				return m
			}
			s := `<!--[if ` + cond + `]>`
			if sub[2] != "" {
				s += `<!-->`
			}
			return s
		}
		if sub := mClose.FindStringSubmatch(m); sub != nil {
			if sub[1] != "" {
				return `<!--<![endif]-->`
			}
			return `<![endif]-->`
		}
		return m
	})
}

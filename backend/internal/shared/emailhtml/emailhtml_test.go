package emailhtml

import (
	"bytes"
	htmltmpl "html/template"
	"strings"
	"testing"
)

func renderThrough(t *testing.T, src string, data map[string]any) string {
	t.Helper()
	tpl, err := htmltmpl.New("x").Parse(ProtectComments(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var b bytes.Buffer
	if err := tpl.Execute(&b, data); err != nil {
		t.Fatalf("exec: %v", err)
	}
	return RestoreComments(b.String())
}

func TestConditionalSurvivesHTMLTemplate(t *testing.T) {
	src := `<body><!--[if mso]><table><tr><td>MSO</td></tr></table><![endif]--><p>{{.Name}}</p></body>`
	out := renderThrough(t, src, map[string]any{"Name": "Mario"})
	if !strings.Contains(out, `<!--[if mso]><table><tr><td>MSO</td></tr></table><![endif]-->`) {
		t.Fatalf("conditional lost or altered: %s", out)
	}
	if !strings.Contains(out, "<p>Mario</p>") {
		t.Fatalf("action not rendered: %s", out)
	}
}

func TestConditionalInnerActionsAreEscapedContextually(t *testing.T) {
	src := `<!--[if mso]><v:roundrect href="{{.URL}}"><center>{{.Label}}</center></v:roundrect><![endif]-->`
	out := renderThrough(t, src, map[string]any{"URL": "javascript:alert(1)", "Label": "<b>x</b>"})
	if !strings.Contains(out, `href="#ZgotmplZ"`) {
		t.Fatalf("href not filtered: %s", out)
	}
	if !strings.Contains(out, "&lt;b&gt;x&lt;/b&gt;") {
		t.Fatalf("text not escaped: %s", out)
	}
	if !strings.HasPrefix(out, "<!--[if mso]>") || !strings.HasSuffix(out, "<![endif]-->") {
		t.Fatalf("delimiters not restored: %s", out)
	}
}

func TestDownlevelRevealedMarkersRoundTrip(t *testing.T) {
	src := `<!--[if !mso]><!--><div>web</div><!--<![endif]-->`
	out := renderThrough(t, src, nil)
	if out != src {
		t.Fatalf("got %q want %q", out, src)
	}
}

func TestNestedConditionalsAndHead(t *testing.T) {
	src := `<html><head><!--[if gte mso 9]><xml><o:OfficeDocumentSettings><o:PixelsPerInch>96</o:PixelsPerInch></o:OfficeDocumentSettings></xml><![endif]--></head><body><!--[if mso]><!--[if lt mso 15]>old<![endif]-->new<![endif]--></body></html>`
	out := renderThrough(t, src, nil)
	if out != src {
		t.Fatalf("got %q want %q", out, src)
	}
}

func TestOrdinaryCommentProtectedWhole(t *testing.T) {
	src := `<p>a</p><!-- {{.Secret}} {{ not a template }} --><p>b</p>`
	out := renderThrough(t, src, map[string]any{"Secret": "S"})
	if out != src {
		t.Fatalf("ordinary comment must come back byte-identical, got %q", out)
	}
}

func TestProtectIsIdempotentOnPlainHTML(t *testing.T) {
	src := `<p>no comments here</p>`
	if ProtectComments(src) != src || RestoreComments(src) != src {
		t.Fatal("plain html must pass untouched")
	}
}

// TestOrdinaryCommentContainingLiteralOpenMarkerRoundTrips guards against a
// two-pass restore that re-scans a decoded ordinary comment's content: text
// that merely LOOKS like our own open-marker tag (e.g. documentation of this
// package's own syntax, written by a human) must come back byte-identical,
// never be reinterpreted as a live conditional.
func TestOrdinaryCommentContainingLiteralOpenMarkerRoundTrips(t *testing.T) {
	src := `<p>a</p><!-- see <x-emailhtml-cc data-c="dGVzdA=="> for details --><p>b</p>`
	out := renderThrough(t, src, nil)
	if out != src {
		t.Fatalf("ordinary comment mimicking an open marker must come back byte-identical, got %q want %q", out, src)
	}
}

// TestOrdinaryCommentContainingLiteralCloseMarkerRoundTrips is the close-tag
// twin of TestOrdinaryCommentContainingLiteralOpenMarkerRoundTrips.
func TestOrdinaryCommentContainingLiteralCloseMarkerRoundTrips(t *testing.T) {
	src := `<p>a</p><!-- close tag example: <x-emailhtml-ce> end --><p>b</p>`
	out := renderThrough(t, src, nil)
	if out != src {
		t.Fatalf("ordinary comment mimicking a close marker must come back byte-identical, got %q want %q", out, src)
	}
}

// TestForgedMarkerPayloadDoesNotReachOutputAsLiveMarkup asserts the actual
// injection path a two-pass restore opens: a forged data-c payload chosen so
// that, if RestoreComments re-scanned decoded ordinary-comment content, it
// would splice the decoded text into a brand-new "<!--[if ...]>" conditional
// that did not exist in the source. The payload must stay inert data.
func TestForgedMarkerPayloadDoesNotReachOutputAsLiveMarkup(t *testing.T) {
	// base64 of `<script>alert(1)</script>`.
	forged := `<!-- <x-emailhtml-cc data-c="PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg=="> --><p>{{.Name}}</p>`
	want := `<!-- <x-emailhtml-cc data-c="PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg=="> --><p>Mario</p>`
	out := renderThrough(t, forged, map[string]any{"Name": "Mario"})
	if strings.Contains(out, "<!--[if") {
		t.Fatalf("forged marker payload reached output as a live conditional: %s", out)
	}
	if out != want {
		t.Fatalf("ordinary comment must come back byte-identical, got %q want %q", out, want)
	}
}

// TestRestoreCommentsKeepsMalformedMarkerVerbatim covers a marker element
// whose base64 payload does not decode (e.g. corrupted in transit): the
// malformed marker must be left exactly as found, not silently dropped.
func TestRestoreCommentsKeepsMalformedMarkerVerbatim(t *testing.T) {
	cases := []string{
		`<x-emailhtml-cm data-b="A">`,
		`<x-emailhtml-cc data-c="A">`,
	}
	for _, in := range cases {
		if out := RestoreComments(in); out != in {
			t.Fatalf("malformed marker must be left unchanged, got %q want %q", out, in)
		}
	}
}

// TestProtectCommentsIdempotentOnCommentBearingHTML extends
// TestProtectIsIdempotentOnPlainHTML (which only covers comment-free input)
// to content that DOES have conditional and ordinary comments: the package
// doc promises a stateless round trip, and template composition (a layout
// wrapping an already-protected fragment) applies ProtectComments twice.
func TestProtectCommentsIdempotentOnCommentBearingHTML(t *testing.T) {
	src := `<!--[if mso]><table><tr><td>MSO</td></tr></table><![endif]--><!-- plain note --><p>x</p>`
	once := ProtectComments(src)
	twice := ProtectComments(once)
	if once != twice {
		t.Fatalf("ProtectComments must be idempotent on its own output, got %q want %q", twice, once)
	}
}

// TestAdjacentSiblingConditionalBlocks covers two (or more) non-nested
// conditional blocks in sequence — TestNestedConditionalsAndHead only
// exercises nesting, not siblings.
func TestAdjacentSiblingConditionalBlocks(t *testing.T) {
	src := `<!--[if mso]>A<![endif]--><!--[if !mso]><!-->B<!--<![endif]-->`
	out := renderThrough(t, src, nil)
	if out != src {
		t.Fatalf("got %q want %q", out, src)
	}
}

// TestRawEndifFragmentInsideOrdinaryCommentIsNotRestored pins the ONE
// accepted, ruled trade-off of RestoreComments' single-pass design — see the
// "Known trade-off" paragraph in its doc comment, which names this test.
//
// It does NOT assert byte-identity with src: ProtectComments' ccClose pass
// consumes the "<![endif]-->"-shaped fragment into a close marker before the
// ordinary pass wraps the rest of the comment in base64, so restoring it
// verbatim necessarily leaves that marker as literal "<x-emailhtml-ce>" text
// instead of decoding it back. That is the actual, accepted behaviour this
// test pins.
//
// If this test ever starts failing because the output went back to
// byte-identical with src, it means RestoreComments was changed back into a
// two-pass (decode-then-rescan) design — and that reopens the marker-
// spoofing vulnerability the single-pass design exists to close (see
// TestForgedMarkerPayloadDoesNotReachOutputAsLiveMarkup). Do not "fix" this
// test by asserting byte-identity again without re-closing that hole.
func TestRawEndifFragmentInsideOrdinaryCommentIsNotRestored(t *testing.T) {
	src := `<p>a</p><!-- random text <![endif]--> more --><p>b</p>`
	want := `<p>a</p><!-- random text <x-emailhtml-ce> more --><p>b</p>`
	out := renderThrough(t, src, nil)
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

package errcode

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestErrorExtensionsAreTopLevelAndNeverOverrideTheEnvelope(t *testing.T) {
	t.Parallel()
	e := Conflict("x.y", "detail").WithExtension("retainUntil", "2031-01-01T00:00:00Z").WithExtension("code", "hijack")
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["retainUntil"] != "2031-01-01T00:00:00Z" || m["code"] != "x.y" || m["status"] != float64(409) {
		t.Fatalf("body = %s", b)
	}
	plain, _ := json.Marshal(Conflict("x.y", "d"))
	if string(plain) != `{"status":409,"title":"Conflict","detail":"d","code":"x.y"}` {
		t.Fatalf("plain body changed: %s", plain)
	}
}

// legacyEnvelope is the struct shape before Extensions existed.
type legacyEnvelope struct {
	Status int    `json:"status"`
	Title  string `json:"title,omitempty"`
	Detail string `json:"detail"`
	Code   string `json:"code,omitempty"`
}

func encodeLikeHuma(t *testing.T, v any) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestErrorBodyIsByteIdenticalToTheLegacyEncodingWithHTMLCharacters(t *testing.T) {
	t.Parallel()
	const detail = "a & <b>"
	want := encodeLikeHuma(t, legacyEnvelope{Status: 409, Title: "Conflict", Detail: detail, Code: "x.y"})
	if got := encodeLikeHuma(t, Conflict("x.y", detail)); got != want {
		t.Fatalf("plain: got %s want %s", got, want)
	}
	// With extensions the envelope members keep their exact bytes.
	got := encodeLikeHuma(t, Conflict("x.y", detail).WithExtension("reasons", []string{"p&q"}))
	if !strings.Contains(got, `"detail":"a & <b>"`) || !strings.Contains(got, `"reasons":["p&q"]`) {
		t.Fatalf("with extensions: %s", got)
	}
}

func TestExtensionsCannotShadowEmptyEnvelopeMembers(t *testing.T) {
	t.Parallel()
	e := &Error{Status: 500, Detail: "d"} // title and code empty: omitted
	e.WithExtension("code", "hijack").WithExtension("title", "hijack")
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), "hijack") {
		t.Fatalf("body = %s", b)
	}
}

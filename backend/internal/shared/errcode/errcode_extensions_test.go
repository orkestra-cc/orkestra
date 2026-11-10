package errcode

import (
	"encoding/json"
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

package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

type testEmail string

type testDoc map[string]any // a named map, like bson.M

type testValuer struct{ addr string }

func (v testValuer) LogValue() slog.Value {
	return slog.GroupValue(slog.String("contact", v.addr), slog.String("password", "hunter2"))
}

// Values whose static type the masker's type switch cannot name still reach
// the free-text rules: named string types, byte slices, json.RawMessage,
// map keys, named maps, pointers, LogValuers inside containers.
func TestMaskAttr_ValuesBeyondTheTypeSwitch(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	const addr = "alice@example.com"
	email := testEmail(addr)
	cases := []struct {
		name string
		attr slog.Attr
	}{
		{"named string", slog.Any("to", email)},
		{"byte slice", slog.Any("body", []byte("mail "+addr))},
		{"json.RawMessage", slog.Any("payload", json.RawMessage(`{"email":"`+addr+`"}`))},
		{"map[string]any key", slog.Any("m", map[string]any{addr: 1})},
		{"map[string]int key", slog.Any("m", map[string]int{addr: 1})},
		{"map[string]string key", slog.Any("m", map[string]string{addr: "x"})},
		{"map[string][]string key", slog.Any("m", map[string][]string{addr: {"x"}})},
		{"named map", slog.Any("doc", testDoc{"k": addr, addr: "v"})},
		{"map of named strings", slog.Any("m", map[string]testEmail{"to": email})},
		{"slice of named strings", slog.Any("s", []testEmail{email})},
		{"slice of byte slices", slog.Any("s", [][]byte{[]byte(addr)})},
		{"array of strings", slog.Any("a", [2]string{"x", addr})},
		{"pointer to string", slog.Any("p", &email)},
		{"LogValuer in a map", slog.Any("m", map[string]any{"v": testValuer{addr: addr}})},
		{"slog.Value in a slice", slog.Any("s", []any{slog.StringValue(addr)})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			h := NewPolicyHandler(slog.NewJSONHandler(&buf, nil), NewStaticLogPolicyResolver(*m.p), testHashKey)
			slog.New(h).Info("x", c.attr)
			var tbuf bytes.Buffer
			th := NewPolicyHandler(slog.NewTextHandler(&tbuf, nil), NewStaticLogPolicyResolver(*m.p), testHashKey)
			slog.New(th).Info("x", c.attr)
			for name, out := range map[string]string{"json": buf.String(), "text": tbuf.String()} {
				if strings.Contains(out, "alice") || strings.Contains(out, "YWxpY2") { // YWxpY2 = base64("alic")
					t.Fatalf("%s output leaks the address: %s", name, out)
				}
				if !strings.Contains(out, "[EMAIL]") {
					t.Fatalf("%s output lost the value instead of masking it: %s", name, out)
				}
				if strings.Contains(out, "hunter2") {
					t.Fatalf("%s output leaks a secret: %s", name, out)
				}
			}
		})
	}
}

// Map entries keep the key rules of their raw key; values of basic types
// and byte arrays come through unchanged.
func TestMaskAttr_MapKeyRulesAndBasicValues(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	v, _ := maskOne(t, m, slog.Any("m", map[string]int{"password": 7, "count": 3}))
	got := v.(map[string]any)
	if got["password"] != "[REDACTED]" || got["count"] != 3 {
		t.Fatalf("map[string]int = %v", got)
	}
	ints := []int{1, 2}
	if v, _ := maskOne(t, m, slog.Any("ids", ints)); v.([]int)[1] != 2 {
		t.Fatalf("[]int = %v", v)
	}
	id := [16]byte{1, 2, 3}
	if v, _ := maskOne(t, m, slog.Any("id", id)); v != id {
		t.Fatalf("[16]byte = %v", v)
	}
	// Without free-text scanning the key stays as logged.
	off := logMasker{p: policy(func(p *iface.LogContentPolicy) { p.ScanFreeText = false }), key: testHashKey}
	if v, _ := maskOne(t, off, slog.Any("m", map[string]int{"bob@example.org": 1})); v.(map[string]any)["bob@example.org"] != 1 {
		t.Fatalf("scanFreeText=false map = %v", v)
	}
}

// The node budget bounds the reflective paths too.
func TestMaskAttr_ReflectiveContainersRespectBudget(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	big := make(map[string]testEmail, maxMaskNodes+10)
	for i := range maxMaskNodes + 10 {
		big[fmt.Sprintf("k%d", i)] = "alice@example.com"
	}
	if v, _ := maskOne(t, m, slog.Any("big", big)); v != "[REDACTED]" {
		t.Fatalf("over-budget named map = %T, want [REDACTED]", v)
	}
	long := make([]testEmail, maxMaskNodes+10)
	if v, _ := maskOne(t, m, slog.Any("long", long)); v != "[REDACTED]" {
		t.Fatalf("over-budget slice = %T, want [REDACTED]", v)
	}
}

// Forwarding headers and peer addresses logged under their own names are IP
// keys: the IP rule applies even with free-text scanning off.
func TestMaskAttr_HeaderStyleIPKeys(t *testing.T) {
	m := logMasker{p: policy(func(p *iface.LogContentPolicy) {
		p.ScanFreeText = false
		p.IPAddress = iface.IPAddressTruncated
	}), key: testHashKey}
	for _, key := range []string{"X-Forwarded-For", "x_real_ip", "Forwarded", "client.address", "peer_address", "network.peer.address"} {
		if v, _ := maskOne(t, m, slog.String(key, "203.0.113.7")); v != "203.0.113.0/24" {
			t.Errorf("%s = %v, want 203.0.113.0/24", key, v)
		}
	}
	// A list the IP rule cannot parse fails closed.
	if v, _ := maskOne(t, m, slog.String("X-Forwarded-For", "203.0.113.7, 198.51.100.2")); v != "[REDACTED]" {
		t.Errorf("XFF list = %v, want [REDACTED]", v)
	}
	// In an http.Header the header name is the key.
	v, _ := maskOne(t, m, slog.Any("h", map[string][]string{"X-Real-Ip": {"203.0.113.7"}}))
	if got := v.(map[string][]string)["X-Real-Ip"]; len(got) != 1 || got[0] != "203.0.113.0/24" {
		t.Errorf("header X-Real-Ip = %v", got)
	}
}

package utils

import (
	"log/slog"
	"strings"
	"testing"
)

func keyCached(raw string) bool {
	mp := keyCache.Load()
	if mp == nil {
		return false
	}
	_, ok := (*mp)[raw]
	return ok
}

// Long raw keys can be caller-controlled (even PII): they are classified but
// never retained by the cache.
func TestClassifyKey_LongKeysNotCached(t *testing.T) {
	short := "zz_cache_probe_short"
	long := strings.Repeat("x", maxCachedKeyLen) + "_password" // > maxCachedKeyLen bytes
	if len(long) <= maxCachedKeyLen {
		t.Fatal("test key must exceed the cache limit")
	}

	classifyKey(short)
	if !keyCached(short) {
		t.Fatal("a short key must be cached")
	}
	if ki := classifyKey(long); ki.class != keySecret {
		t.Fatalf("long key classified as %v, want keySecret", ki.class)
	}
	if keyCached(long) {
		t.Fatal("a key over maxCachedKeyLen bytes must not enter the cache")
	}

	m := logMasker{p: policy(nil), key: testHashKey}
	if v, _ := maskOne(t, m, slog.String(long, "s3cr3t")); v != "[REDACTED]" {
		t.Fatalf("long secret key value = %v, want [REDACTED]", v)
	}
	if keyCached(long) {
		t.Fatal("masking must not cache the long key either")
	}
}

// Keys found inside logged values are data (often caller-controlled): they
// are classified but never enter the cache. Attribute keys do.
func TestClassifyKey_MapKeysNotCached(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	const attrKey, mapKey, nestedKey, headerKey = "zz_attr_probe", "zz_map_probe", "zz_nested_probe", "Zz-Header-Probe"
	m.maskAttr(slog.Any(attrKey, map[string]any{
		mapKey: map[string]string{nestedKey: "v"},
		"h":    map[string][]string{headerKey: {"v"}},
	}))
	if !keyCached(attrKey) {
		t.Fatal("the attribute key must be cached")
	}
	for _, k := range []string{mapKey, nestedKey, headerKey} {
		if keyCached(k) {
			t.Errorf("map key %q entered the cache", k)
		}
	}
}

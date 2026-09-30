package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/orkestra/backend/internal/shared/redact"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	piiMask      = "[PII]"
	maskErrValue = "[REDACTED:error]"
	textEmail    = "[EMAIL]"
	textIBAN     = "[IBAN]"
	textCF       = "[CF]"
	textIP       = "[IP]"
)

// maxScanTextLen bounds the free-text scan (T1 follow-up): a longer value is
// cut at the last whitespace within the first maxScanTextLen bytes, so no
// partial token (half an e-mail, half an IBAN) survives, and textTruncated
// marks the cut. The regexps are linear, but a multi-megabyte value would
// still cost milliseconds on every record.
const (
	maxScanTextLen = 32 << 10
	textTruncated  = "[TRUNCATED]"
)

// Normalized attribute keys with a dedicated rule (spec §2.3).
var (
	ipKeys = map[string]struct{}{
		"remote": {}, "ip": {}, "ipaddress": {}, "clientip": {}, "remoteaddr": {}, "remoteip": {},
		// forwarding headers and peer addresses logged under their own names
		"xforwardedfor": {}, "xrealip": {}, "forwarded": {},
		"clientaddress": {}, "peeraddress": {}, "networkpeeraddress": {},
	}
	uaKeys      = map[string]struct{}{"ua": {}, "useragent": {}}
	subjectKeys = map[string]struct{}{"userid": {}, "useruuid": {}, "actoruserid": {}, "subjectid": {}}
	// correlationKeys carry the tracer's generated hex ids, which the
	// free-text rules only ever misread as an IBAN or similar. The exemption
	// holds only for a value of exactly that shape (correlationShaped): the
	// key name alone proves nothing, anyone can log under it. request_id is
	// not here: chi's RequestID copies the client's X-Request-Id verbatim.
	correlationKeys = map[string]int{"traceid": 32, "spanid": 16}
)

// correlationShaped reports whether s is exactly what the tracer generates
// for the correlation key norm: 32 (trace id) or 16 (span id) lowercase hex
// characters. Any other value under such a key is scanned like free text.
func correlationShaped(norm, s string) bool {
	n, ok := correlationKeys[norm]
	if !ok || len(s) != n {
		return false
	}
	for i := 0; i < n; i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

var (
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// IBAN: compact or in space-separated groups of four, any case.
	ibanRe = regexp.MustCompile(`(?i)\b[A-Z]{2}[0-9]{2}(?:[A-Z0-9]{11,30}|(?: [A-Z0-9]{4}){2,7}(?: [A-Z0-9]{1,4})?)\b`)
	// Codice fiscale; the omocodia substitutions (L M N P Q R S T U V) may
	// replace any of the digits.
	cfRe = regexp.MustCompile(`(?i)\b[A-Z]{6}[0-9LMNPQRSTUV]{2}[A-Z][0-9LMNPQRSTUV]{2}[A-Z][0-9LMNPQRSTUV]{3}[A-Z]\b`)
)

const (
	// maxMaskDepth bounds the recursion into maps and slices: a
	// self-referencing container would otherwise overflow the stack, which
	// recover() cannot catch.
	maxMaskDepth = 32
	// maxMaskNodes bounds the values visited per attribute: the depth cap
	// alone lets a branching cycle (m["a"]=m; m["b"]=m) visit 2^33 nodes.
	maxMaskNodes = 10000
	// maxIPv6Substring is the longest textual IPv6 address (with an
	// IPv4-mapped tail); the address search never looks at longer substrings.
	maxIPv6Substring = 45
)

// maskingPanicHook is called after every recovered masking panic; main.go
// points it at the Prometheus counter (spec §9). nil = no-op.
var maskingPanicHook atomic.Pointer[func()]

// SetMaskingPanicHook installs (or, with nil, removes) the panic hook.
func SetMaskingPanicHook(f func()) {
	if f == nil {
		maskingPanicHook.Store(nil)
		return
	}
	maskingPanicHook.Store(&f)
}

func reportMaskingPanic() {
	if f := maskingPanicHook.Load(); f != nil {
		(*f)()
	}
}

// keyClass is the policy-independent rule family of a normalised attribute
// key; whether it is one of the policy's PIIKeys stays a per-policy check.
type keyClass uint8

const (
	keyOther keyClass = iota
	keySecret
	keyIP
	keyUA
	keySubject
	// keyCorrelation marks the tracer's correlation ids (trace_id, span_id):
	// not scanned as free text when the value has the generated shape.
	keyCorrelation
)

type keyInfo struct {
	norm  string
	class keyClass
}

// maxKeyCacheEntries bounds the normalisation cache. Attribute keys are
// almost always a small fixed vocabulary; once the cache is full new keys are
// simply recomputed, so an attacker-chosen key stream cannot grow memory.
const maxKeyCacheEntries = 4096

// maxCachedKeyLen keeps long raw keys out of the cache: a key can be caller
// controlled (even PII), and the cache would retain up to 4096 of them.
const maxCachedKeyLen = 64

// keyCache is copy-on-write: readers do one atomic load and a map lookup, no
// lock; the few writers (each new key once, at most maxKeyCacheEntries times)
// clone the map under keyCacheMu.
var (
	keyCache   atomic.Pointer[map[string]keyInfo]
	keyCacheMu sync.Mutex
)

// classifyKey returns the normalised form and rule family of an attribute
// key, memoised: NormalizeKey and the secret/IP/UA/subject lookups are the
// dominant per-attribute cost otherwise.
func classifyKey(raw string) keyInfo { return lookupKey(raw, true) }

// classifyDataKey classifies a key found inside a logged value (a map key):
// the cache is read but never grows, because such keys are data, often
// caller-controlled, and the cache would retain them.
func classifyDataKey(raw string) keyInfo { return lookupKey(raw, false) }

func lookupKey(raw string, store bool) keyInfo {
	mp := keyCache.Load()
	if mp != nil {
		if ki, ok := (*mp)[raw]; ok {
			return ki
		}
	}
	var ki keyInfo
	ki.norm = redact.NormalizeKey(raw)
	switch {
	case redact.IsSecretNormalized(ki.norm):
		ki.class = keySecret
	default:
		if _, ok := ipKeys[ki.norm]; ok {
			ki.class = keyIP
		} else if _, ok := uaKeys[ki.norm]; ok {
			ki.class = keyUA
		} else if _, ok := subjectKeys[ki.norm]; ok {
			ki.class = keySubject
		} else if _, ok := correlationKeys[ki.norm]; ok {
			ki.class = keyCorrelation
		}
	}
	// A full cache is final: checked before the mutex, so once it is full no
	// lookup miss ever takes the lock.
	if !store || len(raw) > maxCachedKeyLen || mp != nil && len(*mp) >= maxKeyCacheEntries {
		return ki
	}
	keyCacheMu.Lock()
	defer keyCacheMu.Unlock()
	var cur map[string]keyInfo
	if mp := keyCache.Load(); mp != nil {
		cur = *mp
	}
	if _, ok := cur[raw]; !ok && len(cur) < maxKeyCacheEntries {
		next := make(map[string]keyInfo, len(cur)+1)
		for k, v := range cur {
			next[k] = v
		}
		next[strings.Clone(raw)] = ki
		keyCache.Store(&next)
	}
	return ki
}

// valueString is fmt.Sprint(val) without the copy for the common string case.
func valueString(val any) string {
	if s, ok := val.(string); ok {
		return s
	}
	return fmt.Sprint(val)
}

// logMasker applies one LogContentPolicy to log attributes. A nil key makes
// the "hashed" modes drop the value.
type logMasker struct {
	p   *iface.LogContentPolicy
	key []byte
}

// maskAttr returns the masked attribute and whether to keep it. A panic
// while masking becomes a [REDACTED:error] value: a log line must never
// crash the process nor leak the unmasked value.
func (m logMasker) maskAttr(a slog.Attr) (slog.Attr, bool) {
	budget := maxMaskNodes
	return m.maskAttrAt(a, 0, &budget)
}

// maskAttrAt is maskAttr at a given group depth, sharing the attribute's node
// budget: a LogValuer that returns a group containing itself must neither
// overflow the stack nor branch exponentially.
func (m logMasker) maskAttrAt(a slog.Attr, depth int, budget *int) (out slog.Attr, keep bool) {
	defer func() {
		if recover() != nil {
			reportMaskingPanic()
			out, keep = slog.String(a.Key, maskErrValue), true
		}
	}()
	*budget--
	if depth > maxMaskDepth || *budget < 0 {
		return slog.String(a.Key, redact.Redacted), true
	}
	v := a.Value
	if v.Kind() == slog.KindLogValuer {
		v = v.Resolve()
	}
	kind := v.Kind()
	if kind == slog.KindGroup {
		// A named group follows the same key rules as a map under that key;
		// an empty key (inline group) just recurses.
		if a.Key != "" {
			if attr, keep, handled := m.groupKeyRule(a.Key); handled {
				return attr, keep
			}
		}
		children := v.Group()
		masked := make([]slog.Attr, 0, len(children))
		for _, c := range children {
			if mc, ok := m.maskAttrAt(c, depth+1, budget); ok {
				masked = append(masked, mc)
			}
			if *budget < 0 {
				return slog.String(a.Key, redact.Redacted), true
			}
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(masked...)}, true
	}
	// Fast path (behaviour-identical to the maskKV route below): a key with no
	// rule and not a PII key only needs its value scanned, and scalar kinds
	// need nothing at all, so neither pays for the any/slog.Any round trip.
	ki := classifyKey(a.Key)
	if kind == slog.KindString {
		// String values under an IP, subject or user-agent key skip the
		// v.Any() boxing too; the key rules themselves are unchanged.
		switch ki.class {
		case keyIP:
			s, ok := m.maskIPString(v.String())
			if !ok {
				return slog.Attr{}, false
			}
			return slog.String(a.Key, s), true
		case keySubject:
			s, ok := m.maskSubjectString(v.String())
			if !ok {
				return slog.Attr{}, false
			}
			return slog.String(a.Key, s), true
		case keyUA:
			if m.p.UserAgent == iface.UserAgentFull {
				return slog.Attr{Key: a.Key, Value: v}, true
			}
			return slog.Attr{}, false
		}
	}
	if (ki.class == keyOther || ki.class == keyCorrelation) && !slices.Contains(m.p.PIIKeys, ki.norm) {
		switch kind {
		case slog.KindString, slog.KindInt64, slog.KindUint64, slog.KindFloat64,
			slog.KindBool, slog.KindDuration, slog.KindTime:
			// maskValue charges one more node against the budget.
			*budget--
			if *budget < 0 {
				return slog.String(a.Key, redact.Redacted), true
			}
			if kind == slog.KindString && (ki.class == keyOther || !correlationShaped(ki.norm, v.String())) {
				return slog.String(a.Key, m.maskText(v.String())), true
			}
			return slog.Attr{Key: a.Key, Value: v}, true
		}
	}
	nv, ok := m.maskKV(a.Key, v.Any(), depth, budget)
	if !ok {
		return slog.Attr{}, false
	}
	return slog.Any(a.Key, nv), true
}

// groupKeyRule applies to a named slog group the key rules maskKV applies to
// a map under the same key, in the same order. handled=false means no rule
// claims the key and the children are masked one by one.
func (m logMasker) groupKeyRule(rawKey string) (attr slog.Attr, keep, handled bool) {
	ki := classifyKey(rawKey)
	switch ki.class {
	case keySecret:
		return slog.String(rawKey, redact.Redacted), true, true
	case keyIP:
		switch m.p.IPAddress {
		case iface.IPAddressFull:
			return slog.Attr{}, false, false
		case iface.IPAddressTruncated, iface.IPAddressHashed:
			return slog.String(rawKey, redact.Redacted), true, true
		default: // omitted, or an unknown mode: fail closed
			return slog.Attr{}, false, true
		}
	case keyUA:
		if m.p.UserAgent == iface.UserAgentFull {
			return slog.Attr{}, false, false
		}
		return slog.Attr{}, false, true
	case keySubject:
		if m.p.SubjectIDs == iface.SubjectIDUUID {
			return slog.Attr{}, false, false
		}
		return slog.Attr{}, false, true
	}
	if slices.Contains(m.p.PIIKeys, ki.norm) {
		return slog.String(rawKey, piiMask), true, true
	}
	return slog.Attr{}, false, false
}

// maskKV applies the key rules in spec order, then the value rules, to an
// attribute key and its value.
func (m logMasker) maskKV(rawKey string, val any, depth int, budget *int) (any, bool) {
	return m.maskKVInfo(classifyKey(rawKey), val, depth, budget)
}

// maskKVInfo is maskKV for an already classified key.
func (m logMasker) maskKVInfo(ki keyInfo, val any, depth int, budget *int) (any, bool) {
	switch ki.class {
	case keySecret:
		return redact.Redacted, true
	case keyIP:
		return m.maskIP(valueString(val))
	case keyUA:
		if m.p.UserAgent == iface.UserAgentFull {
			return val, true
		}
		return nil, false
	case keySubject:
		return m.maskSubject(valueString(val))
	}
	if slices.Contains(m.p.PIIKeys, ki.norm) {
		return piiMask, true
	}
	if ki.class == keyCorrelation {
		if str, ok := val.(string); ok && correlationShaped(ki.norm, str) {
			return str, true
		}
	}
	return m.maskValue(val, depth, budget), true
}

// maskDataKV masks one map entry: the key is scanned as free text (a map key
// is data and can carry an e-mail), the key rules apply to the value under
// the raw key. ok=false drops the entry.
func (m logMasker) maskDataKV(rawKey string, val any, depth int, budget *int) (key string, out any, ok bool) {
	out, ok = m.maskKVInfo(classifyDataKey(rawKey), val, depth, budget)
	if !ok {
		return "", nil, false
	}
	return m.maskText(rawKey), out, true
}

func (m logMasker) maskValue(val any, depth int, budget *int) any {
	*budget--
	if depth > maxMaskDepth || *budget < 0 {
		return redact.Redacted
	}
	// Containers give up as a whole once the node budget is spent, instead of
	// allocating a full-width copy at every remaining level.
	switch x := val.(type) {
	case nil:
		return nil
	case string:
		return m.maskText(x)
	case error:
		return m.maskText(x.Error())
	case slog.LogValuer:
		// Inside a container nothing resolves it (a handler would marshal the
		// raw type): resolve it and mask what it stands for.
		return m.maskValue(slog.AnyValue(x).Resolve().Any(), depth+1, budget)
	case slog.Value:
		return m.maskValue(x.Resolve().Any(), depth+1, budget)
	case []slog.Attr: // a resolved group
		out := make(map[string]any, len(x))
		for _, a := range x {
			if k, nv, ok := m.maskDataKV(a.Key, a.Value.Resolve().Any(), depth+1, budget); ok {
				out[k] = nv
			}
			if *budget < 0 {
				return redact.Redacted
			}
		}
		return out
	case []byte:
		// The text handler prints the bytes raw, the JSON one base64: both
		// give the content back, so it is scanned as text.
		return m.maskText(string(x))
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			if mk, nv, ok := m.maskDataKV(k, v, depth+1, budget); ok {
				out[mk] = nv
			}
			if *budget < 0 {
				return redact.Redacted
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = m.maskValue(v, depth+1, budget)
			if *budget < 0 {
				return redact.Redacted
			}
		}
		return out
	case []string:
		out := make([]string, len(x))
		for i, v := range x {
			out[i] = m.maskText(v)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(x))
		for k, v := range x {
			if mk, nv, ok := m.maskDataKV(k, v, depth+1, budget); ok {
				out[mk] = fmt.Sprint(nv)
			}
			if *budget < 0 {
				return redact.Redacted
			}
		}
		return out
	case http.Header: // named types do not match the plain map case below
		mv, ok := m.maskValue(map[string][]string(x), depth, budget).(map[string][]string)
		if !ok {
			return redact.Redacted
		}
		return http.Header(mv)
	case url.Values:
		mv, ok := m.maskValue(map[string][]string(x), depth, budget).(map[string][]string)
		if !ok {
			return redact.Redacted
		}
		return url.Values(mv)
	case map[string][]string:
		out := make(map[string][]string, len(x))
		for k, vs := range x {
			ki := classifyDataKey(k)
			masked := make([]string, 0, len(vs))
			for _, v := range vs {
				if nv, ok := m.maskKVInfo(ki, v, depth+1, budget); ok {
					masked = append(masked, fmt.Sprint(nv))
				}
				if *budget < 0 {
					return redact.Redacted
				}
			}
			out[m.maskText(k)] = masked
		}
		return out
	default:
		return m.maskReflect(val, depth, budget)
	}
}

// maskReflect covers what the switch in maskValue cannot name: named string
// types (type Email string), byte slices such as json.RawMessage, maps with
// keys of any kind (rendered as text), slices and arrays of anything, pointers
// to non-structs. The rest (numbers, booleans, structs, functions) is
// returned as it is; logscope flags the opaque ones at the call site.
func (m logMasker) maskReflect(val any, depth int, budget *int) any {
	rv := reflect.ValueOf(val)
	switch rv.Kind() {
	case reflect.String:
		return m.maskText(rv.String())
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return m.maskText(string(rv.Bytes()))
		}
		return m.maskSeq(val, rv, depth, budget)
	case reflect.Array:
		// Byte arrays are fixed-size binary ids (uuid.UUID), not text.
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return val
		}
		return m.maskSeq(val, rv, depth, budget)
	case reflect.Map:
		// Keys of any kind are rendered as text, as the JSON handler does with
		// numeric keys, and masked as data keys: a map keyed by user id or by
		// something that prints an e-mail must not bypass the rules.
		stringKeys := rv.Type().Key().Kind() == reflect.String
		out := make(map[string]any, rv.Len())
		for it := rv.MapRange(); it.Next(); {
			var k string
			if stringKeys {
				k = it.Key().String()
			} else {
				k = fmt.Sprint(it.Key().Interface())
			}
			if mk, nv, ok := m.maskDataKV(k, it.Value().Interface(), depth+1, budget); ok {
				out[mk] = nv
			}
			if *budget < 0 {
				return redact.Redacted
			}
		}
		return out
	case reflect.Pointer:
		if rv.IsNil() || rv.Elem().Kind() == reflect.Struct {
			return val
		}
		return m.maskValue(rv.Elem().Interface(), depth+1, budget)
	default:
		return val
	}
}

// maskSeq masks the elements of a slice or array; one of numbers or
// booleans is returned as it is, without a copy.
func (m logMasker) maskSeq(val any, rv reflect.Value, depth int, budget *int) any {
	switch rv.Type().Elem().Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return val
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = m.maskValue(rv.Index(i).Interface(), depth+1, budget)
		if *budget < 0 {
			return redact.Redacted
		}
	}
	return out
}

func (m logMasker) maskIP(raw string) (any, bool) {
	s, ok := m.maskIPString(raw)
	if !ok {
		return nil, false
	}
	return s, true
}

func (m logMasker) maskIPString(raw string) (string, bool) {
	if raw == "" {
		return raw, true
	}
	switch m.p.IPAddress {
	case iface.IPAddressFull:
		return raw, true
	case iface.IPAddressTruncated:
		host := hostOf(raw)
		if strings.IndexByte(host, ':') < 0 {
			// No colon: only a dotted quad can parse, and netip does it
			// without allocating.
			addr, err := netip.ParseAddr(host)
			if err != nil || !addr.Is4() {
				return redact.Redacted, true
			}
			return truncateV4(addr.As4()), true
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return redact.Redacted, true
		}
		return truncateIP(ip), true
	case iface.IPAddressHashed:
		if len(m.key) == 0 {
			return "", false
		}
		host := raw
		if ip := parseHostIP(raw); ip != nil {
			host = ip.String()
		}
		return m.hash("ip:", host), true
	default: // omitted, or an unknown mode: fail closed
		return "", false
	}
}

func (m logMasker) maskSubject(raw string) (any, bool) {
	s, ok := m.maskSubjectString(raw)
	if !ok {
		return nil, false
	}
	return s, true
}

func (m logMasker) maskSubjectString(raw string) (string, bool) {
	if raw == "" {
		return raw, true
	}
	switch m.p.SubjectIDs {
	case iface.SubjectIDUUID:
		return raw, true
	case iface.SubjectIDHashed:
		if len(m.key) == 0 {
			return "", false
		}
		return m.hash("sub:", raw), true
	default:
		return "", false
	}
}

// maskText scans free text: email, IBAN and codice fiscale first, then IP
// addresses (maskIPRuns). Every step is linear in the input, and the input
// is bounded by maxScanTextLen.
func (m logMasker) maskText(s string) string {
	if !m.p.ScanFreeText || s == "" {
		return s
	}
	suffix := ""
	if len(s) > maxScanTextLen {
		s = s[:strings.LastIndexAny(s[:maxScanTextLen], " \t\r\n")+1]
		suffix = textTruncated
	}
	s = emailRe.ReplaceAllString(s, textEmail)
	s = ibanRe.ReplaceAllStringFunc(s, maskIBANCandidate)
	s = cfRe.ReplaceAllString(s, textCF)
	if m.p.IPAddress != iface.IPAddressFull {
		s = m.maskIPRuns(s)
	}
	return s + suffix
}

// maskIBANCandidate replaces an ibanRe match with [IBAN]. Only a candidate
// made entirely of hex characters must also pass the ISO 13616 mod-97
// check: that is the one shape a generated hex id (a 32-hex trace id
// starting with two letters and two digits) shares with an IBAN. Any other
// match is masked whatever its checksum, so a mistyped IBAN does not leak.
func maskIBANCandidate(c string) string {
	if !allHex(c) || validIBAN(c) {
		return textIBAN
	}
	return c
}

// allHex reports whether c, spaces aside, is only [0-9A-Fa-f].
func allHex(c string) bool {
	for i := 0; i < len(c); i++ {
		if c[i] != ' ' && !isHexDigit(c[i]) {
			return false
		}
	}
	return true
}

// validIBAN reports whether c (spaces allowed, any case) is 15-34 letters
// and digits with a valid mod-97 checksum: the first four characters move to
// the end, letters become 10..35, and the number mod 97 must be 1.
func validIBAN(c string) bool {
	var buf [40]byte
	n := 0
	for i := 0; i < len(c); i++ {
		b := c[i]
		switch {
		case b == ' ':
			continue
		case b >= 'a' && b <= 'z':
			b -= 'a' - 'A'
		case b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		default:
			return false
		}
		if n == len(buf) {
			return false
		}
		buf[n] = b
		n++
	}
	if n < 15 || n > 34 {
		return false
	}
	rem := 0
	feed := func(b byte) {
		if b >= 'A' {
			rem = (rem*100 + int(b-'A') + 10) % 97
			return
		}
		rem = (rem*10 + int(b-'0')) % 97
	}
	for _, b := range buf[4:n] {
		feed(b)
	}
	for _, b := range buf[:4] {
		feed(b)
	}
	return rem == 1
}

func isIPRunByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' || c == ':' || c == '.'
}

func isZoneByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '.' || c == '-'
}

// maskIPRuns is the single fail-closed rule for IP addresses in free text.
//
// The text is cut into maximal runs of [0-9A-Fa-f:.], a run being extended by
// a '%zone' suffix and by surrounding '[' ']'. A run with at least two ':' or
// three '.' is a candidate; if it contains anything that is or looks like an
// address (runContainsIP) the WHOLE run is replaced, so no group, octet, word
// tail or port glued to an address can stay behind. The exact truncated or
// hashed form is rendered only when the run, minus brackets, zone and one
// leading/trailing punctuation or port, is exactly one address (exactRunIP);
// every other case becomes "[IP]". Words are part of a run when they end in
// hex letters ("src:2001:..." gives "sr[IP]", "source2001:..." "sour[IP]"),
// and digits glued to a quad join it ("id1203.0.113.7" gives "i[IP]"):
// over-masking by design.
func (m logMasker) maskIPRuns(s string) string {
	var b strings.Builder
	last := 0
	for i := 0; i < len(s); {
		if !isIPRunByte(s[i]) {
			i++
			continue
		}
		j := i
		colons, dots := 0, 0
		for j < len(s) && isIPRunByte(s[j]) {
			switch s[j] {
			case ':':
				colons++
			case '.':
				dots++
			}
			j++
		}
		core := s[i:j]
		if (colons < 2 && dots < 3) || !runContainsIP(core) {
			i = j
			continue
		}
		end := j
		if end < len(s) && s[end] == '%' {
			k := end + 1
			for k < len(s) && isZoneByte(s[k]) {
				k++
			}
			if k > end+1 {
				end = k
			}
		}
		start, bracketed := i, false
		if i > 0 && s[i-1] == '[' && end < len(s) && s[end] == ']' {
			start, end, bracketed = i-1, end+1, true
		}
		rep := textIP
		if m.p.IPAddress == iface.IPAddressTruncated || m.p.IPAddress == iface.IPAddressHashed {
			if lead, ip, trail, ok := exactRunIP(core); ok {
				rep = lead + m.ipReplacement(ip) + trail
				if bracketed {
					rep = "[" + rep + "]"
				}
			}
		}
		b.WriteString(s[last:start])
		b.WriteString(rep)
		last, i = end, end
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// runContainsIP reports whether a candidate run holds an address or something
// shaped like one (hasDottedQuad or hasIPv6). Glued words and digits do not
// hide an address: both checks look at every substring, not only at group
// boundaries. Linear in the run.
func runContainsIP(r string) bool {
	return hasDottedQuad(r) || hasIPv6(r)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHexDigit(c byte) bool {
	return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// hasDottedQuad reports whether r contains [0-9]+(\.[0-9]+){3}: four
// consecutive '.'-separated fields, the middle two all digits (any count), the
// first ending and the last starting with a digit. Valid or not, and whatever
// is glued before or after it, such a quad counts as an address (fail closed).
func hasDottedQuad(r string) bool {
	// ends/all of the three fields before the current one, oldest first
	var ends, all [3]bool
	seen := 0
	for start := 0; start <= len(r); {
		end := strings.IndexByte(r[start:], '.')
		if end < 0 {
			end = len(r)
		} else {
			end += start
		}
		f := r[start:end]
		fAll := f != ""
		for i := 0; i < len(f) && fAll; i++ {
			fAll = isDigit(f[i])
		}
		fStarts := f != "" && isDigit(f[0])
		fEnds := f != "" && isDigit(f[len(f)-1])
		if seen >= 3 && ends[0] && all[1] && all[2] && fStarts {
			return true
		}
		ends = [3]bool{ends[1], ends[2], fEnds}
		all = [3]bool{all[1], all[2], fAll}
		seen++
		start = end + 1
	}
	return false
}

// hasIPv6 reports whether some substring of r is an IPv6 address for
// net.ParseIP. An address starts with a hex digit or with "::", so only those
// positions are tried; that includes positions inside a longer hex word
// ("src2001:..." holds "2001:..."), and ipv6From lets the address end inside
// a longer last group ("...:7334abc"). Each start looks at most
// maxIPv6Substring bytes ahead and stops at the first byte no address can
// continue with, so the scan is linear with a small constant.
func hasIPv6(r string) bool {
	for i := 0; i < len(r); i++ {
		if isHexDigit(r[i]) || r[i] == ':' && i+1 < len(r) && r[i+1] == ':' {
			if ipv6From(r, i) {
				return true
			}
		}
	}
	return false
}

// ipv6From reports whether some r[i:j] parses as an IPv6 address. It walks
// the IPv6 grammar forward from i and gives up as soon as the prefix can no
// longer be the start of an address: a group over four hex digits, a second
// "::" or ":::", more than eight groups, an IPv4 tail not after a ':' or
// with more than four fields or three-digit octets. The grammar here is looser
// than net.ParseIP's (octet values, leading zeros, "::" standing for no group
// are not checked), so it never stops early on a real address; net.ParseIP
// only confirms prefixes that are complete (at least two ':', ending in a hex
// group, "::" or a four-field IPv4 tail, eight groups or fewer with "::").
func ipv6From(r string, i int) bool {
	limit := min(len(r), i+maxIPv6Substring)
	colons, groups, cur := 0, 0, 0 // cur: hex digits in the current group
	digitsOnly, ellipsis := true, false
	v4, dots, oct := false, 0, 0 // IPv4 tail: fields seen, digits in the current one
	for j := i; j < limit; j++ {
		c := r[j]
		complete, total := false, 0
		switch {
		case v4:
			switch {
			case isDigit(c):
				oct++
				if oct > 3 {
					return false
				}
				complete, total = dots == 3, groups+2
			case c == '.':
				if oct == 0 || dots == 3 {
					return false
				}
				dots, oct = dots+1, 0
			default:
				return false
			}
		case isHexDigit(c):
			cur++
			if cur > 4 {
				return false
			}
			digitsOnly = digitsOnly && isDigit(c)
			complete, total = true, groups+1
		case c == ':':
			if cur > 0 {
				groups++
				if groups > 8 {
					return false
				}
				cur, digitsOnly = 0, true
			} else if j > i { // the previous byte was ':' too
				if ellipsis {
					return false
				}
				ellipsis = true
				complete, total = true, groups
			}
			colons++
		case c == '.':
			// the current group becomes the first IPv4 field
			if cur == 0 || cur > 3 || !digitsOnly || colons == 0 {
				return false
			}
			v4, dots, oct = true, 1, 0
		default:
			return false
		}
		if complete && colons >= 2 && (total == 8 || ellipsis && total <= 8) && net.ParseIP(r[i:j+1]) != nil {
			return true
		}
	}
	return false
}

// maxExactRun bounds the runs exactRunIP looks at: the longest address plus
// a leading punctuation, a ":<port>" and a trailing punctuation.
const maxExactRun = maxIPv6Substring + 8

// exactRunIP accepts a run (without its brackets and '%zone', which the
// caller handles) that is exactly one address, optionally with one leading
// ':' or '.' and one trailing ':' or '.' and/or ':<port digits>'; lead and
// trail are returned to be kept outside the replacement. A run with anything
// glued to the address is never exact. Whole-run parsing is tried first, and a port is only cut off an
// IPv4 address (bare IPv6 has no port syntax, and reading the last group of a
// nine-group run as a port would leave it in the clear).
func exactRunIP(core string) (lead string, ip net.IP, trail string, ok bool) {
	if len(core) > maxExactRun {
		return "", nil, "", false
	}
	for _, tk := range [4]int{0, 1, 2, 3} {
		for _, strip := range [2]bool{false, true} {
			t := core
			l := ""
			if strip {
				if t == "" || (t[0] != ':' && t[0] != '.') {
					continue
				}
				l, t = t[:1], t[1:]
			}
			tr := ""
			switch tk {
			case 1: // one trailing ':' or '.'
				if t == "" || (t[len(t)-1] != ':' && t[len(t)-1] != '.') {
					continue
				}
				t, tr = t[:len(t)-1], t[len(t)-1:]
			case 2: // one trailing ':<port>'
				var found bool
				if t, tr, found = cutPort(t); !found || strings.Contains(t, ":") {
					continue
				}
			case 3: // ':<port>:' or ':<port>.'
				if t == "" || (t[len(t)-1] != ':' && t[len(t)-1] != '.') {
					continue
				}
				punct := t[len(t)-1:]
				var found bool
				if t, tr, found = cutPort(t[:len(t)-1]); !found || strings.Contains(t, ":") {
					continue
				}
				tr += punct
			}
			if ip := net.ParseIP(t); ip != nil {
				return l, ip, tr, true
			}
		}
	}
	return "", nil, "", false
}

// cutPort splits a trailing ":<digits>" (at most five) off t.
func cutPort(t string) (head, port string, ok bool) {
	c := strings.LastIndexByte(t, ':')
	if c < 0 || c == len(t)-1 || len(t)-c-1 > 5 {
		return t, "", false
	}
	for i := c + 1; i < len(t); i++ {
		if !isDigit(t[i]) {
			return t, "", false
		}
	}
	return t[:c], t[c:], true
}

func (m logMasker) safeText(s string) (out string) {
	defer func() {
		if recover() != nil {
			reportMaskingPanic()
			out = maskErrValue
		}
	}()
	return m.maskText(s)
}

// ipReplacement renders an address found in free text per the IP mode; any
// mode that cannot produce a pseudonym fails closed to "[IP]".
func (m logMasker) ipReplacement(ip net.IP) string {
	if ip == nil {
		return textIP
	}
	switch m.p.IPAddress {
	case iface.IPAddressTruncated:
		return truncateIP(ip)
	case iface.IPAddressHashed:
		if len(m.key) > 0 {
			return m.hash("ip:", ip.String())
		}
	}
	return textIP
}

// hash is a keyed, domain-separated pseudonym: the same value hashes
// differently as an IP and as a subject.
func (m logMasker) hash(domain, value string) string {
	mac := hmac.New(sha256.New, m.key)
	mac.Write([]byte(domain + value))
	return "h:" + hex.EncodeToString(mac.Sum(nil))[:16]
}

// parseHostIP accepts "ip", "ip:port" and "[ipv6]:port" (http.Request's
// RemoteAddr carries the port).
func parseHostIP(raw string) net.IP {
	return net.ParseIP(hostOf(raw))
}

// hostOf strips the port and any brackets; a string without ':' cannot carry
// a port, so SplitHostPort (whose error allocates) is skipped for it.
func hostOf(raw string) string {
	if strings.IndexByte(raw, ':') >= 0 {
		if host, _, err := net.SplitHostPort(raw); err == nil {
			raw = host
		}
	}
	return strings.Trim(raw, "[]")
}

func truncateIP(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return truncateV4([4]byte(v4))
	}
	return ip.Mask(net.CIDRMask(48, 128)).String() + "/48"
}

// truncateV4 renders "a.b.c.0/24" in one allocation.
func truncateV4(a [4]byte) string {
	var buf [18]byte
	b := strconv.AppendUint(buf[:0], uint64(a[0]), 10)
	b = append(b, '.')
	b = strconv.AppendUint(b, uint64(a[1]), 10)
	b = append(b, '.')
	b = strconv.AppendUint(b, uint64(a[2]), 10)
	b = append(b, ".0/24"...)
	return string(b)
}

// MaskKV applies policy p to one key/value pair with the same rules as the
// slog PolicyHandler (spec §2.3), for callers outside the slog chain such as
// the span exporter. keep=false means "drop the pair". Never panics.
func MaskKV(p *iface.LogContentPolicy, hashKey []byte, key string, val any) (out any, keep bool) {
	defer func() {
		if recover() != nil {
			reportMaskingPanic()
			out, keep = maskErrValue, true
		}
	}()
	budget := maxMaskNodes
	return logMasker{p: p, key: hashKey}.maskKV(key, val, 0, &budget)
}

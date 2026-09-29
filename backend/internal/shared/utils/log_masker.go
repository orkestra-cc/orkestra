package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
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

// Normalized attribute keys with a dedicated rule (spec §2.3).
var (
	ipKeys      = map[string]struct{}{"remote": {}, "ip": {}, "ipaddress": {}, "clientip": {}, "remoteaddr": {}, "remoteip": {}}
	uaKeys      = map[string]struct{}{"ua": {}, "useragent": {}}
	subjectKeys = map[string]struct{}{"userid": {}, "useruuid": {}, "actoruserid": {}, "subjectid": {}}
)

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
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
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
	key := redact.NormalizeKey(rawKey)
	if redact.IsSecretNormalized(key) {
		return slog.String(rawKey, redact.Redacted), true, true
	}
	if _, ok := ipKeys[key]; ok {
		switch m.p.IPAddress {
		case iface.IPAddressFull:
			return slog.Attr{}, false, false
		case iface.IPAddressTruncated, iface.IPAddressHashed:
			return slog.String(rawKey, redact.Redacted), true, true
		default: // omitted, or an unknown mode: fail closed
			return slog.Attr{}, false, true
		}
	}
	if _, ok := uaKeys[key]; ok {
		if m.p.UserAgent == iface.UserAgentFull {
			return slog.Attr{}, false, false
		}
		return slog.Attr{}, false, true
	}
	if _, ok := subjectKeys[key]; ok {
		if m.p.SubjectIDs == iface.SubjectIDUUID {
			return slog.Attr{}, false, false
		}
		return slog.Attr{}, false, true
	}
	if slices.Contains(m.p.PIIKeys, key) {
		return slog.String(rawKey, piiMask), true, true
	}
	return slog.Attr{}, false, false
}

// maskKV applies the key rules in spec order, then the value rules.
func (m logMasker) maskKV(rawKey string, val any, depth int, budget *int) (any, bool) {
	key := redact.NormalizeKey(rawKey)
	if redact.IsSecretNormalized(key) {
		return redact.Redacted, true
	}
	if _, ok := ipKeys[key]; ok {
		return m.maskIP(fmt.Sprint(val))
	}
	if _, ok := uaKeys[key]; ok {
		if m.p.UserAgent == iface.UserAgentFull {
			return val, true
		}
		return nil, false
	}
	if _, ok := subjectKeys[key]; ok {
		return m.maskSubject(fmt.Sprint(val))
	}
	if slices.Contains(m.p.PIIKeys, key) {
		return piiMask, true
	}
	return m.maskValue(val, depth, budget), true
}

func (m logMasker) maskValue(val any, depth int, budget *int) any {
	*budget--
	if depth > maxMaskDepth || *budget < 0 {
		return redact.Redacted
	}
	// Containers give up as a whole once the node budget is spent, instead of
	// allocating a full-width copy at every remaining level.
	switch x := val.(type) {
	case string:
		return m.maskText(x)
	case error:
		return m.maskText(x.Error())
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			if nv, ok := m.maskKV(k, v, depth+1, budget); ok {
				out[k] = nv
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
			if nv, ok := m.maskKV(k, v, depth+1, budget); ok {
				out[k] = fmt.Sprint(nv)
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
			masked := make([]string, 0, len(vs))
			for _, v := range vs {
				if nv, ok := m.maskKV(k, v, depth+1, budget); ok {
					masked = append(masked, fmt.Sprint(nv))
				}
				if *budget < 0 {
					return redact.Redacted
				}
			}
			out[k] = masked
		}
		return out
	default:
		return val
	}
}

func (m logMasker) maskIP(raw string) (any, bool) {
	if raw == "" {
		return raw, true
	}
	switch m.p.IPAddress {
	case iface.IPAddressFull:
		return raw, true
	case iface.IPAddressTruncated:
		ip := parseHostIP(raw)
		if ip == nil {
			return redact.Redacted, true
		}
		return truncateIP(ip), true
	case iface.IPAddressHashed:
		if len(m.key) == 0 {
			return nil, false
		}
		host := raw
		if ip := parseHostIP(raw); ip != nil {
			host = ip.String()
		}
		return m.hash("ip:", host), true
	default: // omitted, or an unknown mode: fail closed
		return nil, false
	}
}

func (m logMasker) maskSubject(raw string) (any, bool) {
	if raw == "" {
		return raw, true
	}
	switch m.p.SubjectIDs {
	case iface.SubjectIDUUID:
		return raw, true
	case iface.SubjectIDHashed:
		if len(m.key) == 0 {
			return nil, false
		}
		return m.hash("sub:", raw), true
	default:
		return nil, false
	}
}

// maskText scans free text: email, IBAN and codice fiscale first, then IP
// addresses (maskIPRuns). Every step is linear in the input.
func (m logMasker) maskText(s string) string {
	if !m.p.ScanFreeText || s == "" {
		return s
	}
	s = emailRe.ReplaceAllString(s, textEmail)
	s = ibanRe.ReplaceAllString(s, textIBAN)
	s = cfRe.ReplaceAllString(s, textCF)
	if m.p.IPAddress == iface.IPAddressFull {
		return s
	}
	return m.maskIPRuns(s)
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
// hex letters ("src:2001:..." gives "sr[IP]"): over-masking by design.
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
// shaped like one: four consecutive dotted decimal groups of one to three
// digits (valid or not, so zero-padded quads count; the first group may end
// in digits and the last may start with digits, so a hex-letter word glued to
// an octet does not hide it), or a substring of at most 45 characters that
// starts at a group boundary (run start, after ':' or '.'), ends at one (run
// end, before ':' or '.') and parses as an address. Linear: each start looks
// at most 45 characters ahead.
func runContainsIP(r string) bool {
	if parts := strings.Split(r, "."); len(parts) >= 4 {
		for k := 0; k+3 < len(parts); k++ {
			if decTail(parts[k]) && decGroup(parts[k+1]) && decGroup(parts[k+2]) && decHead(parts[k+3]) {
				return true
			}
		}
	}
	for i := 0; i < len(r); i++ {
		if i > 0 && r[i-1] != ':' && r[i-1] != '.' {
			continue
		}
		limit := min(len(r), i+maxIPv6Substring)
		for j := i + 2; j <= limit; j++ {
			if j < len(r) && r[j] != ':' && r[j] != '.' {
				continue
			}
			if net.ParseIP(r[i:j]) != nil {
				return true
			}
		}
	}
	return false
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func decGroup(p string) bool {
	if len(p) == 0 || len(p) > 3 {
		return false
	}
	for i := 0; i < len(p); i++ {
		if !isDigit(p[i]) {
			return false
		}
	}
	return true
}

// decTail: p ends in one to three digits that follow a non-digit or start.
func decTail(p string) bool {
	n := 0
	for n < len(p) && isDigit(p[len(p)-1-n]) {
		n++
	}
	return n >= 1 && n <= 3
}

// decHead: p starts with one to three digits followed by a non-digit or end.
func decHead(p string) bool {
	n := 0
	for n < len(p) && isDigit(p[n]) {
		n++
	}
	return n >= 1 && n <= 3
}

// exactRunIP accepts a run that is exactly one address, optionally with one
// leading ':' or '.', a '%zone' (dropped), and one trailing ':' or '.' and/or
// ':<port digits>'; lead and trail are returned to be kept outside the
// replacement. Whole-run parsing is tried first, and a port is only cut off an
// IPv4 address (bare IPv6 has no port syntax, and reading the last group of a
// nine-group run as a port would leave it in the clear).
func exactRunIP(core string) (lead string, ip net.IP, trail string, ok bool) {
	if i := strings.IndexByte(core, '%'); i >= 0 {
		core = core[:i]
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
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	return net.ParseIP(strings.Trim(raw, "[]"))
}

func truncateIP(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.Mask(net.CIDRMask(24, 32)).String() + "/24"
	}
	return ip.Mask(net.CIDRMask(48, 128)).String() + "/48"
}

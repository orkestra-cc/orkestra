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
	// Any dotted run of four numbers; boundaries and validity are decided in
	// code (net.ParseIP), so "client_203.0.113.7" and "203.000.113.007" are
	// both seen.
	ipv4Re = regexp.MustCompile(`[0-9]+(?:\.[0-9]+){3,}`)
	// A maximal run of hex digits and colons with at least two colons, whose
	// last segment may carry a dotted-quad tail (IPv4-mapped addresses).
	// It is only a candidate: ipv6Spans finds the real address inside it.
	ipv6CandidateRe = regexp.MustCompile(`[0-9A-Fa-f:]*:[0-9A-Fa-f:]*:[0-9A-Fa-f:.]*`)
)

const (
	// maxMaskDepth bounds the recursion into maps and slices: a
	// self-referencing container would otherwise overflow the stack, which
	// recover() cannot catch.
	maxMaskDepth = 32
	// maxMaskNodes bounds the values visited per attribute: the depth cap
	// alone lets a branching cycle (m["a"]=m; m["b"]=m) visit 2^33 nodes.
	maxMaskNodes = 10000
	// maxIPv6Candidate bounds the quadratic address search inside a candidate.
	maxIPv6Candidate = 128
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
func (m logMasker) maskAttr(a slog.Attr) (out slog.Attr, keep bool) {
	defer func() {
		if recover() != nil {
			reportMaskingPanic()
			out, keep = slog.String(a.Key, maskErrValue), true
		}
	}()
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
			if mc, ok := m.maskAttr(c); ok {
				masked = append(masked, mc)
			}
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(masked...)}, true
	}
	budget := maxMaskNodes
	nv, ok := m.maskKV(a.Key, v.Any(), 0, &budget)
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
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = m.maskValue(v, depth+1, budget)
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
		}
		return out
	case http.Header: // named types do not match the plain map case below
		return http.Header(m.maskValue(map[string][]string(x), depth, budget).(map[string][]string))
	case url.Values:
		return url.Values(m.maskValue(map[string][]string(x), depth, budget).(map[string][]string))
	case map[string][]string:
		out := make(map[string][]string, len(x))
		for k, vs := range x {
			masked := make([]string, 0, len(vs))
			for _, v := range vs {
				if nv, ok := m.maskKV(k, v, depth+1, budget); ok {
					masked = append(masked, fmt.Sprint(nv))
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
// addresses. IPv6 spans are located first and the text between them is
// scanned for IPv4, so one address is never processed twice (an IPv6 output
// such as "203.0.113.0/24" is not re-matched).
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
	var b strings.Builder
	last := 0
	for _, sp := range ipv6Spans(s) {
		b.WriteString(m.maskIPv4Text(s[last:sp.start]))
		b.WriteString(m.ipReplacement(sp.ip))
		last = sp.end
	}
	b.WriteString(m.maskIPv4Text(s[last:]))
	return b.String()
}

type ipSpan struct {
	start, end int
	ip         net.IP
}

// ipv6Spans finds IPv6 addresses in s and fails closed around them. A
// candidate run may carry extra characters ("host:2001:db8::1:", a word whose
// tail is hex digits before an uncompressed address, a trailing "."), so the
// address is searched inside it: start at the run start or right after a
// colon, end at the run end or before a ':' or '.'. Among the valid
// addresses the one reaching furthest right wins (earliest start on a tie),
// so a word tail cannot absorb a group and strand the last one in the clear.
// Hex left over next to the address is masked with it; bare ':' and '.'
// punctuation stays. A run too long to search is masked whole (nil ip).
func ipv6Spans(s string) []ipSpan {
	var spans []ipSpan
	for _, loc := range ipv6CandidateRe.FindAllStringIndex(s, -1) {
		lo, hi := loc[0], loc[1]
		// "203.0.113.7:2001:db8::1": the group before the first colon is the
		// last octet of an IPv4 address, which the IPv4 pass handles.
		if lo >= 2 && s[lo-1] == '.' && s[lo-2] >= '0' && s[lo-2] <= '9' {
			if c := strings.IndexByte(s[lo:hi], ':'); c >= 0 {
				lo += c
			}
		}
		if hi-lo > maxIPv6Candidate {
			spans = append(spans, ipSpan{start: lo, end: hi})
			continue
		}
		bestI, bestJ := -1, -1
		var bestIP net.IP
		for i := lo; i < hi; i++ {
			if i > lo && s[i-1] != ':' {
				continue
			}
			for j := hi; j > i && j > bestJ; j-- {
				if j < hi && s[j] != ':' && s[j] != '.' {
					continue
				}
				if !strings.Contains(s[i:j], ":") {
					break
				}
				if ip := net.ParseIP(s[i:j]); ip != nil {
					bestI, bestJ, bestIP = i, j, ip
					break
				}
			}
		}
		if bestI < 0 {
			continue
		}
		start, end := bestI, bestJ
		if strings.Trim(s[lo:bestI], ":") != "" {
			start = lo
		}
		if strings.Trim(s[bestJ:hi], ":.") != "" {
			end = hi
		}
		spans = append(spans, ipSpan{start: start, end: end, ip: bestIP})
	}
	return spans
}

// maskIPv4Text replaces the dotted quads of a text segment. A quad that
// net.ParseIP rejects ("203.000.113.007", "999.1.1.1") still looks like an
// address and becomes "[IP]". In a run of five or more numbers ("v1.203.0.113.7",
// "203.0.113.7.5") every window of four that parses is an address: the span
// from the first to the last such window is replaced as one, rendered from
// the first window's address; a run with no valid window is left alone.
func (m logMasker) maskIPv4Text(s string) string {
	locs := ipv4Re.FindAllStringIndex(s, -1)
	if locs == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		run := s[loc[0]:loc[1]]
		nums := strings.Split(run, ".")
		if len(nums) == 4 {
			b.WriteString(s[last:loc[0]])
			if ip := net.ParseIP(run); ip != nil {
				b.WriteString(m.ipReplacement(ip))
			} else {
				b.WriteString(textIP)
			}
			last = loc[1]
			continue
		}
		first, lastEnd := -1, -1
		var firstIP net.IP
		for w := 0; w+4 <= len(nums); w++ {
			if ip := net.ParseIP(strings.Join(nums[w:w+4], ".")); ip != nil {
				if first < 0 {
					first, firstIP = w, ip
				}
				lastEnd = w + 4
			}
		}
		if first < 0 {
			continue
		}
		off := 0 // byte offset of nums[first] and end of nums[lastEnd-1] in run
		for _, n := range nums[:first] {
			off += len(n) + 1
		}
		endOff := off
		for _, n := range nums[first:lastEnd] {
			endOff += len(n) + 1
		}
		endOff-- // no dot after the last number
		b.WriteString(s[last : loc[0]+off])
		b.WriteString(m.ipReplacement(firstIP))
		b.WriteString(s[loc[0]+endOff : loc[1]])
		last = loc[1]
	}
	b.WriteString(s[last:])
	return b.String()
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

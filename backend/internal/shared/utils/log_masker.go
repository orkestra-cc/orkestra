package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
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
	ibanRe  = regexp.MustCompile(`\b[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}\b`)
	cfRe    = regexp.MustCompile(`(?i)\b[A-Z]{6}[0-9]{2}[A-Z][0-9]{2}[A-Z][0-9]{3}[A-Z]\b`)
	ipv4Re  = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	// Any run with at least two colons; net.ParseIP decides whether it is
	// really an IPv6 address (times like 10:20:30 are rejected there).
	ipv6CandidateRe = regexp.MustCompile(`[0-9A-Fa-f:]*:[0-9A-Fa-f:]*:[0-9A-Fa-f:]*`)
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
		children := v.Group()
		masked := make([]slog.Attr, 0, len(children))
		for _, c := range children {
			if mc, ok := m.maskAttr(c); ok {
				masked = append(masked, mc)
			}
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(masked...)}, true
	}
	nv, ok := m.maskKV(a.Key, v.Any())
	if !ok {
		return slog.Attr{}, false
	}
	return slog.Any(a.Key, nv), true
}

// maskKV applies the key rules in spec order, then the value rules.
func (m logMasker) maskKV(rawKey string, val any) (any, bool) {
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
	return m.maskValue(val), true
}

func (m logMasker) maskValue(val any) any {
	switch x := val.(type) {
	case string:
		return m.maskText(x)
	case error:
		return m.maskText(x.Error())
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			if nv, ok := m.maskKV(k, v); ok {
				out[k] = nv
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = m.maskValue(v)
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

// maskText scans free text. IPv6 runs first so that the "/48" a truncated
// IPv6 leaves behind is not re-matched; the IPv4 output has no colons.
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
	s = ipv6CandidateRe.ReplaceAllStringFunc(s, m.textIP)
	return ipv4Re.ReplaceAllStringFunc(s, m.textIP)
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

func (m logMasker) textIP(candidate string) string {
	ip := net.ParseIP(candidate)
	if ip == nil {
		return candidate
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

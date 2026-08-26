package services

import "strconv"

// SenderProfile is one configured sender: transport and identity together
// (ADR-0019 D2). PR 2 decodes it from the email.senders record list; while
// the roster is empty it is synthesized from the flat legacy keys (D6).
type SenderProfile struct {
	Slug       string   // element key segment; LegacySlug for the legacy profile
	Label      string   // operator display name
	Provider   string   // driver name: "noop" | "smtp" (| "mailup", PR 3)
	Categories []string // normalized routing patterns; "*" marks the default

	FromAddress string
	FromName    string
	ReplyTo     string

	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPTLSMode  string // "starttls" | "tls" | "none"
}

// Sub-field keys of one email.senders element. They are the record-list
// item keys (PR 2), the names a driver's Requires() speaks, and the
// argument Field accepts — one vocabulary, three readers.
const (
	SubProvider     = "provider"
	SubCategories   = "categories"
	SubFromAddress  = "from_address"
	SubFromName     = "from_name"
	SubReplyTo      = "reply_to"
	SubSMTPHost     = "smtp_host"
	SubSMTPPort     = "smtp_port"
	SubSMTPTLSMode  = "smtp_tls_mode"
	SubSMTPUsername = "smtp_username"
	SubSMTPPassword = "smtp_password"
)

// LegacySlug names the profile synthesized from the flat email.* keys. The
// leading underscore is outside the record-list slug grammar
// (^[a-z0-9]+(-[a-z0-9]+)*$), so no roster element can ever mint it: the
// legacy profile and a profile an operator labels "Default" cannot collide,
// in the resolver or in the delivery log.
const LegacySlug = "_legacy"

// Field returns the value stored under a sub-field key; "" when unset or
// unknown. A zero port is unset.
func (p SenderProfile) Field(key string) string {
	switch key {
	case SubProvider:
		return p.Provider
	case SubFromAddress:
		return p.FromAddress
	case SubFromName:
		return p.FromName
	case SubReplyTo:
		return p.ReplyTo
	case SubSMTPHost:
		return p.SMTPHost
	case SubSMTPPort:
		if p.SMTPPort == 0 {
			return ""
		}
		return strconv.Itoa(p.SMTPPort)
	case SubSMTPTLSMode:
		return p.SMTPTLSMode
	case SubSMTPUsername:
		return p.SMTPUsername
	case SubSMTPPassword:
		return p.SMTPPassword
	}
	return ""
}

// setField is Field's inverse, used by the record-list decoder (PR 2).
// Categories are not a scalar and are set by the decoder directly.
func (p *SenderProfile) setField(key, v string) {
	switch key {
	case SubProvider:
		p.Provider = v
	case SubFromAddress:
		p.FromAddress = v
	case SubFromName:
		p.FromName = v
	case SubReplyTo:
		p.ReplyTo = v
	case SubSMTPHost:
		p.SMTPHost = v
	case SubSMTPPort:
		p.SMTPPort, _ = strconv.Atoi(v)
	case SubSMTPTLSMode:
		p.SMTPTLSMode = v
	case SubSMTPUsername:
		p.SMTPUsername = v
	case SubSMTPPassword:
		p.SMTPPassword = v
	}
}

// LegacyProfile stamps the identity of the profile synthesized from the
// flat keys: LegacySlug, the "*" pattern, and — the one normalization —
// an empty provider reads as noop, which is how Send has always treated it.
func LegacyProfile(p SenderProfile) SenderProfile {
	p.Slug = LegacySlug
	p.Label = "Legacy default (flat email.* keys)"
	p.Categories = []string{"*"}
	if p.Provider == "" {
		p.Provider = "noop"
	}
	return p
}

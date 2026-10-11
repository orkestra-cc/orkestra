package services

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
)

const (
	SIWCModeDisabled      = "disabled"
	SIWCModeLoopbackLocal = "loopback_local"
	SIWCModeHostedPaste   = "hosted_paste"

	// SIWCCallbackPath is fixed: OpenAI binds scheme, host and path at
	// registration and lets only the port vary.
	SIWCCallbackPath = "/v1/llm/accounts/openai/callback"
	SIWCLoopbackHost = "127.0.0.1"

	// SIWCDefaultPastePort is the hosted_paste redirect port when none is
	// configured (the Codex CLI uses 1455, so this one stays clear of it).
	SIWCDefaultPastePort = 14555

	// Config keys the validation errors point at.
	SIWCFieldMode      = "siwc_mode"
	SIWCFieldPastePort = "siwc_paste_port"
	SIWCFieldAllowHost = "allow_hosted"
)

var (
	ErrSIWCDisabled              = errors.New("llm: Sign in with ChatGPT is disabled")
	ErrSIWCUnsupportedDeployment = errors.New("llm: loopback_local requires the operator API to be reached as http://127.0.0.1:<port>")
	ErrSIWCConfigInvalid         = errors.New("llm: invalid Sign in with ChatGPT configuration")
)

// SIWCConfigError is the typed validation failure: it names the offending
// config field (so callers never match on message text), carries an
// operator-facing reason, and unwraps to ErrSIWCConfigInvalid or
// ErrSIWCUnsupportedDeployment.
type SIWCConfigError struct {
	Field  string
	Reason string
	kind   error
}

func (e *SIWCConfigError) Error() string { return e.kind.Error() + ": " + e.Reason }
func (e *SIWCConfigError) Unwrap() error { return e.kind }

func invalidSIWC(field, format string, args ...any) error {
	return &SIWCConfigError{Field: field, Reason: fmt.Sprintf(format, args...), kind: ErrSIWCConfigInvalid}
}

// SIWCConfig is the live Sign in with ChatGPT setting set. OperatorPublicURL
// is the operator API origin (OPERATOR_API_URL).
type SIWCConfig struct {
	Mode              string
	PastePort         int
	OperatorPublicURL string
	AllowHosted       bool
	ProductionLike    bool
}

func (c SIWCConfig) Enabled() bool {
	return c.Mode == SIWCModeLoopbackLocal || c.Mode == SIWCModeHostedPaste
}

// CallbackRouteMounted: the public GET callback exists only when the API
// itself listens on the loopback the browser is redirected to.
func (c SIWCConfig) CallbackRouteMounted() bool { return c.Mode == SIWCModeLoopbackLocal }

func loopbackPort(raw string) (int, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Hostname() != SIWCLoopbackHost || (u.Path != "" && u.Path != "/") {
		return 0, ErrSIWCUnsupportedDeployment
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return 0, ErrSIWCUnsupportedDeployment
	}
	return n, nil
}

// RedirectURI is the authorize redirect_uri: always http://127.0.0.1, never
// localhost; loopback_local follows the API's own port, hosted_paste the
// configured paste port.
func (c SIWCConfig) RedirectURI() (string, error) {
	switch c.Mode {
	case SIWCModeLoopbackLocal:
		port, err := loopbackPort(c.OperatorPublicURL)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("http://%s:%d%s", SIWCLoopbackHost, port, SIWCCallbackPath), nil
	case SIWCModeHostedPaste:
		return fmt.Sprintf("http://%s:%d%s", SIWCLoopbackHost, c.PastePort, SIWCCallbackPath), nil
	}
	return "", ErrSIWCDisabled
}

// ValidateSIWCConfig refuses a contradictory or unsupported configuration.
// Failures are *SIWCConfigError carrying the offending field.
func ValidateSIWCConfig(c SIWCConfig) error {
	switch c.Mode {
	case SIWCModeDisabled:
		return nil
	case SIWCModeLoopbackLocal, SIWCModeHostedPaste:
	default:
		return invalidSIWC(SIWCFieldMode, "siwc_mode must be disabled, loopback_local or hosted_paste")
	}
	if !c.AllowHosted {
		return invalidSIWC(SIWCFieldMode, "siwc_mode requires Allow hosted providers to be enabled")
	}
	if c.Mode == SIWCModeLoopbackLocal {
		if _, err := loopbackPort(c.OperatorPublicURL); err != nil {
			return &SIWCConfigError{
				Field:  SIWCFieldMode,
				Reason: "loopback_local requires OPERATOR_API_URL to be exactly http://127.0.0.1:<port> (never localhost)",
				kind:   ErrSIWCUnsupportedDeployment,
			}
		}
		return nil
	}
	if c.PastePort < 1024 || c.PastePort > 65535 {
		return invalidSIWC(SIWCFieldPastePort, "siwc_paste_port must be between 1024 and 65535")
	}
	if c.ProductionLike {
		if u, err := url.Parse(c.OperatorPublicURL); err != nil || u.Scheme != "https" {
			return invalidSIWC(SIWCFieldMode, "hosted_paste requires OPERATOR_API_URL to be an HTTPS URL in production-like environments")
		}
	}
	return nil
}

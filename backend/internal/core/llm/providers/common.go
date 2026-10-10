package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// transportErr maps a failed round trip onto the module's sentinels. The
// caller cancelling is not a provider fault and keeps context.Canceled as
// the only identity (the router's breaker ignores it); everything else is
// "provider unavailable". The *url.Error wrapper is stripped because it
// carries the full request URL.
func transportErr(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return fmt.Errorf("llm providers: request canceled: %w", context.Canceled)
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		err = ue.Err
	}
	return fmt.Errorf("%w: %w", iface.ErrLLMProviderUnavailable, err)
}

// malformed reports a response the adapter cannot interpret. The upstream
// text is never included.
func malformed(what string) error {
	return fmt.Errorf("%w: malformed %s", iface.ErrLLMProviderUnavailable, what)
}

// normalizeArgs returns tool-call arguments as JSON-object text: empty,
// whitespace or null become "{}"; anything else must be a valid JSON object
// (a bare string, number or array is refused, never unwrapped or repaired).
func normalizeArgs(raw []byte) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "{}", nil
	}
	if raw[0] != '{' || !json.Valid(raw) {
		return "", errors.New("tool arguments are not a JSON object")
	}
	return string(raw), nil
}

// ensureUsage makes sure a reported usage is never under-counted: the budget
// is admission-based, so a provider that omits (or zeroes) a side gets the
// module-wide conservative estimate. outputBytes is everything the model
// emitted: text plus tool-call names and argument bytes.
func ensureUsage(req iface.ChatRequest, u iface.LLMUsage, outputBytes int) iface.LLMUsage {
	if u.InputTokens <= 0 {
		u.InputTokens = EstimateTokens(req)
	}
	if u.OutputTokens <= 0 {
		u.OutputTokens = (outputBytes + 2) / 3
	}
	return u
}

// ensureEmbedUsage applies the same rule to embeddings: ceil(bytes/3) over
// the inputs when the provider reported no input tokens.
func ensureEmbedUsage(inputs []string, u iface.LLMUsage) iface.LLMUsage {
	if u.InputTokens <= 0 {
		n := 0
		for _, in := range inputs {
			n += len(in)
		}
		u.InputTokens = (n + 2) / 3
	}
	return u
}

func firstFloat(vals ...*float64) *float64 {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func firstInt(vals ...*int) *int {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// captureErrorMiddleware is the vendor-neutral half of the per-call SDK
// middleware (both SDKs' option.Middleware share this exact signature). For
// an error status it reads the (bounded) body itself, keeps only the
// allowlisted identifiers in *dst and hands the SDK an empty error object,
// so that neither the SDK's unbounded read nor its Error() text (URL + raw
// body) is ever involved.
func captureErrorMiddleware(dst *ErrorInfo) func(*http.Request, func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	return func(req *http.Request, next func(*http.Request) (*http.Response, error)) (*http.Response, error) {
		resp, err := next(req)
		if err != nil || resp == nil || resp.StatusCode < 400 {
			return resp, err
		}
		*dst = ReadErrorInfo(resp)
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		resp.Body = io.NopCloser(strings.NewReader(`{"error":{}}`))
		resp.ContentLength = -1
		return resp, nil
	}
}

// sdkTransportFailure maps the failures every SDK adapter shares (caller
// cancel, deadline, the transport's own sentinels) onto the module's
// sentinels. It returns nil for anything else.
func sdkTransportFailure(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return transportErr(ctx, err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %w", iface.ErrLLMProviderUnavailable, context.DeadlineExceeded)
	case errors.Is(err, ErrStreamIdle):
		return fmt.Errorf("%w: %w", iface.ErrLLMProviderUnavailable, ErrStreamIdle)
	case errors.Is(err, ErrResponseTooLarge):
		return fmt.Errorf("%w: %w", iface.ErrLLMProviderUnavailable, ErrResponseTooLarge)
	case errors.Is(err, ErrDestinationNotPublic):
		return fmt.Errorf("%w: %w", iface.ErrLLMProviderUnavailable, ErrDestinationNotPublic)
	case errors.Is(err, ErrRedirectRefused):
		return fmt.Errorf("%w: %w", iface.ErrLLMProviderUnavailable, ErrRedirectRefused)
	}
	return nil
}

// sdkFallbackFailure is the last resort of an SDK classifier: network-level
// errors become provider-unavailable, anything unrecognised is "malformed
// response". The SDK's text is never included.
func sdkFallbackFailure(ctx context.Context, err error) error {
	var ne net.Error
	var ue *url.Error
	if errors.As(err, &ue) || errors.As(err, &ne) {
		return transportErr(ctx, err)
	}
	return malformed("response")
}

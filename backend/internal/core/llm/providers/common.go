package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

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

package providers

import (
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

// normalizeArgs turns whatever a provider sent as tool-call arguments into
// JSON-object text: absent/null -> "{}", a JSON string holding JSON is
// unwrapped, anything else must already be valid JSON.
func normalizeArgs(raw []byte) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "{}", nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", err
		}
		return normalizeArgs([]byte(s))
	}
	if !json.Valid(raw) {
		return "", errors.New("invalid json")
	}
	return string(raw), nil
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

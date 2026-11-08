package iface

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Push channel contract. The core notification module implements
// PushSender (FCM); addons depend on this interface, never on the module.

// Sentinel errors of the push channel.
var (
	// ErrPushInvalidRequest wraps every PushRequest.Validate failure.
	ErrPushInvalidRequest = errors.New("push: invalid request")
	// ErrPushUnavailable means push delivery is not configured or the
	// provider is down; callers may retry later or fall back to email.
	ErrPushUnavailable = errors.New("push: channel unavailable")
)

// PushRequest limits.
const (
	PushMaxUsers     = 500
	PushMaxTitleLen  = 200  // characters (runes)
	PushMaxBodyLen   = 1000 // characters (runes)
	PushMaxDataBytes = 3 * 1024

	PushTierOperator = "operator"
	PushTierClient   = "client"

	PushTypeTransactional = "transactional"
	PushTypeMarketing     = "marketing"

	PushPriorityHigh   = "high"
	PushPriorityNormal = "normal"

	PushStatusDelivered         = "delivered"
	PushStatusPartial           = "partial"
	PushStatusFailed            = "failed"
	PushStatusSkippedPreference = "skipped_preference"
	PushStatusSkippedNoDevice   = "skipped_no_device"
	PushStatusSkippedDuplicate  = "skipped_duplicate"
)

// PushRequest asks the notification module to push a message to every
// registered device of the listed users in one tier.
type PushRequest struct {
	Tier      string   // "operator" | "client"
	UserUUIDs []string // 1..500
	// Category is the notification preference category the user can mute.
	Category string
	// Type is "transactional" | "marketing"; marketing honours opt-out.
	Type  string
	Title string // required, <= 200 characters
	Body  string // <= 1000 characters
	// Data is the custom payload. Limit: sum of len(key)+len(value) in
	// bytes <= 3 KiB. Keys "from", "notification", "message_type" and the
	// prefixes "google." and "gcm." are reserved by FCM and rejected.
	Data        map[string]string
	CollapseKey string
	// Priority is "" (treated as high) | "high" | "normal".
	Priority string
	// TTL is the provider time-to-live; 0 = FCM default.
	TTL time.Duration
	// IdempotencyKey, when set, makes a repeated request a no-op
	// (per-user status skipped_duplicate).
	IdempotencyKey string
}

// PushUserResult is the outcome for one user.
type PushUserResult struct {
	UserUUID string
	// Status: delivered | partial | failed | skipped_preference |
	// skipped_no_device | skipped_duplicate.
	Status    string
	Attempted int
	Delivered int
	Disabled  int // device tokens disabled because the provider rejected them
	Error     string
}

// PushResult carries one PushUserResult per requested user.
type PushResult struct {
	Users []PushUserResult
}

// PushSender is implemented by the notification module.
type PushSender interface {
	SendPush(ctx context.Context, req PushRequest) (*PushResult, error)
}

// SessionActivityReader reports which auth sessions are still usable.
// Implemented by the auth tier services. The value is true only when the
// session doc is isActive and not expired; unknown ids are absent from the
// map (or false). Empty input returns an empty map without any query.
type SessionActivityReader interface {
	ActiveSessions(ctx context.Context, sessionIDs []string) (map[string]bool, error)
}

func pushInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrPushInvalidRequest, fmt.Sprintf(format, args...))
}

// Validate enforces the request limits. Every error wraps
// ErrPushInvalidRequest.
func (r PushRequest) Validate() error {
	switch r.Tier {
	case PushTierOperator, PushTierClient:
	default:
		return pushInvalid("tier %q must be operator or client", r.Tier)
	}
	if len(r.UserUUIDs) == 0 {
		return pushInvalid("at least one user is required")
	}
	if len(r.UserUUIDs) > PushMaxUsers {
		return pushInvalid("at most %d users per request, got %d", PushMaxUsers, len(r.UserUUIDs))
	}
	for _, u := range r.UserUUIDs {
		if u == "" {
			return pushInvalid("empty user uuid")
		}
	}
	if strings.TrimSpace(r.Category) == "" {
		return pushInvalid("category is required")
	}
	switch r.Type {
	case PushTypeTransactional, PushTypeMarketing:
	default:
		return pushInvalid("type %q must be transactional or marketing", r.Type)
	}
	if strings.TrimSpace(r.Title) == "" {
		return pushInvalid("title is required")
	}
	if n := utf8.RuneCountInString(r.Title); n > PushMaxTitleLen {
		return pushInvalid("title is %d characters, max %d", n, PushMaxTitleLen)
	}
	if n := utf8.RuneCountInString(r.Body); n > PushMaxBodyLen {
		return pushInvalid("body is %d characters, max %d", n, PushMaxBodyLen)
	}
	switch r.Priority {
	case "", PushPriorityHigh, PushPriorityNormal:
	default:
		return pushInvalid("priority %q must be high or normal", r.Priority)
	}
	if r.TTL < 0 {
		return pushInvalid("ttl must not be negative")
	}
	size := 0
	for k, v := range r.Data {
		if k == "" {
			return pushInvalid("empty data key")
		}
		switch k {
		case "from", "notification", "message_type":
			return pushInvalid("data key %q is reserved", k)
		}
		if strings.HasPrefix(k, "google.") || strings.HasPrefix(k, "gcm.") {
			return pushInvalid("data key %q uses a reserved prefix", k)
		}
		size += len(k) + len(v)
	}
	if size > PushMaxDataBytes {
		return pushInvalid("data is %d bytes, max %d", size, PushMaxDataBytes)
	}
	return nil
}

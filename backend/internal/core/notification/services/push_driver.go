package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// PushMessage is one device notification. Platform is informational ("android",
// "ios", "web"); FCM v1 takes the same body for all of them.
type PushMessage struct {
	Token, Title, Body string
	Data               map[string]string
	CollapseKey        string
	Priority           string // "high" | "normal" (default normal)
	TTL                time.Duration
	Platform           string
}

// PushOutcome is the per-message result, in the order of the input slice.
// Permanent means the token is dead (or the message can never succeed) and
// retrying is pointless; Reason is a stable machine-readable label.
type PushOutcome struct {
	Delivered bool
	Permanent bool
	Reason    string // fcm_unregistered | fcm_invalid | fcm_bad_request | fcm_auth | fcm_error | transient
	Err       error  // bounded, never contains the token or credentials
}

// PushDriver delivers push messages through one provider.
type PushDriver interface {
	Name() string
	Send(ctx context.Context, msgs []PushMessage) []PushOutcome // same order as msgs
}

// tokenPrefix is the only part of a push token that may reach a log.
func tokenPrefix(tok string) string {
	if len(tok) > 8 {
		return tok[:8]
	}
	return tok
}

// Config keys of the Push group.
const (
	PushProviderField    = "push.provider"
	PushFCMSAField       = "push.fcm.service_account_json"
	PushFCMProjectField  = "push.fcm.project_id"
	pushProviderNoop     = "noop"
	pushProviderFCM      = "fcm"
	pushErrBound         = 512
	pushFCMDriverMaxKeep = 4
)

type pushSettings struct {
	Provider  string `module:"push.provider"`
	SAJSON    string `module:"push.fcm.service_account_json"`
	ProjectID string `module:"push.fcm.project_id"`
}

// PushDriverFactory builds the active push driver from ONE read of the
// module's config document, so provider, credentials and project come from
// the same environment. FCM drivers (and their OAuth token sources) are
// cached by a hash of credentials+project so access tokens are reused.
type PushDriverFactory struct {
	get    SnapshotGetter
	logger *slog.Logger
	opt    FCMOptions

	mu    sync.Mutex
	cache map[string]PushDriver
}

func NewPushDriverFactory(get SnapshotGetter, logger *slog.Logger, opt FCMOptions) *PushDriverFactory {
	if logger == nil {
		logger = slog.Default()
	}
	return &PushDriverFactory{get: get, logger: logger, opt: opt, cache: map[string]PushDriver{}}
}

// PushDriver returns the driver for the currently configured provider.
// A read or decode failure is an error (fail closed); an unset provider is noop.
func (f *PushDriverFactory) PushDriver(ctx context.Context) (PushDriver, error) {
	if f.get == nil {
		return NewNoopPushDriver(f.logger), nil
	}
	doc, err := f.get(ctx)
	if err != nil {
		return nil, fmt.Errorf("notification: read push config: %w", err)
	}
	if doc == nil {
		return NewNoopPushDriver(f.logger), nil
	}
	var ps pushSettings
	if err := module.UnmarshalConfig(doc.ConfigSchema, doc.ActiveConfigValues(), doc.ActiveEncryptedValues(), &ps); err != nil {
		return nil, fmt.Errorf("notification: decode push settings: %w", err)
	}
	switch ps.Provider {
	case "", pushProviderNoop:
		return NewNoopPushDriver(f.logger), nil
	case pushProviderFCM:
		return f.fcm(ps.SAJSON, ps.ProjectID)
	default:
		return nil, fmt.Errorf("notification: unknown push provider %q", ps.Provider)
	}
}

func (f *PushDriverFactory) fcm(saJSON, projectID string) (PushDriver, error) {
	sum := sha256.Sum256([]byte(saJSON + "\x00" + projectID))
	key := hex.EncodeToString(sum[:])
	f.mu.Lock()
	defer f.mu.Unlock()
	if d, ok := f.cache[key]; ok {
		return d, nil
	}
	d, err := NewFCMPushDriver([]byte(saJSON), projectID, f.opt)
	if err != nil {
		return nil, err
	}
	if len(f.cache) >= pushFCMDriverMaxKeep {
		f.cache = map[string]PushDriver{} // credentials rotated; drop stale drivers
	}
	f.cache[key] = d
	return d, nil
}

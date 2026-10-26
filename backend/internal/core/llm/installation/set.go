package installation

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	// DefaultVerifyInterval is how long a positive verification is reused.
	DefaultVerifyInterval = time.Minute
	// DefaultFailureBackoff is how long a failed verification is returned again.
	DefaultFailureBackoff = 5 * time.Second
	// DefaultVerifyTimeout bounds one verification, detached from the caller.
	DefaultVerifyTimeout = 10 * time.Second
	// ProbeText is the single input embedded to measure the dimensions.
	ProbeText = "orkestra inference probe"
)

// Options tunes a Set; zero values select the defaults.
type Options struct {
	VerifyInterval, FailureBackoff, VerifyTimeout time.Duration
	Now                                           func() time.Time
}

// Verified is an endpoint's profile checked against what the server reports.
type Verified struct {
	Profile   iface.InferenceProfile // declared fields + ID + Dimensions (embed) + MaxInputTokens
	BatchSize int                    // max_client_batch_size, TEIDefaultBatchSize when <= 0
}

// entry is the verification state of one endpoint.
type entry struct {
	ep      Endpoint
	adapter *providers.TEI

	mu       sync.Mutex
	ok       *Verified
	okAt     time.Time
	failErr  error
	failAt   time.Time
	lastDims int // last measured dimensions; survives Invalidate
}

// Set holds the installation endpoints and verifies them against TEI /info.
// The endpoint map is immutable after NewSet.
type Set struct {
	entries map[string]*entry
	byID    map[string]string
	opts    Options
	flights singleflight.Group
}

// NewSet builds the set. client must come from providers.NewInstallationHTTPClient.
func NewSet(eps []Endpoint, client *http.Client, opts Options) *Set {
	if opts.VerifyInterval <= 0 {
		opts.VerifyInterval = DefaultVerifyInterval
	}
	if opts.FailureBackoff <= 0 {
		opts.FailureBackoff = DefaultFailureBackoff
	}
	if opts.VerifyTimeout <= 0 {
		opts.VerifyTimeout = DefaultVerifyTimeout
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	s := &Set{entries: make(map[string]*entry, len(eps)), byID: make(map[string]string, len(eps)), opts: opts}
	for _, ep := range eps {
		e := &entry{ep: ep}
		if ep.Kind != iface.InferenceKindChat {
			if a, err := providers.NewTEI(ep.URL, ep.APIKey, client); err == nil {
				e.adapter = a
			}
		}
		s.entries[ep.Name] = e
		s.byID[ep.Profile().ID] = ep.Name
	}
	return s
}

// Len is the number of endpoints.
func (s *Set) Len() int { return len(s.entries) }

// Endpoint returns the declared endpoint of that name.
func (s *Set) Endpoint(name string) (Endpoint, bool) {
	e, ok := s.entries[name]
	if !ok {
		return Endpoint{}, false
	}
	return e.ep, true
}

// ByProfile returns the endpoint declaring that profile ID.
func (s *Set) ByProfile(id string) (Endpoint, bool) {
	name, ok := s.byID[id]
	if !ok {
		return Endpoint{}, false
	}
	return s.Endpoint(name)
}

// HasKind reports whether any endpoint has that kind.
func (s *Set) HasKind(k iface.InferenceKind) bool {
	for _, e := range s.entries {
		if e.ep.Kind == k {
			return true
		}
	}
	return false
}

// Adapter returns the TEI adapter of an embed or rerank endpoint, else nil.
func (s *Set) Adapter(name string) *providers.TEI {
	if e, ok := s.entries[name]; ok {
		return e.adapter
	}
	return nil
}

// Invalidate drops the cached success of an endpoint; the last measured
// dimensions are kept so a change is still detected.
func (s *Set) Invalidate(name string) {
	e, ok := s.entries[name]
	if !ok {
		return
	}
	e.mu.Lock()
	e.ok = nil
	e.mu.Unlock()
}

// Verify returns the endpoint's verified profile (a copy), reusing a recent
// outcome. One verification per endpoint runs at a time, detached from the
// caller's cancellation.
func (s *Set) Verify(ctx context.Context, name string) (*Verified, error) {
	e, ok := s.entries[name]
	if !ok {
		return nil, fmt.Errorf("%w: unknown inference endpoint", iface.ErrLLMNotConfigured)
	}
	if e.ep.Kind == iface.InferenceKindChat {
		return nil, fmt.Errorf("%w: chat endpoints are served from delivery step 3", iface.ErrLLMNotConfigured)
	}
	if v, err, hit := s.cached(e); hit {
		return v, err
	}

	ch := s.flights.DoChan(name, func() (any, error) {
		if v, err, hit := s.cached(e); hit { // a flight that just finished
			return v, err
		}
		vctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.opts.VerifyTimeout)
		defer cancel()
		v, err := s.verify(vctx, e)
		s.store(e, v, err)
		return v, err
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.Err != nil {
			return nil, r.Err
		}
		return copyVerified(r.Val.(*Verified)), nil
	}
}

func copyVerified(v *Verified) *Verified {
	c := *v
	return &c
}

// cached returns a fresh cached outcome, as a copy.
func (s *Set) cached(e *entry) (*Verified, error, bool) {
	now := s.opts.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ok != nil && now.Sub(e.okAt) < s.opts.VerifyInterval {
		return copyVerified(e.ok), nil, true
	}
	if e.failErr != nil && now.Sub(e.failAt) < s.opts.FailureBackoff {
		return nil, e.failErr, true
	}
	return nil, nil, false
}

func (s *Set) store(e *entry, v *Verified, err error) {
	now := s.opts.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	if err != nil {
		e.failErr, e.failAt = err, now
		return
	}
	e.ok, e.okAt = copyVerified(v), now
	e.failErr = nil
	if v.Profile.Dimensions > 0 {
		e.lastDims = v.Profile.Dimensions
	}
}

func mismatch(field string) error {
	return fmt.Errorf("%w: %s", iface.ErrLLMProfileMismatch, field)
}

func (s *Set) verify(ctx context.Context, e *entry) (*Verified, error) {
	ep := e.ep
	info, err := e.adapter.Info(ctx)
	if err != nil {
		return nil, err
	}
	if info.ModelID != ep.Model {
		return nil, mismatch("model_id")
	}
	if info.ModelSHA == nil || *info.ModelSHA != ep.Revision {
		return nil, mismatch("model_sha")
	}
	p := ep.Profile()
	p.MaxInputTokens = info.MaxInputLength
	switch ep.Kind {
	case iface.InferenceKindEmbed:
		if info.ModelType.Embedding == nil {
			return nil, mismatch("model_type")
		}
		if info.ModelType.Embedding.Pooling != ep.Pooling {
			return nil, mismatch("pooling")
		}
		out, err := e.adapter.Embed(ctx, []string{ProbeText}, providers.TEIEmbedOptions{
			Normalize: ep.Normalize, Truncate: ep.Truncate, BatchSize: 1,
		})
		if err != nil {
			return nil, err
		}
		if len(out.Vectors) != 1 || len(out.Vectors[0]) == 0 {
			return nil, fmt.Errorf("%w: probe returned no vector", iface.ErrLLMProviderUnavailable)
		}
		p.Dimensions = len(out.Vectors[0])
		e.mu.Lock()
		prev := e.lastDims
		e.mu.Unlock()
		if prev != 0 && prev != p.Dimensions {
			return nil, mismatch("dimensions")
		}
	case iface.InferenceKindRerank:
		if len(info.ModelType.Reranker) == 0 {
			return nil, mismatch("model_type")
		}
	}
	bs := info.MaxClientBatchSize
	if bs <= 0 {
		bs = providers.TEIDefaultBatchSize
	}
	return &Verified{Profile: p, BatchSize: bs}, nil
}

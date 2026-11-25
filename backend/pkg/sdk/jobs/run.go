package jobs

import (
	"errors"
	"math"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxDetailKeys     = 32
	maxDetailStringLn = 256
)

// Trigger says why a run started.
type Trigger string

const (
	TriggerSchedule Trigger = "schedule"
	TriggerManual   Trigger = "manual"
	TriggerRetry    Trigger = "retry"
	TriggerCatchup  Trigger = "catchup"
)

// Run is the handle a job body receives for one attempt. It carries identity
// and a small bounded map of details the body reports; details must never
// hold personal data.
type Run struct {
	key          string
	id           string
	trigger      Trigger
	attempt      int
	scheduledFor time.Time

	mu      sync.Mutex
	details map[string]any
	dropped int
}

// NewRun builds a Run. The jobs module uses it; tests of job bodies may too.
func NewRun(key, id string, trigger Trigger, attempt int, scheduledFor time.Time) *Run {
	return &Run{key: key, id: id, trigger: trigger, attempt: attempt, scheduledFor: scheduledFor, details: map[string]any{}}
}

func (r *Run) Key() string             { return r.key }
func (r *Run) ID() string              { return r.id }
func (r *Run) Trigger() Trigger        { return r.trigger }
func (r *Run) Attempt() int            { return r.attempt }
func (r *Run) ScheduledFor() time.Time { return r.scheduledFor }

// Detail records a value under key. It is safe for concurrent use. At most 32
// keys are kept; strings are cut to 256 runes; only strings, bools, numbers,
// time.Time and time.Duration (stored as its String()) are accepted. NaN and
// ±Inf floats and unsigned values above MaxInt64 are rejected too, so the
// details always encode as JSON. Anything rejected is dropped and counted.
func (r *Run) Detail(key string, value any) {
	v, ok := normalizeDetail(value)
	r.mu.Lock()
	defer r.mu.Unlock()
	if !ok {
		r.dropped++
		return
	}
	if _, exists := r.details[key]; !exists && len(r.details) >= maxDetailKeys {
		r.dropped++
		return
	}
	r.details[key] = v
}

// Details returns a copy of the details and the number of dropped values.
func (r *Run) Details() (map[string]any, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]any, len(r.details))
	for k, v := range r.details {
		out[k] = v
	}
	return out, r.dropped
}

func normalizeDetail(value any) (any, bool) {
	switch v := value.(type) {
	case string:
		if utf8.RuneCountInString(v) > maxDetailStringLn {
			v = string([]rune(v)[:maxDetailStringLn])
		}
		return v, true
	case bool:
		return v, true
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return unsignedDetail(uint64(v))
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		return unsignedDetail(v)
	case float32:
		return floatDetail(float64(v))
	case float64:
		return floatDetail(v)
	case time.Time:
		return v, true
	case time.Duration:
		return v.String(), true
	}
	return nil, false
}

// unsignedDetail keeps an unsigned value that fits int64; a larger one would
// wrap to a negative number, so it is dropped.
func unsignedDetail(v uint64) (any, bool) {
	if v > math.MaxInt64 {
		return nil, false
	}
	return int64(v), true
}

// floatDetail drops NaN and ±Inf, which JSON cannot encode.
func floatDetail(v float64) (any, bool) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, false
	}
	return v, true
}

type permanentError struct{ err error }

func (p permanentError) Error() string { return p.err.Error() }
func (p permanentError) Unwrap() error { return p.err }

// Permanent marks err as not worth retrying. Permanent(nil) is nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanent reports whether err, or anything it wraps, was marked Permanent.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

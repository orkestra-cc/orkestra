package syncdelta

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Error codes of the sync problem envelope.
const (
	CodeConflict             = "sync.conflict"
	CodeCursorExpired        = "sync.cursor_expired"
	CodeClientTimeOutOfRange = "sync.client_time_out_of_range"
	CodeInvalidCursor        = "sync.invalid_cursor"
)

type envelope struct {
	Status int    `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Code   string `json:"code"`
}

// ConflictError is the 409 returned when If-Match does not match the current
// version. It carries the server copy so the client can merge.
type ConflictError struct {
	CurrentVersion int64
	Current        any
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("version conflict: current version is %d", e.CurrentVersion)
}

// GetStatus makes Huma treat the error as a huma.StatusError.
func (e *ConflictError) GetStatus() int { return http.StatusConflict }

func (e *ConflictError) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		envelope
		CurrentVersion int64 `json:"currentVersion"`
		Current        any   `json:"current"`
	}{
		envelope{http.StatusConflict, http.StatusText(http.StatusConflict), e.Error(), CodeConflict},
		e.CurrentVersion, e.Current,
	})
}

// ParseIfMatch reads a numeric version from an If-Match value: `"12"`, `12`
// or `W/"12"`. Wildcards, non-numbers and negatives return false.
func ParseIfMatch(h string) (int64, bool) {
	h = strings.TrimSpace(h)
	h = strings.TrimPrefix(h, "W/")
	h = strings.Trim(h, `"`)
	v, err := strconv.ParseInt(h, 10, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

// statusError is a Huma-compatible error with the sync envelope; it unwraps
// to its sentinel so errors.Is keeps working.
type statusError struct {
	envelope
	sentinel error
}

func (e *statusError) Error() string  { return e.Detail }
func (e *statusError) GetStatus() int { return e.Status }
func (e *statusError) Unwrap() error  { return e.sentinel }
func (e *statusError) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.envelope)
}

func newStatusError(status int, code, detail string, sentinel error) error {
	return &statusError{envelope{status, http.StatusText(status), detail, code}, sentinel}
}

// CursorExpiredError is the 410 response for ErrCursorExpired.
func CursorExpiredError() error {
	return newStatusError(http.StatusGone, CodeCursorExpired,
		"cursor is older than the tombstone retention; perform a full resync", ErrCursorExpired)
}

// InvalidCursorError is the 400 response for ErrInvalidCursor.
func InvalidCursorError() error {
	return newStatusError(http.StatusBadRequest, CodeInvalidCursor, "cursor is malformed", ErrInvalidCursor)
}

// ClientTimeOutOfRangeError is the 422 response for ErrClientTimeOutOfRange.
func ClientTimeOutOfRangeError() error {
	return newStatusError(http.StatusUnprocessableEntity, CodeClientTimeOutOfRange,
		"client timestamp is more than 5 minutes in the future or 30 days in the past", ErrClientTimeOutOfRange)
}

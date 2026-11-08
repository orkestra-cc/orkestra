package syncdelta

import (
	"errors"
	"time"
)

// ErrClientTimeOutOfRange is returned by ValidateClientTime.
var ErrClientTimeOutOfRange = errors.New("syncdelta: client time out of range")

const (
	maxClientFuture = 5 * time.Minute
	maxClientPast   = 30 * 24 * time.Hour
)

// ValidateClientTime rejects client-supplied timestamps more than 5 minutes
// in the future or 30 days in the past relative to now.
func ValidateClientTime(t, now time.Time) error {
	if t.After(now.Add(maxClientFuture)) || t.Before(now.Add(-maxClientPast)) {
		return ErrClientTimeOutOfRange
	}
	return nil
}

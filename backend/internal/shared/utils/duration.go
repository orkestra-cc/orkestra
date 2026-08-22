package utils

import (
	"strconv"
	"strings"
	"time"
)

// ParseDuration accepts everything time.ParseDuration does, plus a
// trailing "d" for days. Only a bare "<number>d" is special-cased;
// compound forms ("1d12h") stay unsupported rather than half-supported,
// so a value either parses exactly or is rejected.
//
// This is the single parser for durations that reach Orkestra from a
// human: environment variables, module config values typed into the
// admin UI, and defaults declared in ConfigSchema. Before ADR-0017 the
// env path accepted "30d" and the admin path did not, so the same
// string meant two different things depending on where it was typed.
func ParseDuration(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if days, ok := strings.CutSuffix(raw, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil {
			return 0, false
		}
		return time.Duration(n * float64(24*time.Hour)), true
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false
	}
	return d, true
}

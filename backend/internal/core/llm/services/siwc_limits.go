package services

import (
	"context"
	"time"
)

// Sign in with ChatGPT rate limits (spec Global Constraints). Every one goes
// through RateLimiter.Check and so surfaces as a *RateLimitedError, which
// the handlers' MapError turns into 429 + Retry-After. The limiter adds the
// "llm:rl:" prefix itself. The handler wiring is Task 8.
const (
	SIWCLinkLimit     = 5  // per user
	SIWCCompleteLimit = 10 // per user
	SIWCCallbackLimit = 20 // per client IP
	SIWCLimitWindow   = 10 * time.Minute

	siwcMaxIPKeyLen = 64
)

func SIWCLinkLimitKey(userUUID string) string     { return "siwc_link:" + userUUID }
func SIWCCompleteLimitKey(userUUID string) string { return "siwc_complete:" + userUUID }
func SIWCCallbackLimitKey(ip string) string {
	if ip == "" {
		ip = "unknown"
	}
	if len(ip) > siwcMaxIPKeyLen {
		ip = ip[:siwcMaxIPKeyLen]
	}
	return "siwc_callback:" + ip
}

// CheckSIWCLink counts one link start for the user.
func (l *RateLimiter) CheckSIWCLink(ctx context.Context, userUUID string) error {
	return l.Check(ctx, SIWCLinkLimitKey(userUUID), SIWCLinkLimit, SIWCLimitWindow)
}

// CheckSIWCComplete counts one paste-completion for the user.
func (l *RateLimiter) CheckSIWCComplete(ctx context.Context, userUUID string) error {
	return l.Check(ctx, SIWCCompleteLimitKey(userUUID), SIWCCompleteLimit, SIWCLimitWindow)
}

// CheckSIWCCallback counts one loopback callback for the client IP.
func (l *RateLimiter) CheckSIWCCallback(ctx context.Context, ip string) error {
	return l.Check(ctx, SIWCCallbackLimitKey(ip), SIWCCallbackLimit, SIWCLimitWindow)
}

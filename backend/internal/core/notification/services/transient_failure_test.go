package services

import (
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Only failures that provably left nothing at the provider are transient: a
// retry of an ambiguous one (timeout, 500 after the request was written) could
// deliver the message twice.
func TestIsTransientSendFailure(t *testing.T) {
	dialErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"http 429", vendorEnvelopeError("smtp2go", 429, "error", ""), true},
		{"http 502", vendorBodyError("smtp2go", 502, bodyUnparseable, 10, "text/html"), true},
		{"http 503", vendorEnvelopeError("smtp2go", 503, "error", ""), true},
		{"http 504", vendorBodyError("smtp2go", 504, bodyTooLarge, 0, ""), true},
		{"http 500 is ambiguous", vendorEnvelopeError("smtp2go", 500, "error", ""), false},
		{"http 400 is a verdict", vendorEnvelopeError("smtp2go", 400, "error", "E_ApiResponseCodes.ENDPOINT_PERMISSION_DENIED"), false},
		{"http 401", vendorEnvelopeError("smtp2go", 401, "error", ""), false},
		{"smtp 421", rejectionError("smtp", "rcpt", 421, errors.New("x")), true},
		{"smtp 451", rejectionError("smtp", "data", 451, errors.New("x")), true},
		{"smtp 550", rejectionError("smtp", "rcpt", 550, errors.New("x")), false},
		{"dial never connected", transportError("smtp2go", "", dialErr), true},
		{"timeout is ambiguous", transportError("smtp2go", "", timeoutErr{}), false},
		{"io is ambiguous", transportError("smtp2go", "read", errors.New("unexpected EOF")), false},
		{"opt-out store unreadable", ErrOptoutLookupUnavailable, true},
		{"unsubscribe token store unreadable", fmt.Errorf("wrap: %w", ErrUnsubscribeTokenUnavailable), true},
		{"sender config unreadable", ErrSenderConfigUnavailable, true},
		{"sender not configured", ErrSenderNotConfigured, false},
		{"attachment rejected", ErrAttachmentRejected, false},
		{"plain error", errors.New("boom"), false},
	}
	for _, c := range cases {
		if got := isTransientSendFailure(c.err); got != c.want {
			t.Errorf("%s: isTransientSendFailure = %v, want %v", c.name, got, c.want)
		}
	}
}

// The classification rides on the result AND the log row, so the idempotent
// replay of the failed attempt returns it too.
func TestSend_TransientDriverFailure_ClassifiedOnResultAndLog(t *testing.T) {
	k := newKit(Options{})
	k.driver.sendErr = vendorEnvelopeError("noop", 429, "error", "")
	res, err := k.svc.Send(ctx(), reqWith())
	if err == nil || res == nil || res.Status != "failed" || res.FailureReason != iface.FailureTransient {
		t.Fatalf("res=%+v err=%v, want failed/transient", res, err)
	}
	if len(k.logRepo.created) != 1 || k.logRepo.created[0].FailureReason != iface.FailureTransient {
		t.Fatalf("log row must carry failureReason=transient: %+v", k.logRepo.created)
	}
}

func TestSend_PermanentDriverFailure_NotTransient(t *testing.T) {
	k := newKit(Options{})
	k.driver.sendErr = vendorEnvelopeError("noop", 400, "error", "")
	res, _ := k.svc.Send(ctx(), reqWith())
	if res == nil || res.FailureReason != "" {
		t.Fatalf("res=%+v, want no failure reason for a 400", res)
	}
}

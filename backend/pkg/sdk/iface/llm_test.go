package iface

import (
	"errors"
	"fmt"
	"testing"
)

func TestLLMSentinels_AreDistinctAndWrappable(t *testing.T) {
	all := []error{
		ErrLLMNotConfigured, ErrLLMNoEligibleModel, ErrLLMModelAccessDenied,
		ErrLLMUserAccountRequired, ErrLLMPlanUsageDisabled, ErrLLMUserAccountReauth,
		ErrLLMBudgetExceeded, ErrLLMQuotaExhausted, ErrLLMCapabilityMismatch,
		ErrLLMPartialOutput, ErrLLMProviderUnavailable, ErrLLMInvalidRequest,
	}
	seen := map[string]bool{}
	for _, e := range all {
		if seen[e.Error()] {
			t.Fatalf("duplicate sentinel message %q", e.Error())
		}
		seen[e.Error()] = true
		wrapped := fmt.Errorf("ctx: %w", e)
		if !errors.Is(wrapped, e) {
			t.Fatalf("errors.Is must see through wrapping for %v", e)
		}
	}
}

func TestChatRequest_ZeroPurposeMeansDefault(t *testing.T) {
	var r ChatRequest
	if r.EffectivePurpose() != "default" {
		t.Fatalf("EffectivePurpose() = %q, want default", r.EffectivePurpose())
	}
	r.Purpose = "summarize"
	if r.EffectivePurpose() != "summarize" {
		t.Fatalf("EffectivePurpose() = %q, want summarize", r.EffectivePurpose())
	}
}

package models

import (
	"errors"
	"testing"
	"time"
)

func validCardType() *CardType {
	return &CardType{
		UUID:        "type-1",
		Key:         "premium_member",
		DisplayName: "Premium Member",
		CodeFormat:  "PREM-{YYYY}-{seq:05}",
	}
}

func validCard() *Card {
	return &Card{
		UUID:         "card-1",
		CardTypeUUID: "type-1",
		Code:         "PREM-2026-00042",
		PersonUUID:   "person-1",
		Status:       CardStatusActive,
		IssuedAt:     time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
		IssuedBy:     "user-admin-1",
	}
}

func TestCardType_Validate_OK(t *testing.T) {
	if err := validCardType().Validate(); err != nil {
		t.Fatalf("valid card type should pass: %v", err)
	}
}

func TestCardType_Validate_Missing(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*CardType)
	}{
		{"nil", func(c *CardType) { *c = CardType{} }},
		{"missing key", func(c *CardType) { c.Key = "" }},
		{"missing displayName", func(c *CardType) { c.DisplayName = "" }},
		{"missing codeFormat", func(c *CardType) { c.CodeFormat = "" }},
		{"invalid key uppercase", func(c *CardType) { c.Key = "Premium_Member" }},
		{"invalid key punctuation", func(c *CardType) { c.Key = "premium-member" }},
		{"empty tier", func(c *CardType) { c.Tiers = []string{"gold", ""} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCardType()
			tc.mutate(c)
			err := c.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !errors.Is(err, ErrInvalidCardType) {
				t.Errorf("error chain missing ErrInvalidCardType: %v", err)
			}
		})
	}
}

func TestCardType_Validate_NilReceiver(t *testing.T) {
	var c *CardType
	if err := c.Validate(); !errors.Is(err, ErrInvalidCardType) {
		t.Errorf("nil receiver should return ErrInvalidCardType, got %v", err)
	}
}

func TestCard_Validate_OK(t *testing.T) {
	if err := validCard().Validate(); err != nil {
		t.Fatalf("valid card should pass: %v", err)
	}
}

func TestCard_Validate_Missing(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Card)
	}{
		{"missing cardTypeUuid", func(c *Card) { c.CardTypeUUID = "" }},
		{"missing code", func(c *Card) { c.Code = "" }},
		{"missing personUuid", func(c *Card) { c.PersonUUID = "" }},
		{"invalid status", func(c *Card) { c.Status = "bogus" }},
		{"zero issuedAt", func(c *Card) { c.IssuedAt = time.Time{} }},
		{"missing issuedBy", func(c *Card) { c.IssuedBy = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCard()
			tc.mutate(c)
			err := c.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !errors.Is(err, ErrInvalidCard) {
				t.Errorf("error chain missing ErrInvalidCard: %v", err)
			}
		})
	}
}

// TestCanTransitionCardStatus walks every (from, to) pair against the
// matrix documented in §3.6 of the Phase 4 plan and on
// CanTransitionCardStatus. Self-transitions are always rejected;
// revoked is terminal.
func TestCanTransitionCardStatus(t *testing.T) {
	type pair struct {
		from, to CardStatus
		want     bool
	}
	cases := []pair{
		// active → ...
		{CardStatusActive, CardStatusActive, false},
		{CardStatusActive, CardStatusSuspended, true},
		{CardStatusActive, CardStatusRevoked, true},

		// suspended → ...
		{CardStatusSuspended, CardStatusActive, true},
		{CardStatusSuspended, CardStatusSuspended, false},
		{CardStatusSuspended, CardStatusRevoked, true},

		// revoked is terminal
		{CardStatusRevoked, CardStatusActive, false},
		{CardStatusRevoked, CardStatusSuspended, false},
		{CardStatusRevoked, CardStatusRevoked, false},
	}
	for _, c := range cases {
		t.Run(string(c.from)+"->"+string(c.to), func(t *testing.T) {
			if got := CanTransitionCardStatus(c.from, c.to); got != c.want {
				t.Errorf("CanTransitionCardStatus(%q, %q) = %v, want %v", c.from, c.to, got, c.want)
			}
		})
	}
}

func TestIsKnownCardStatus(t *testing.T) {
	for _, s := range AllCardStatuses {
		if !IsKnownCardStatus(s) {
			t.Errorf("declared status %q is not in AllCardStatuses-driven IsKnownCardStatus", s)
		}
	}
	for _, s := range []CardStatus{"", "bogus", "ACTIVE"} {
		if IsKnownCardStatus(s) {
			t.Errorf("undeclared status %q should not be known", s)
		}
	}
}

package services

// Two small policy defects that both make a configured control weaker
// than the operator asked for.
//
//  1. The account-lockout threshold was admin-managed
//     (accountLockoutThreshold, plumbed into the rate limiter on every
//     login) but the branch that actually stamps User.LockedUntil
//     compared against a hardcoded 5. Lowering the policy to 3 left the
//     persisted lock at 5.
//  2. checkCharacterClasses treated "anything that is not [A-Za-z0-9]"
//     as a symbol, so a space satisfied requireSymbol, and any letter
//     outside ASCII satisfied requireSymbol while satisfying neither
//     requireUpper nor requireLower.

import (
	"context"
	"testing"
)

func TestLockout_UsesConfiguredThresholdNotHardcodedFive(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, map[string]string{
		"accountLockoutThreshold": "3",
	}, nil)
	u := env.hashedUser("locked@example.com", "correct-horse-battery")

	// Two prior failures on record; the third must trip the lock.
	u.FailedLoginCount = 2

	_, err := env.auth.Login(context.Background(), LoginInput{
		Email:    "locked@example.com",
		Password: "wrong-password",
		IP:       "203.0.113.1",
	})
	if err == nil {
		t.Fatal("a wrong password must be rejected")
	}

	if u.LockedUntil == nil {
		t.Error("reaching the configured threshold (3) must stamp LockedUntil; the hardcoded 5 is still in force")
	}
}

func TestPasswordPolicy_SpaceIsNotASymbol(t *testing.T) {
	svc := NewPasswordService(silentLogger(), false)
	pol := PasswordPolicy{MinLength: 8, MaxLength: 128, RequireSymbol: true}

	if err := checkCharacterClasses("parola chiave", pol); err == nil {
		t.Error("a space must not satisfy requireSymbol — it is whitespace, not punctuation")
	}
	if err := checkCharacterClasses("parola-chiave!", pol); err != nil {
		t.Errorf("real punctuation must satisfy requireSymbol: %v", err)
	}
	_ = svc
}

func TestPasswordPolicy_NonASCIILettersCountAsLetters(t *testing.T) {
	upper := PasswordPolicy{MinLength: 4, MaxLength: 128, RequireUpper: true}
	lower := PasswordPolicy{MinLength: 4, MaxLength: 128, RequireLower: true}
	symbol := PasswordPolicy{MinLength: 4, MaxLength: 128, RequireSymbol: true}

	// Cyrillic capitals are uppercase letters, not symbols.
	if err := checkCharacterClasses("ПАРОЛЬ", upper); err != nil {
		t.Errorf("non-ASCII uppercase must satisfy requireUpper: %v", err)
	}
	if err := checkCharacterClasses("ПАРОЛЬ", symbol); err == nil {
		t.Error("a password of only letters must not satisfy requireSymbol")
	}

	// Accented lowercase likewise.
	if err := checkCharacterClasses("passwörd", lower); err != nil {
		t.Errorf("non-ASCII lowercase must satisfy requireLower: %v", err)
	}
	if err := checkCharacterClasses("passwörd", symbol); err == nil {
		t.Error("an accented letter is not a symbol")
	}
}

func TestPasswordPolicy_AsciiClassesUnchanged(t *testing.T) {
	// Guard the common case against an over-clever rewrite.
	all := PasswordPolicy{
		MinLength: 4, MaxLength: 128,
		RequireUpper: true, RequireLower: true, RequireDigit: true, RequireSymbol: true,
	}
	if err := checkCharacterClasses("Abcdef1!", all); err != nil {
		t.Errorf("a password with every class must pass: %v", err)
	}
	if err := checkCharacterClasses("abcdef1!", all); err == nil {
		t.Error("missing uppercase must still fail")
	}
	if err := checkCharacterClasses("Abcdefg!", all); err == nil {
		t.Error("missing digit must still fail")
	}
}

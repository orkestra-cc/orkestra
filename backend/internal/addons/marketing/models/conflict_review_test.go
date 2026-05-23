package models

import (
	"errors"
	"testing"
)

// validReview returns a fully-populated ConflictReview that passes
// Validate. Each test mutates one field to exercise a specific failure
// mode.
func validReview() *ConflictReview {
	return &ConflictReview{
		UUID:          "rev-1",
		ImportJobUUID: "job-1",
		TargetKind:    ConflictTargetPerson,
		ExistingUUID:  "person-1",
		IncomingPayload: map[string]any{
			"firstName": "Jane",
			"lastName":  "Doe",
		},
		Conflicts: []ConflictField{{
			Field:         "primaryEmail",
			ExistingValue: "jane@a.example",
			IncomingValue: "j@b.example",
			Severity:      ConflictSeverityBlocking,
		}},
		Status: ConflictReviewStatusPending,
	}
}

func TestConflictReview_Validate_OK(t *testing.T) {
	if err := validReview().Validate(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestConflictReview_Validate_Failures(t *testing.T) {
	cases := []struct {
		name  string
		patch func(*ConflictReview)
	}{
		{"missing importJobUuid", func(c *ConflictReview) { c.ImportJobUUID = "" }},
		{"invalid targetKind", func(c *ConflictReview) { c.TargetKind = ConflictTargetKind("alien") }},
		{"missing existingUuid", func(c *ConflictReview) { c.ExistingUUID = "" }},
		{"empty incomingPayload", func(c *ConflictReview) { c.IncomingPayload = nil }},
		{"empty conflicts", func(c *ConflictReview) { c.Conflicts = nil }},
		{"conflict with empty field name", func(c *ConflictReview) {
			c.Conflicts = []ConflictField{{Severity: ConflictSeverityBlocking}}
		}},
		{"conflict with invalid severity", func(c *ConflictReview) {
			c.Conflicts = []ConflictField{{Field: "x", Severity: "fatal"}}
		}},
		{"invalid status", func(c *ConflictReview) { c.Status = ConflictReviewStatus("frozen") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validReview()
			tc.patch(c)
			err := c.Validate()
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, ErrInvalidConflictReview) {
				t.Fatalf("expected ErrInvalidConflictReview, got %v", err)
			}
		})
	}
}

func TestConflictResolution_Validate(t *testing.T) {
	cases := []struct {
		name    string
		res     ConflictResolution
		wantErr bool
	}{
		{"keep_existing OK", ConflictResolution{Action: ConflictActionKeepExisting}, false},
		{"take_incoming OK", ConflictResolution{Action: ConflictActionTakeIncoming}, false},
		{"manual_merge with overrides OK", ConflictResolution{
			Action:         ConflictActionManualMerge,
			FieldOverrides: map[string]any{"primaryEmail": "x@a.example"},
		}, false},
		{"dismiss OK", ConflictResolution{Action: ConflictActionDismiss}, false},
		{"unknown action", ConflictResolution{Action: ConflictAction("frob")}, true},
		{"manual_merge missing overrides", ConflictResolution{Action: ConflictActionManualMerge}, true},
		{"keep_existing with overrides", ConflictResolution{
			Action:         ConflictActionKeepExisting,
			FieldOverrides: map[string]any{"a": 1},
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.res.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
			if tc.wantErr && !errors.Is(err, ErrInvalidConflictReview) {
				t.Fatalf("expected ErrInvalidConflictReview, got %v", err)
			}
		})
	}
}

func TestConflictReview_Validate_DelegatesToResolution(t *testing.T) {
	c := validReview()
	c.Resolution = &ConflictResolution{Action: ConflictAction("frob")}
	if err := c.Validate(); err == nil || !errors.Is(err, ErrInvalidConflictReview) {
		t.Fatalf("expected ErrInvalidConflictReview, got %v", err)
	}
}

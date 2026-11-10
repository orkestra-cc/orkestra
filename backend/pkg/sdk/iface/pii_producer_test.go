package iface

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPurgeResultPendingIsOmittedWhenFalse(t *testing.T) {
	b, _ := json.Marshal(PurgeResult{RowsDeleted: 1})
	if strings.Contains(string(b), "pending") {
		t.Fatalf("got %s; pending must be omitted when false", b)
	}
	b, _ = json.Marshal(PurgeResult{Pending: true})
	if !strings.Contains(string(b), `"pending":true`) {
		t.Fatalf("got %s", b)
	}
}

// plainProducer holds data but offers neither companion.
type plainProducer struct{}

func (plainProducer) Subject() string { return "plain" }
func (plainProducer) ExportPersonalData(context.Context, string) (any, error) {
	return nil, nil
}
func (plainProducer) PurgePersonalData(context.Context, string, EraseMode) (PurgeResult, error) {
	return PurgeResult{}, nil
}

// companionProducer implements both optional companions.
type companionProducer struct {
	plainProducer
	until time.Time
}

func (companionProducer) Subject() string { return "companion" }

func (companionProducer) CountPersonalData(context.Context, string) (PersonalDataCount, error) {
	return PersonalDataCount{Rows: 3, Collections: []string{"a", "b"}}, nil
}

func (c companionProducer) RetainUntil(context.Context, string) (*RetentionObligation, error) {
	return &RetentionObligation{Until: c.until, Reason: "fiscal_records"}, nil
}

// Compile-time checks: the companion producer satisfies both optional
// interfaces, the plain one satisfies neither.
var (
	_ PIIProducerCounter   = companionProducer{}
	_ PIIRetentionReporter = companionProducer{}
	_ PIIProducer          = companionProducer{}
	_ PIIProducer          = plainProducer{}
)

func TestPIIProducerCompanionsAreOptionalTypeAssertions(t *testing.T) {
	until := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		producer    PIIProducer
		wantCounter bool
		wantRetain  bool
	}{
		{name: "plain producer has neither", producer: plainProducer{}},
		{name: "companion producer has both", producer: companionProducer{until: until}, wantCounter: true, wantRetain: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, gotCounter := tt.producer.(PIIProducerCounter)
			if gotCounter != tt.wantCounter {
				t.Fatalf("PIIProducerCounter assertion = %v, want %v", gotCounter, tt.wantCounter)
			}
			_, gotRetain := tt.producer.(PIIRetentionReporter)
			if gotRetain != tt.wantRetain {
				t.Fatalf("PIIRetentionReporter assertion = %v, want %v", gotRetain, tt.wantRetain)
			}
		})
	}
}

func TestPIIProducerCompanionValues(t *testing.T) {
	until := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	p := companionProducer{until: until}
	c, err := p.CountPersonalData(context.Background(), "u")
	if err != nil || c.Rows != 3 || len(c.Collections) != 2 {
		t.Fatalf("CountPersonalData = %+v, %v", c, err)
	}
	r, err := p.RetainUntil(context.Background(), "u")
	if err != nil || r == nil || !r.Until.Equal(until) || r.Reason != "fiscal_records" {
		t.Fatalf("RetainUntil = %+v, %v", r, err)
	}
}

func TestPersonalDataCountJSONOmitsEmptyCollections(t *testing.T) {
	b, _ := json.Marshal(PersonalDataCount{Rows: 0})
	if strings.Contains(string(b), "collections") {
		t.Fatalf("got %s; collections must be omitted when empty", b)
	}
	if !strings.Contains(string(b), `"rows":0`) {
		t.Fatalf("got %s; rows must always be present", b)
	}
}

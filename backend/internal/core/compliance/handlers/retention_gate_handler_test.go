package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/internal/core/compliance/services/erasuretest"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// retainingProducer reports a retention obligation (or a reporter failure).
type retainingProducer struct {
	fakeProducer
	until time.Time
	err   error
}

func (p *retainingProducer) RetainUntil(context.Context, string) (*iface.RetentionObligation, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &iface.RetentionObligation{Until: p.until, Reason: "fiscal_records"}, nil
}

func gatedDSR(p iface.PIIProducer) *services.DSRService {
	reg := iface.NewPIIProducerRegistry()
	reg.Register(p)
	dsr := services.NewDSRService(reg, nil, slog.Default())
	dsr.SetRetentionGate(services.NewRetentionGate(nil, reg, func() int { return 0 }))
	return dsr
}

func assertCoded(t *testing.T, err error, status int, code string) *errcode.Error {
	t.Helper()
	assertStatus(t, err, status)
	var ce *errcode.Error
	if !errors.As(err, &ce) || ce.Code != code {
		t.Fatalf("err = %#v; want code %s", err, code)
	}
	return ce
}

func TestExecuteHardDeleteInsideRetentionIs409WithDate(t *testing.T) {
	t.Parallel()
	until := time.Now().UTC().AddDate(3, 0, 0).Truncate(time.Second)
	repo := seeded(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending})
	h := newErasureHandler(repo, gatedDSR(&retainingProducer{fakeProducer: fakeProducer{subject: "billing"}, until: until}))

	_, err := h.Execute(authedCtx("admin-1"), &ExecuteErasureRequestInput{ID: "r-1"})
	ce := assertCoded(t, err, 409, errcode.ComplianceRetentionMinimumNotMet)
	body, _ := json.Marshal(ce)
	var m struct {
		RetainUntil string   `json:"retainUntil"`
		Reasons     []string `json:"reasons"`
	}
	_ = json.Unmarshal(body, &m)
	if m.RetainUntil != until.Format(time.RFC3339) || len(m.Reasons) != 1 || m.Reasons[0] != "fiscal_records" {
		t.Fatalf("body = %s", body)
	}
	if repo.Row("r-1").Status != models.ErasureRequestPending {
		t.Fatal("request must stay pending")
	}

	in := &ExecuteErasureRequestInput{ID: "r-1"}
	in.Body.Mode = "anonymize"
	if _, err := h.Execute(authedCtx("admin-1"), in); err != nil {
		t.Fatalf("anonymize must run: %v", err)
	}
}

func TestExecuteReporterFailureIs503(t *testing.T) {
	t.Parallel()
	repo := seeded(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending})
	h := newErasureHandler(repo, gatedDSR(&retainingProducer{fakeProducer: fakeProducer{subject: "billing"}, err: errors.New("down")}))
	_, err := h.Execute(authedCtx("admin-1"), &ExecuteErasureRequestInput{ID: "r-1"})
	assertCoded(t, err, 503, errcode.ComplianceRetentionCheckUnavailable)
}

func TestExecuteLegalHoldCarriesItsCode(t *testing.T) {
	t.Parallel()
	repo := seeded(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending})
	dsr := newDSR(&fakeProducer{subject: "user"})
	dsr.SetLegalHoldChecker(&fakeHoldChecker{held: true})
	_, err := newErasureHandler(repo, dsr).Execute(authedCtx("admin-1"), &ExecuteErasureRequestInput{ID: "r-1"})
	assertCoded(t, err, 409, errcode.ComplianceLegalHoldActive)
}

func TestMeEraseInsideRetentionIs409AndRecordsNothing(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	until := time.Now().UTC().AddDate(1, 0, 0)
	h := newMeWithRepo(gatedDSR(&retainingProducer{fakeProducer: fakeProducer{subject: "billing"}, until: until}), repo)
	_, err := h.Erase(authedCtx("u-1"), &struct{}{})
	assertCoded(t, err, 409, errcode.ComplianceRetentionMinimumNotMet)
	if rows, _ := repo.ListByStatus(context.Background()); len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestMeEraseLegalHoldCarriesItsCode(t *testing.T) {
	t.Parallel()
	dsr := newDSR(&fakeProducer{subject: "user"})
	dsr.SetLegalHoldChecker(&fakeHoldChecker{held: true})
	_, err := newMe(dsr).Erase(authedCtx("u-1"), &struct{}{})
	assertCoded(t, err, 409, errcode.ComplianceLegalHoldActive)
}

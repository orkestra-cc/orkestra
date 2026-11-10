package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services/erasuretest"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func approvalSvc(n int, producers ...iface.PIIProducer) (*ErasureRequestService, *DSRService, *erasuretest.MemRepo) {
	repo := erasuretest.NewMemRepo()
	svc, dsr := newErasureSvc(repo, producers...)
	svc.SetRequiredApprovals(func(context.Context) int { return n })
	return svc, dsr, repo
}

func approvers(r models.ErasureRequest) []string {
	var out []string
	for _, a := range r.Approvals {
		out = append(out, a.By)
	}
	return out
}

func TestDefaultOneApprovalKeepsExecuteAlone(t *testing.T) {
	t.Parallel()
	user := &stubProducer{subject: "user"}
	svc, _, repo := approvalSvc(1, user)
	req, err := svc.Lodge(context.Background(), "u-1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if req.RequiredApprovals != 1 {
		t.Fatalf("RequiredApprovals = %d; want 1", req.RequiredApprovals)
	}
	if _, err := svc.Execute(context.Background(), req.UUID, iface.EraseHardDelete, "op-a"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := approvers(repo.Row(req.UUID)); len(got) != 1 || got[0] != "op-a" {
		t.Fatalf("approvals = %v; want [op-a]", got)
	}
}

func TestRequiredApprovalsAreClamped(t *testing.T) {
	t.Parallel()
	for in, want := range map[int]int{-3: 1, 0: 1, 3: 3, 9: 5} {
		svc, _, _ := approvalSvc(in)
		req, err := svc.Lodge(context.Background(), "u-1", "", "")
		if err != nil || req.RequiredApprovals != want {
			t.Errorf("config %d: RequiredApprovals = %d, %v; want %d", in, req.RequiredApprovals, err, want)
		}
	}
}

func TestExecuteWithTwoRequiredNeedsAnotherOperator(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	user := &stubProducer{subject: "user"}
	svc, _, repo := approvalSvc(2, user)
	req, _ := svc.Lodge(ctx, "u-1", "", "")

	_, err := svc.Execute(ctx, req.UUID, iface.EraseHardDelete, "op-a")
	var miss *ErasureApprovalsMissingError
	if !errors.Is(err, ErrErasureApprovalsMissing) || !errors.As(err, &miss) || miss.Have != 1 || miss.Need != 2 {
		t.Fatalf("err = %v; want approvals missing {1,2}", err)
	}
	if row := repo.Row(req.UUID); row.Status != models.ErasureRequestPending || len(row.Approvals) != 0 || row.Mode != "" || user.purgeCalls != 0 {
		t.Fatalf("row must be untouched: %+v purge=%d", row, user.purgeCalls)
	}

	if _, err := svc.Approve(ctx, req.UUID, iface.EraseHardDelete, "op-a", "reviewed"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, err := svc.Execute(ctx, req.UUID, iface.EraseHardDelete, "op-a"); !errors.Is(err, ErrErasureApprovalsMissing) {
		t.Fatalf("same operator approve then execute err = %v; want approvals missing", err)
	}
	out, err := svc.Execute(ctx, req.UUID, iface.EraseHardDelete, "op-b")
	if err != nil {
		t.Fatalf("Execute by B: %v", err)
	}
	if got := approvers(repo.Row(req.UUID)); len(got) != 2 || got[0] != "op-a" || got[1] != "op-b" {
		t.Fatalf("approvals = %v; want [op-a op-b]", got)
	}
	if repo.Row(req.UUID).Approvals[1].Note != "executed" || out.Request.Status != models.ErasureRequestCompleted {
		t.Fatalf("executor approval note / status wrong: %+v", repo.Row(req.UUID))
	}
}

func TestMissingRequiredApprovalsCountsAsOne(t *testing.T) {
	t.Parallel()
	svc, _, repo := approvalSvc(3, &stubProducer{subject: "user"})
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending})
	if _, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "op-a"); err != nil {
		t.Fatalf("legacy row without requiredApprovals must run with one: %v", err)
	}
}

func TestSubjectCannotApproveOrExecute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, repo := approvalSvc(1, &stubProducer{subject: "user"})
	req, _ := svc.Lodge(ctx, "u-1", "", "")
	if _, err := svc.Approve(ctx, req.UUID, iface.EraseHardDelete, "u-1", "mine"); !errors.Is(err, ErrErasureSelfApproval) {
		t.Fatalf("Approve by subject err = %v", err)
	}
	if _, err := svc.Execute(ctx, req.UUID, iface.EraseHardDelete, "u-1"); !errors.Is(err, ErrErasureSelfApproval) {
		t.Fatalf("Execute by subject err = %v", err)
	}
	if row := repo.Row(req.UUID); row.Status != models.ErasureRequestPending || len(row.Approvals) != 0 {
		t.Fatalf("row touched: %+v", row)
	}
}

func TestApproveValidations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, repo := approvalSvc(2, &stubProducer{subject: "user"})
	req, _ := svc.Lodge(ctx, "u-1", "", "")

	if _, err := svc.Approve(ctx, req.UUID, iface.EraseAnonymize, "op-a", "  "); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("blank note err = %v; want ErrReasonRequired", err)
	}
	got, err := svc.Approve(ctx, req.UUID, iface.EraseAnonymize, "op-a", "mail a@b.it ok")
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "anonymize" || len(got.Approvals) != 1 || got.Approvals[0].Note == "mail a@b.it ok" {
		t.Fatalf("row = %+v; want locked anonymize and a masked note", got)
	}
	if _, err := svc.Approve(ctx, req.UUID, iface.EraseAnonymize, "op-a", "again"); !errors.Is(err, ErrErasureAlreadyApproved) {
		t.Fatalf("duplicate err = %v; want ErrErasureAlreadyApproved", err)
	}
	if _, err := svc.Approve(ctx, req.UUID, iface.EraseHardDelete, "op-b", "other mode"); !errors.Is(err, ErrErasureModeMismatch) {
		t.Fatalf("mode err = %v; want ErrErasureModeMismatch", err)
	}
	if _, err := svc.Execute(ctx, req.UUID, iface.EraseHardDelete, "op-b"); !errors.Is(err, ErrErasureModeMismatch) {
		t.Fatalf("Execute with other mode err = %v; want ErrErasureModeMismatch", err)
	}
	if _, err := svc.Approve(ctx, "nope", iface.EraseAnonymize, "op-b", "x"); !errors.Is(err, repository.ErrErasureRequestNotFound) {
		t.Fatalf("missing err = %v", err)
	}
	repo.Put(models.ErasureRequest{UUID: "r-inc", UserUUID: "u-2", Status: models.ErasureRequestIncomplete})
	if _, err := svc.Approve(ctx, "r-inc", iface.EraseAnonymize, "op-b", "x"); !errors.Is(err, repository.ErrErasureRequestNotFound) {
		t.Fatalf("non-pending err = %v; want not found", err)
	}
}

func TestOpenCreatesAdminRequestWithRequesterApproval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, repo := approvalSvc(2, &stubProducer{subject: "user"})
	req, err := svc.Open(ctx, OpenInput{UserUUID: "u-1", Reason: "contract ended, contact a@b.it", RequestedBy: "op-a", Mode: iface.EraseAnonymize})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	row := repo.Row(req.UUID)
	if row.Status != models.ErasureRequestPending || row.Source != models.ErasureSourceAdmin || row.RequestedBy != "op-a" ||
		row.Mode != "anonymize" || row.RequiredApprovals != 2 || len(row.Approvals) != 1 || row.Approvals[0].By != "op-a" {
		t.Fatalf("row = %+v", row)
	}
	if row.Reason == "contract ended, contact a@b.it" || row.Approvals[0].Note != row.Reason {
		t.Fatalf("reason must be masked and be the approval note: %+v", row)
	}

	if _, err := svc.Execute(ctx, req.UUID, iface.EraseAnonymize, "op-a"); !errors.Is(err, ErrErasureApprovalsMissing) {
		t.Fatalf("opener executing alone err = %v; want approvals missing", err)
	}
	if _, err := svc.Execute(ctx, req.UUID, iface.EraseAnonymize, "op-b"); err != nil {
		t.Fatalf("Execute by B: %v", err)
	}
}

func TestOpenRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsr, _ := approvalSvc(1, &stubProducer{subject: "user"})
	in := OpenInput{UserUUID: "u-1", Reason: "why", RequestedBy: "op-a", Mode: iface.EraseHardDelete}

	bad := in
	bad.Reason = " "
	if _, err := svc.Open(ctx, bad); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("blank reason err = %v", err)
	}
	self := in
	self.RequestedBy = "u-1"
	if _, err := svc.Open(ctx, self); !errors.Is(err, ErrErasureSelfApproval) {
		t.Fatalf("self open err = %v", err)
	}

	first, err := svc.Open(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Open(ctx, in)
	var ex *ErasureRequestExistsError
	if !errors.Is(err, ErrErasureRequestExists) || !errors.As(err, &ex) || ex.UUID != first.UUID {
		t.Fatalf("second open err = %v; want exists carrying %s", err, first.UUID)
	}
	if _, err := svc.Lodge(ctx, "u-1", "", ""); err != nil {
		t.Fatalf("Lodge is not gated by Open: %v", err)
	}

	dsr.SetLegalHoldChecker(&fakeHoldChecker{held: true})
	held := in
	held.UserUUID = "u-2"
	if _, err := svc.Open(ctx, held); !errors.Is(err, ErrLegalHoldActive) {
		t.Fatalf("held open err = %v", err)
	}
}

func TestOpenHardDeleteChecksRetentionAnonymizeDoesNot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsr, repo := approvalSvc(1, &stubProducer{subject: "user"})
	twoYearsAgo := erasureT0.AddDate(-2, 0, 0)
	dsr.SetRetentionGate(gateAt(5, &twoYearsAgo))

	_, err := svc.Open(ctx, OpenInput{UserUUID: "u-1", Reason: "r", RequestedBy: "op-a", Mode: iface.EraseHardDelete})
	if !errors.Is(err, ErrRetentionMinimum) {
		t.Fatalf("hard delete open err = %v; want retention minimum", err)
	}
	if rows, _ := repo.ListByStatus(ctx); len(rows) != 0 {
		t.Fatalf("a refused open must record nothing: %+v", rows)
	}
	if _, err := svc.Open(ctx, OpenInput{UserUUID: "u-1", Reason: "r", RequestedBy: "op-a", Mode: iface.EraseAnonymize}); err != nil {
		t.Fatalf("anonymize open: %v", err)
	}
}

func TestIncompleteRetryIgnoresApprovals(t *testing.T) {
	t.Parallel()
	svc, _, repo := approvalSvc(2, &stubProducer{subject: "user"})
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, Mode: "hard_delete", RequiredApprovals: 2,
		Progress: []models.ErasureProducerProgress{{Key: "user", Subject: "user", Outcome: models.ProducerOutcomePending}}})
	if _, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "op-a"); err != nil {
		t.Fatalf("retry must skip approvals: %v", err)
	}
}

func TestConcurrentApprovalsLandOncePerOperator(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, repo := approvalSvc(3, &stubProducer{subject: "user"})
	req, _ := svc.Lodge(ctx, "u-1", "", "")
	var wg sync.WaitGroup
	for _, by := range []string{"op-a", "op-b", "op-a", "op-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.Approve(ctx, req.UUID, iface.EraseHardDelete, by, "ok")
		}()
	}
	wg.Wait()
	if got := approvers(repo.Row(req.UUID)); len(got) != 2 {
		t.Fatalf("approvals = %v; want exactly one per operator", got)
	}
}

func TestConcurrentExecuteRunsOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	user := &stubProducer{subject: "user"}
	svc, _, repo := approvalSvc(2, user)
	req, _ := svc.Lodge(ctx, "u-1", "", "")
	if _, err := svc.Approve(ctx, req.UUID, iface.EraseHardDelete, "op-a", "ok"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	for _, by := range []string{"op-b", "op-c"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.Execute(ctx, req.UUID, iface.EraseHardDelete, by)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
		}
	}
	if ok != 1 || user.purgeCalls != 1 {
		t.Fatalf("successful executes = %d, purge calls = %d; want exactly 1", ok, user.purgeCalls)
	}
	if got := approvers(repo.Row(req.UUID)); len(got) != 2 {
		t.Fatalf("approvals = %v; want [op-a + the winner]", got)
	}
}

// racingRepo simulates the interleavings the fake cannot produce on its own.
type racingRepo struct {
	*erasuretest.MemRepo
	approveBeforeClaim bool // the claimant's own approval lands just before its Claim
	hideOpen           bool // the first HasOpen misses (another Open is in flight)
}

func (r *racingRepo) Claim(ctx context.Context, uuid, from, mode, actor string, now, lease time.Time, a *models.ErasureApproval) (*models.ErasureRequest, error) {
	if r.approveBeforeClaim && a != nil {
		r.approveBeforeClaim = false
		_, _ = r.MemRepo.AddApproval(ctx, uuid, mode, models.ErasureApproval{By: actor, At: now, Note: "own"})
	}
	return r.MemRepo.Claim(ctx, uuid, from, mode, actor, now, lease, a)
}

func (r *racingRepo) HasOpen(ctx context.Context, u string) (string, bool, error) {
	if r.hideOpen {
		r.hideOpen = false
		return "", false, nil
	}
	return r.MemRepo.HasOpen(ctx, u)
}

func racingSvc(repo *racingRepo, n int, sink iface.AuditSink, producers ...iface.PIIProducer) *ErasureRequestService {
	dsr, _ := newDSRService(producers...)
	svc := NewErasureRequestService(repo, dsr, sink, nil)
	svc.now = func() time.Time { return erasureT0 }
	svc.SetRequiredApprovals(func(context.Context) int { return n })
	return svc
}

func TestExecuteRetriesClaimWhenOwnApprovalLandedMeanwhile(t *testing.T) {
	t.Parallel()
	repo := &racingRepo{MemRepo: erasuretest.NewMemRepo(), approveBeforeClaim: true}
	user := &stubProducer{subject: "user"}
	svc := racingSvc(repo, 2, nil, user)
	ctx := context.Background()
	req, _ := svc.Lodge(ctx, "u-1", "", "")
	if _, err := svc.Approve(ctx, req.UUID, iface.EraseHardDelete, "op-a", "ok"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Execute(ctx, req.UUID, iface.EraseHardDelete, "op-b"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := approvers(repo.Row(req.UUID)); len(got) != 2 || user.purgeCalls != 1 {
		t.Fatalf("approvals = %v, purge = %d; want [op-a op-b] and one run", got, user.purgeCalls)
	}
}

func TestOpenRaceLoserGetsTheWinnersID(t *testing.T) {
	t.Parallel()
	repo := &racingRepo{MemRepo: erasuretest.NewMemRepo(), hideOpen: true}
	svc := racingSvc(repo, 1, nil)
	ctx := context.Background()
	in := OpenInput{UserUUID: "u-1", Reason: "why", RequestedBy: "op-a", Mode: iface.EraseAnonymize}
	first, err := svc.Open(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	repo.hideOpen = true // the second Open's pre-check misses; the unique index decides
	_, err = svc.Open(ctx, in)
	var ex *ErasureRequestExistsError
	if !errors.As(err, &ex) || ex.UUID != first.UUID {
		t.Fatalf("err = %v; want exists carrying %s", err, first.UUID)
	}
}

func TestExecutePendingRunsInTheLockedModeAndGatesOnIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsr, repo := approvalSvc(1, &stubProducer{subject: "user"})
	twoYearsAgo := erasureT0.AddDate(-2, 0, 0)
	dsr.SetRetentionGate(gateAt(5, &twoYearsAgo))
	req, err := svc.Open(ctx, OpenInput{UserUUID: "u-1", Reason: "r", RequestedBy: "op-a", Mode: iface.EraseAnonymize})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteInMode(ctx, req.UUID, "hard_delete", "op-b"); !errors.Is(err, ErrErasureModeMismatch) {
		t.Fatalf("mismatch err = %v; want mode mismatch, not the retention gate", err)
	}
	if _, err := svc.ExecuteInMode(ctx, req.UUID, "", "op-b"); err != nil {
		t.Fatalf("no mode must run the locked anonymize: %v", err)
	}
	if got := repo.Row(req.UUID); got.Mode != "anonymize" {
		t.Fatalf("mode = %q", got.Mode)
	}
	// A legacy row without a locked mode defaults to hard delete (and is gated).
	repo.Put(models.ErasureRequest{UUID: "r-legacy", UserUUID: "u-2", Status: models.ErasureRequestPending})
	if _, err := svc.ExecuteInMode(ctx, "r-legacy", "", "op-b"); !errors.Is(err, ErrRetentionMinimum) {
		t.Fatalf("legacy default err = %v; want retention minimum", err)
	}
}

func TestOpenAuditsWithoutPersonalData(t *testing.T) {
	t.Parallel()
	sink := &capturingAuditSink{}
	repo := &racingRepo{MemRepo: erasuretest.NewMemRepo()}
	svc := racingSvc(repo, 2, sink)
	req, err := svc.Open(context.Background(), OpenInput{UserUUID: "u-1", Reason: "mail a@b.it", RequestedBy: "op-a", Mode: iface.EraseAnonymize})
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %+v", sink.events)
	}
	e := sink.events[0]
	if e.Action != "gdpr.dsr.erasure_opened" || e.ActorUserID != "op-a" || e.ResourceID != req.UUID ||
		e.Metadata["subjectUserUuid"] != "u-1" || e.Metadata["mode"] != "anonymize" || e.Metadata["requiredApprovals"] != 2 || len(e.Metadata) != 3 {
		t.Fatalf("event = %+v", e)
	}
}

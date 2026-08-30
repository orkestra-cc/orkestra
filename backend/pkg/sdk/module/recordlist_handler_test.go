package module

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func TestRecordListErrorsMapToStatusCodes(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{ErrRevisionRequired, http.StatusUnprocessableEntity},
		{ErrDuplicateMutationField, http.StatusUnprocessableEntity},
		{ErrCreateRemoveOverlap, http.StatusUnprocessableEntity},
		{ErrRosterFull, http.StatusUnprocessableEntity},
		{ErrRevisionStale, http.StatusConflict},
		{ErrSlugExists, http.StatusConflict},
		{ErrSlugMissing, http.StatusConflict},
		// A value aimed at an element that is not there is the same class of
		// mistake as a stale removal: the client's view no longer holds.
		{ErrUnknownSlug, http.StatusConflict},
		// Label rules and the creation binding are value validation, not races.
		{ErrLabelRequired, http.StatusUnprocessableEntity},
		{ErrLabelTooLong, http.StatusUnprocessableEntity},
		{ErrSlugLabelMismatch, http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		if got := recordListStatus(c.err); got != c.want {
			t.Errorf("recordListStatus(%v) = %d, want %d", c.err, got, c.want)
		}
	}
	if got := recordListStatus(errors.New("something else")); got != 0 {
		t.Errorf("an unrelated error must not claim a record-list status, got %d", got)
	}
	if got := recordListStatus(nil); got != 0 {
		t.Errorf("nil must not claim a record-list status, got %d", got)
	}
}

// The bare module PATCH must not declare recordLists. The refusal comes from
// Huma's strict object schemas, not from handler code — a library default,
// which is exactly why it needs a test.
func TestBareModulePatchDoesNotDeclareRecordLists(t *testing.T) {
	var in UpdateModuleInput
	tp := reflect.TypeOf(in.Body)
	for i := 0; i < tp.NumField(); i++ {
		if strings.EqualFold(tp.Field(i).Name, "RecordLists") {
			t.Fatal("the bare module PATCH declares recordLists; record lists belong to the environment route")
		}
	}
}

// The environment PATCH is where they belong, and the revision it carries is a
// pointer so an omitted revision is distinguishable from an explicit zero.
func TestEnvironmentPatchCarriesRecordListsAndAPointerRevision(t *testing.T) {
	var in UpdateEnvironmentInput
	tp := reflect.TypeOf(in.Body)
	rl, ok := tp.FieldByName("RecordLists")
	if !ok {
		t.Fatal("the environment PATCH does not declare recordLists")
	}
	if rl.Type.Kind() != reflect.Slice {
		t.Fatalf("recordLists is %s, want a slice", rl.Type.Kind())
	}
	revField, ok := tp.FieldByName("Revision")
	if !ok {
		t.Fatal("the environment PATCH does not declare revision")
	}
	if revField.Type.Kind() != reflect.Pointer {
		t.Fatalf("revision is %s, want a pointer — an omitted revision must not read as an explicit 0", revField.Type.Kind())
	}
}

// An orphan element key is the client acting on a roster that no longer
// holds — the same class as a stale removal, so 409, not the blanket 500
// the bare PATCH's fallback used to produce.
func TestUpdateModule_OrphanElementSecretIsA409(t *testing.T) {
	h, repo, _ := newAuditHandler(t, &auditDemoModule{})
	in := patchConfig(nil, map[string]string{"email.profiles.ghost.password": "x"})
	_, err := h.UpdateModule(context.Background(), in)
	if err == nil {
		t.Fatal("expected the orphan secret to be refused")
	}
	var se huma.StatusError
	if !errors.As(err, &se) || se.GetStatus() != http.StatusConflict {
		t.Fatalf("err = %v (%T), want a 409 status error", err, err)
	}
	if repo.docCasCalls != 0 {
		t.Errorf("docCasCalls = %d, want 0", repo.docCasCalls)
	}
}

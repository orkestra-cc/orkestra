package module

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
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

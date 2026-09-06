package module

import (
	"fmt"
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
)

// An IndexKeySpecsConflict means the database's index no longer matches the
// declared spec. That is a drift signal an operator must act on, not routine
// noise — ensureCollection must classify it distinctly from a benign
// already-exists.
func TestIsIndexSpecConflict(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"conflict", errIndexKeySpecsConflict(), true},
		{"nil", nil, false},
		{"unrelated", errUnrelated(), false},
		// ensureCollection wraps the driver error with fmt.Errorf("...: %w", err)
		// before it ever reaches a caller (see ensureCollection's "create
		// indexes %q" wrap). isIndexSpecConflict must still see through that
		// via errors.As, or the real production path — which only ever sees
		// the wrapped form — would misclassify every conflict as routine.
		{"wrapped", fmt.Errorf("create indexes %q: %w", "crm_cards", errIndexKeySpecsConflict()), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isIndexSpecConflict(tc.err); got != tc.want {
				t.Errorf("isIndexSpecConflict(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func errIndexKeySpecsConflict() error {
	return mongo.CommandError{Code: 86, Message: "An existing index has the same name as the requested index"}
}

func errUnrelated() error {
	return mongo.CommandError{Code: 11000, Message: "duplicate key"}
}

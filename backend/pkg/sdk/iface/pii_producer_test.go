package iface

import (
	"encoding/json"
	"strings"
	"testing"
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

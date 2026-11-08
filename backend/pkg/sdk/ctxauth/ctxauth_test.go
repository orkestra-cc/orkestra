package ctxauth

import (
	"context"
	"testing"
)

func TestGetSessionID(t *testing.T) {
	if _, ok := GetSessionID(context.Background()); ok {
		t.Fatal("empty context must report ok=false")
	}
	ctx := context.WithValue(context.Background(), KeySessionID, "sid-1")
	if got, ok := GetSessionID(ctx); !ok || got != "sid-1" {
		t.Fatalf("GetSessionID = %q,%v", got, ok)
	}
	if KeySessionID != "sessionID" {
		t.Fatalf("KeySessionID = %q", KeySessionID)
	}
}

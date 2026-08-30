package main

import "testing"

func TestRequiredPersistedModules_AuthIsRequired(t *testing.T) {
	found := false
	for _, n := range requiredPersistedModules {
		if n == "auth" {
			found = true
		}
	}
	if !found {
		t.Fatal("auth must be a required persisted config: its strict password-policy reader depends on it")
	}
}

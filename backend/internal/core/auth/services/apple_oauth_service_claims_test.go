package services

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// Apple documents email_verified as "String or Boolean": the ID token carries
// a JSON bool on some flows and the string "true"/"false" on others. Since
// §4.4 refuses an unlinked identity whose bit is false, reading the string
// shape as false would lock every Apple signup and link out of the platform —
// so both shapes are accepted and anything else is false.
func TestGetBoolOrStringClaimFromMap(t *testing.T) {
	for name, tc := range map[string]struct {
		claims jwt.MapClaims
		want   bool
	}{
		"bool true":       {jwt.MapClaims{"email_verified": true}, true},
		"bool false":      {jwt.MapClaims{"email_verified": false}, false},
		"string true":     {jwt.MapClaims{"email_verified": "true"}, true},
		"string TRUE":     {jwt.MapClaims{"email_verified": "TRUE"}, true},
		"string padded":   {jwt.MapClaims{"email_verified": " true "}, true},
		"string false":    {jwt.MapClaims{"email_verified": "false"}, false},
		"string yes":      {jwt.MapClaims{"email_verified": "yes"}, false},
		"number 1 (json)": {jwt.MapClaims{"email_verified": float64(1)}, false},
		"int 1":           {jwt.MapClaims{"email_verified": 1}, false},
		"missing":         {jwt.MapClaims{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := getBoolOrStringClaimFromMap(tc.claims, "email_verified"); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

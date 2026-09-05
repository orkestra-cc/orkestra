package iface

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// User.MFAEpoch's whole safety property (edge case 12: a pre-deploy
// document has no mfaEpoch key, so the deploy must not downgrade anyone)
// lives entirely in the bson tag. A Go zero value cannot tell "the field
// was never set in this test" apart from "the tag is wrong and the real
// document round trip would break" — only marshaling and unmarshaling a
// real BSON document can catch a mistyped tag.
func TestUserMFAEpoch_BSONRoundTrip(t *testing.T) {
	t.Run("a document with no mfaEpoch key decodes to zero", func(t *testing.T) {
		raw, err := bson.Marshal(bson.M{"uuid": "u-legacy"})
		if err != nil {
			t.Fatalf("bson.Marshal: %v", err)
		}
		var u User
		if err := bson.Unmarshal(raw, &u); err != nil {
			t.Fatalf("bson.Unmarshal: %v", err)
		}
		if u.MFAEpoch != 0 {
			t.Fatalf("MFAEpoch = %d, want 0", u.MFAEpoch)
		}
	})

	t.Run("a zero MFAEpoch is omitted from the marshaled document", func(t *testing.T) {
		raw, err := bson.Marshal(User{UUID: "u-new"})
		if err != nil {
			t.Fatalf("bson.Marshal: %v", err)
		}
		var doc bson.M
		if err := bson.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("bson.Unmarshal: %v", err)
		}
		if _, present := doc["mfaEpoch"]; present {
			t.Fatalf("mfaEpoch key present in the marshaled zero-value document, want omitted (omitempty)")
		}
	})
}

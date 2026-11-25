package installation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// profileIdentity is the value hashed into a profile ID. Field order and tags
// are part of the contract: changing either changes every ID.
type profileIdentity struct {
	Kind           iface.InferenceKind `json:"kind"`
	ModelID        string              `json:"modelId"`
	Revision       string              `json:"revision"`
	Pooling        string              `json:"pooling"`
	Normalize      bool                `json:"normalize"`
	Truncate       bool                `json:"truncate"`
	QueryPrefix    string              `json:"queryPrefix"`
	DocumentPrefix string              `json:"documentPrefix"`
}

// ProfileID returns "ip_" + hex(sha256(compact JSON of the identity fields)).
// The endpoint name, URL, boundary and measured fields are not part of it.
func ProfileID(p iface.InferenceProfile) string {
	b, err := json.Marshal(profileIdentity{
		Kind:           p.Kind,
		ModelID:        p.ModelID,
		Revision:       p.Revision,
		Pooling:        p.Pooling,
		Normalize:      p.Normalize,
		Truncate:       p.Truncate,
		QueryPrefix:    p.QueryPrefix,
		DocumentPrefix: p.DocumentPrefix,
	})
	if err != nil { // unreachable: only strings, bools
		return ""
	}
	sum := sha256.Sum256(b)
	return "ip_" + hex.EncodeToString(sum[:])
}

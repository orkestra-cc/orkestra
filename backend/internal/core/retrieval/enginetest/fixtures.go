package enginetest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
)

// fixtureCorpus is the corpus name every fixture point id is derived from.
const fixtureCorpus = "c"

const (
	tenantA = "tenant-a"
	tenantB = "tenant-b"
)

// everyone is the tenant-wide principal: the default visibility of fixture
// points and the default principals of fixture queries.
var everyone = []string{"tenant:*"}

// point builds a fixture point whose ID and Runtime are consistent with each
// other: the ID is engine.PointID over (tenant, "c", source, ordinal), the
// chunk id names the source and ordinal, and the content hash and character
// span describe text.
func point(tenant, source string, rev int64, ordinal int, text string, vec []float32,
	visibleTo, subjects []string, fields map[string]any,
) engine.Point {
	sum := sha256.Sum256([]byte(text))
	return engine.Point{
		ID:    engine.PointID(tenant, fixtureCorpus, source, ordinal),
		Dense: vec,
		Text:  text,
		Runtime: engine.Runtime{
			TenantID:         tenant,
			SourceID:         source,
			SourceRevision:   rev,
			ChunkID:          fmt.Sprintf("%s#%d", source, ordinal),
			Ordinal:          ordinal,
			VisibleTo:        visibleTo,
			Subjects:         subjects,
			ContentHash:      hex.EncodeToString(sum[:]),
			IngestionProfile: "conformance-v1",
			CharStart:        0,
			CharEnd:          len([]rune(text)),
		},
		Fields: fields,
	}
}

// simple is point with tenant-wide visibility, no subjects and no fields.
func simple(tenant, source string, rev int64, ordinal int, text string, vec []float32) engine.Point {
	return point(tenant, source, rev, ordinal, text, vec, everyone, nil, nil)
}

// vec builds a 4-dimensional vector; every fixture vector is non-zero.
func vec(a, b, c, d float32) []float32 { return []float32{a, b, c, d} }

// describe names a stored chunk the way failure messages print it:
// tenant/source#ordinal@revision "text".
func describe(tenant, source string, ordinal int, rev int64, text string) string {
	return fmt.Sprintf("%s/%s#%d@r%d %q", tenant, source, ordinal, rev, text)
}

func describePoint(p engine.Point) string {
	return describe(p.Runtime.TenantID, p.Runtime.SourceID, p.Runtime.Ordinal, p.Runtime.SourceRevision, p.Text)
}

func describeCandidate(c engine.Candidate) string {
	return describe(c.Runtime.TenantID, c.Runtime.SourceID, c.Runtime.Ordinal, c.Runtime.SourceRevision, c.Text)
}

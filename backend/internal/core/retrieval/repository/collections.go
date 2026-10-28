// Package repository names the retrieval module's MongoDB collections.
package repository

import "github.com/orkestra/backend/pkg/sdk/module"

// CollCorpusState is installation-wide (one document per corpus, _id = the
// corpus name). It is declared here so the module owns the name; its document
// shape and repository code arrive with the indexing worker (PR 4).
const CollCorpusState = "retrieval_corpus_state"

// CollOutbox is the per-key indexing outbox (tenant-scoped).
const CollOutbox = "retrieval_outbox"

// OutboxIndexes are the declared indexes of CollOutbox; the module and the
// repository tests share them. The last two are system sweeps (not led by
// tenantId) for the cross-tenant due-key scan.
func OutboxIndexes() []module.IndexSpec {
	key := func(fields ...string) []module.IndexKey {
		out := make([]module.IndexKey, 0, len(fields))
		for _, f := range fields {
			out = append(out, module.IndexKey{Field: f, Direction: 1})
		}
		return out
	}
	return []module.IndexSpec{
		{OrderedKeys: key("tenantId", "corpus", "sourceId"), Unique: true},
		{OrderedKeys: key("tenantId", "dirty", "nextAttemptAt")},
		{OrderedKeys: key("tenantId", "leaseUntil")},
		{OrderedKeys: key("dirty", "nextAttemptAt")},
		{OrderedKeys: key("leaseUntil")},
	}
}

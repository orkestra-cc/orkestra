// Package repository names the retrieval module's MongoDB collections.
package repository

// CollCorpusState is installation-wide (one document per corpus, _id = the
// corpus name). It is declared here so the module owns the name; its document
// shape and repository code arrive with the indexing worker (PR 4).
const CollCorpusState = "retrieval_corpus_state"

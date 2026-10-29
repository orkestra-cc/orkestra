package services

import "errors"

// ErrCorpusRegistered reports that an admin action needs the corpus to be
// unregistered, and it is still registered (or was re-registered meanwhile).
var ErrCorpusRegistered = errors.New("retrieval: corpus is still registered")

// ErrCorpusNotFound reports that the corpus has no retrieval state to act on.
var ErrCorpusNotFound = errors.New("retrieval: no state for this corpus")

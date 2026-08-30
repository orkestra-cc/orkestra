package main

// requiredPersistedModules are the modules whose module_configs document is
// REQUIRED once boot seeding has run: a missing document is an outage that
// fails closed (503) and shows as a `missing` row on /admin/modules, never a
// reason to rebuild it from schema defaults. auth is here because its
// per-surface credential policy (password login on/off) is read strictly —
// a lazy re-seed from an admin page read would silently re-enable password
// sign-in with the schema default. Recovery: restore the document, or fix
// Mongo and restart so normal boot seeding runs.
var requiredPersistedModules = []string{"auth"}

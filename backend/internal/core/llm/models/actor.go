package models

// ErasedActor replaces an actor id (createdBy / grantedBy) when the subject
// is erased under GDPR art. 17. It is a fixed constant, deliberately not
// derived from the user id, so it cannot be reversed or correlated.
const ErasedActor = "erased-subject"

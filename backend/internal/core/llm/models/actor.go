package models

// ErasedActor replaces an actor id (createdBy / grantedBy) when the subject
// is erased under GDPR art. 17. It is a fixed constant, deliberately not
// derived from the user id, so it cannot be reversed or correlated.
const ErasedActor = "erased-subject"

// ErasedUsagePrefix starts the random identifier that replaces userUuid
// on a usage row kept by an anonymizing erasure. Every row gets its own
// random suffix (never derived from the user), so the erased subject's
// rows cannot be linked to one another or back to them.
const ErasedUsagePrefix = "erased-"

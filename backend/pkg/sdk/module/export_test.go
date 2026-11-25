package module

import "go.mongodb.org/mongo-driver/mongo"

// EnsureCollectionsForTest runs the registry's boot-time collection and index
// provisioning against db, for external tests that declare indexes through a
// real module's Collections().
func (r *ModuleRegistry) EnsureCollectionsForTest(db *mongo.Database) error {
	return r.ensureCollections(db)
}

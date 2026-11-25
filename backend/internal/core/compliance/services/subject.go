package services

import (
	"context"
	"errors"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrSubjectNotFound is returned when a data subject UUID matches no operator
// or client user row (a soft-deleted tombstone still counts as existing).
var ErrSubjectNotFound = errors.New("compliance: the data subject does not exist")

// SubjectLookup reports whether a user row with the UUID exists in the
// operator or client identity collection, in any state.
type SubjectLookup func(ctx context.Context, userUUID string) (bool, error)

// NormalizeSubjectUUID lower-cases and trims a subject UUID, the form stored
// everywhere.
func NormalizeSubjectUUID(id string) string { return strings.ToLower(strings.TrimSpace(id)) }

// NewSubjectLookup looks the subject up in the tier-split identity
// collections, the lookup the retention gate uses.
func NewSubjectLookup(db *mongo.Database) SubjectLookup {
	return func(ctx context.Context, userUUID string) (bool, error) {
		for _, coll := range userTombstoneCollections {
			//tenantscope:allow subject existence is checked by UUID across tenants (platform DSR).
			err := db.Collection(coll).FindOne(ctx, bson.M{"uuid": userUUID},
				options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
			if err == nil {
				return true, nil
			}
			if !errors.Is(err, mongo.ErrNoDocuments) {
				return false, err
			}
		}
		return false, nil
	}
}

// SetSubjectLookup wires the existence check used by SubjectExists. Without
// one every subject is taken to exist.
func (s *DSRService) SetSubjectLookup(l SubjectLookup) { s.subjectLookup = l }

// SubjectExists reports whether the subject has a user row (any state).
func (s *DSRService) SubjectExists(ctx context.Context, userUUID string) (bool, error) {
	if s.subjectLookup == nil {
		return true, nil
	}
	return s.subjectLookup(ctx, userUUID)
}

// requireSubject returns ErrSubjectNotFound for an unknown subject.
func (s *DSRService) requireSubject(ctx context.Context, userUUID string) error {
	ok, err := s.SubjectExists(ctx, userUUID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSubjectNotFound
	}
	return nil
}

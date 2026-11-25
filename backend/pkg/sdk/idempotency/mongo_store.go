package idempotency

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type mongoStore struct{ coll *mongo.Collection }

// NewMongoStore returns a Store backed by coll, whose indexes come from
// CollectionSpec.
func NewMongoStore(coll *mongo.Collection) Store { return &mongoStore{coll: coll} }

// CollectionSpec declares the store collection: a unique {scope, key} index
// and a reaper on expiresAt (each record carries its own absolute deadline,
// set from Options.Retention, so retention is not baked into the index).
func CollectionSpec(name string) module.CollectionSpec {
	return module.CollectionSpec{
		Name: name,
		Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{{Field: "scope", Direction: 1}, {Field: "key", Direction: 1}}, Unique: true},
			{Keys: map[string]int{"expiresAt": 1}, ExpireAt: true},
		},
	}
}

func newOwner() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *mongoStore) Begin(ctx context.Context, scope, key, fingerprint string, now, lockedUntil, expiresAt time.Time) (string, *Record, error) {
	owner := newOwner()
	rec := Record{
		Scope: scope, Key: key, Fingerprint: fingerprint, State: StateInProgress, Owner: owner,
		LockedUntil: lockedUntil, ExpiresAt: expiresAt, CreatedAt: now,
	}
	_, err := s.coll.InsertOne(ctx, rec)
	if err == nil {
		return owner, nil, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return "", nil, err
	}
	// Take over an abandoned lock (crashed process), only for the same
	// request: a different fingerprint falls through to the existing record.
	res := s.coll.FindOneAndUpdate(ctx,
		bson.M{"scope": scope, "key": key, "state": StateInProgress, "fingerprint": fingerprint, "lockedUntil": bson.M{"$lt": now}},
		bson.M{"$set": bson.M{"owner": owner, "lockedUntil": lockedUntil, "expiresAt": expiresAt, "createdAt": now}})
	if res.Err() == nil {
		return owner, nil, nil
	}
	if !errors.Is(res.Err(), mongo.ErrNoDocuments) {
		return "", nil, res.Err()
	}
	var existing Record
	err = s.coll.FindOne(ctx, bson.M{"scope": scope, "key": key}).Decode(&existing)
	if errors.Is(err, mongo.ErrNoDocuments) {
		// Reaped between the calls: try once more.
		if _, ierr := s.coll.InsertOne(ctx, rec); ierr == nil {
			return owner, nil, nil
		} else if !mongo.IsDuplicateKeyError(ierr) {
			return "", nil, ierr
		}
		err = s.coll.FindOne(ctx, bson.M{"scope": scope, "key": key}).Decode(&existing)
	}
	if err != nil {
		return "", nil, err
	}
	return "", &existing, nil
}

func (s *mongoStore) Renew(ctx context.Context, scope, key, owner string, lockedUntil time.Time) (bool, error) {
	res, err := s.coll.UpdateOne(ctx, bson.M{"scope": scope, "key": key, "state": StateInProgress, "owner": owner},
		bson.M{"$set": bson.M{"lockedUntil": lockedUntil}})
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

func (s *mongoStore) Finish(ctx context.Context, scope, key, owner string, status int, contentType, location string, body []byte, bodyStored bool) (bool, error) {
	set := bson.M{"state": StateDone, "status": status, "contentType": contentType, "location": location, "bodyStored": bodyStored}
	if bodyStored && len(body) > 0 {
		set["body"] = body
	}
	res, err := s.coll.UpdateOne(ctx, bson.M{"scope": scope, "key": key, "state": StateInProgress, "owner": owner}, bson.M{"$set": set})
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

func (s *mongoStore) Abandon(ctx context.Context, scope, key, owner string) (bool, error) {
	res, err := s.coll.DeleteOne(ctx, bson.M{"scope": scope, "key": key, "state": StateInProgress, "owner": owner})
	if err != nil {
		return false, err
	}
	return res.DeletedCount == 1, nil
}

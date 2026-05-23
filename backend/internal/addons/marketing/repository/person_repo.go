package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/importers/match"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrPersonNotFound is returned by Person lookups when no document
// matches in the tenant scope.
var ErrPersonNotFound = errors.New("marketing: person not found")

// PersonRepository is the persistence boundary for marketing_persons.
type PersonRepository struct {
	coll *mongo.Collection
}

// NewPersonRepository binds a repository to the marketing_persons
// collection on db.
func NewPersonRepository(db *mongo.Database) *PersonRepository {
	return &PersonRepository{coll: db.Collection(models.PersonsCollection)}
}

// Create inserts a new Person, stamping tenantId / timestamps,
// normalising every email address to lowercase, and populating the
// soft-match denormalisation (firstNameLower, lastNameLower,
// phoneLast10). The caller is responsible for populating UUID.
//
// Identity-minimum (HasMinimumIdentity) is checked at the service
// layer, not here — importers occasionally stage incomplete rows for
// later enrichment.
func (r *PersonRepository) Create(ctx context.Context, p *models.Person) error {
	if p == nil {
		return errors.New("marketing: nil person")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	p.TenantID = tenantID
	normalizePersonEmails(p)
	denormalizePerson(p)
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	_, err = r.coll.InsertOne(ctx, p)
	return err
}

// GetByUUID returns the person with the given UUID in the caller's
// tenant scope, or ErrPersonNotFound when no document matches.
func (r *PersonRepository) GetByUUID(ctx context.Context, uuid string) (*models.Person, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var out models.Person
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrPersonNotFound
		}
		return nil, err
	}
	return &out, nil
}

// LookupByEmail returns the person in the caller's tenant carrying the
// given email address anywhere in their emails[] array (not just on
// the primary entry). The input is normalised before matching so the
// (tenantId, emails.address) unique index can serve the lookup.
func (r *PersonRepository) LookupByEmail(ctx context.Context, email string) (*models.Person, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, ErrPersonNotFound
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{"emails.address": email})
	if err != nil {
		return nil, err
	}
	var out models.Person
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrPersonNotFound
		}
		return nil, err
	}
	return &out, nil
}

// PersonListFilter parameterises the persons read surface. All fields
// optional.
//
// Phase 4 adds two card-aware filters layered on the existing
// marketing_persons.activeCardUuids denorm:
//
//   - HasActiveCard non-nil → presence/absence check on the array.
//     A true value matches persons with at least one issued non-revoked
//     card; false matches those with none.
//   - ActiveCardUUIDs non-empty → element-in-array check, matching
//     persons whose activeCardUuids contains any of the provided uuids.
//     The handler resolves the operator's `?activeCardOfType=<typeUuid>`
//     by translating it to a list of card uuids upstream.
type PersonListFilter struct {
	TagUUIDs        []string
	HasEmail        bool
	Source          string
	HasActiveCard   *bool
	ActiveCardUUIDs []string
	Limit           int64
	Skip            int64
}

// List returns persons matching filter, newest-first by updatedAt.
// Limit defaults to 50 (cap 500).
func (r *PersonRepository) List(ctx context.Context, f PersonListFilter) ([]models.Person, error) {
	base := bson.M{}
	if len(f.TagUUIDs) > 0 {
		base["tags"] = bson.M{"$in": f.TagUUIDs}
	}
	if f.HasEmail {
		base["emails.0"] = bson.M{"$exists": true}
	}
	if f.Source != "" {
		base["sources.importer"] = f.Source
	}
	if f.HasActiveCard != nil {
		if *f.HasActiveCard {
			base["activeCardUuids.0"] = bson.M{"$exists": true}
		} else {
			// Either the field is absent or the array is empty.
			base["$or"] = []bson.M{
				{"activeCardUuids": bson.M{"$exists": false}},
				{"activeCardUuids": bson.M{"$size": 0}},
			}
		}
	}
	if len(f.ActiveCardUUIDs) > 0 {
		base["activeCardUuids"] = bson.M{"$in": f.ActiveCardUUIDs}
	}
	filter, err := tenantrepo.Scope(ctx, base)
	if err != nil {
		return nil, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "updatedAt", Value: -1}}).
		SetLimit(limit).
		SetSkip(f.Skip)
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Person, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Update applies $set on the mutable fields of the person identified
// by UUID. Returns ErrPersonNotFound when no row matches.
//
// Callers are expected to use the higher-level service when the patch
// touches emails[] / tags[] / sources[] — the repository does not run
// the importer auto-merge logic here.
//
// Soft-match denormalisation: when the patch touches firstName /
// lastName / phones, the matching denorm field is recomputed in the
// same $set so the index stays consistent with the source-of-truth
// fields without needing a second round-trip.
func (r *PersonRepository) Update(ctx context.Context, uuid string, patch bson.M) error {
	if patch == nil {
		patch = bson.M{}
	}
	if v, ok := patch["firstName"].(string); ok {
		patch["firstNameLower"] = strings.ToLower(strings.TrimSpace(v))
	}
	if v, ok := patch["lastName"].(string); ok {
		patch["lastNameLower"] = strings.ToLower(strings.TrimSpace(v))
	}
	if phones, ok := patch["phones"].([]models.PhoneEntry); ok {
		patch["phoneLast10"] = computePhoneLast10(phones)
	}
	patch["updatedAt"] = time.Now().UTC()
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": patch})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrPersonNotFound
	}
	return nil
}

// FindSoftMatchByNameAndPhone is the candidate-fetch backing
// match.SoftMatchPerson. The pipeline's strict-miss path calls this
// after LookupByEmail returns ErrPersonNotFound; the index
// (tenantId, firstNameLower, lastNameLower) drives the first cut and
// the in-memory match.SoftMatchPerson confirms phone overlap.
//
// Returns ErrPersonNotFound when no candidate matches the names *and*
// shares at least one normalized phone. Empty inputs short-circuit to
// the "no soft-match" return because every check requires both names
// (per the SoftMatchPerson contract) and at least one phone.
func (r *PersonRepository) FindSoftMatchByNameAndPhone(ctx context.Context, firstName, lastName string, phones []models.PhoneEntry) (*models.Person, error) {
	first := strings.ToLower(strings.TrimSpace(firstName))
	last := strings.ToLower(strings.TrimSpace(lastName))
	if first == "" || last == "" {
		return nil, ErrPersonNotFound
	}
	last10s := computePhoneLast10(phones)
	if len(last10s) == 0 {
		return nil, ErrPersonNotFound
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"firstNameLower": first,
		"lastNameLower":  last,
		"phoneLast10":    bson.M{"$in": last10s},
	})
	if err != nil {
		return nil, err
	}
	var out models.Person
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrPersonNotFound
		}
		return nil, err
	}
	return &out, nil
}

// AppendSource pushes a new provenance entry onto the person's
// sources[] array.
func (r *PersonRepository) AppendSource(ctx context.Context, uuid string, src models.ProvenanceSource) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	_, err = r.coll.UpdateOne(ctx, filter, bson.M{
		"$push": bson.M{"sources": src},
		"$set":  bson.M{"updatedAt": time.Now().UTC()},
	})
	return err
}

// AddActiveCard appends cardUUID to marketing_persons.activeCardUuids
// via $addToSet (so a re-issue with the same UUID is idempotent). Used
// by CardService.Issue and CardService.Reinstate to keep the
// denormalized list in sync with marketing_cards.status changes.
func (r *PersonRepository) AddActiveCard(ctx context.Context, personUUID, cardUUID string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": personUUID})
	if err != nil {
		return err
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{
		"$addToSet": bson.M{"activeCardUuids": cardUUID},
		"$set":      bson.M{"updatedAt": time.Now().UTC()},
	})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrPersonNotFound
	}
	return nil
}

// RemoveActiveCard pulls cardUUID from activeCardUuids. Used by
// CardService.Revoke and CardService.Expire when a card transitions
// to the terminal revoked state. Suspended cards remain in the list
// (see IMPLEMENTATION_PLAN_PHASE_4.md §3.4 for the rationale).
func (r *PersonRepository) RemoveActiveCard(ctx context.Context, personUUID, cardUUID string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": personUUID})
	if err != nil {
		return err
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{
		"$pull": bson.M{"activeCardUuids": cardUUID},
		"$set":  bson.M{"updatedAt": time.Now().UTC()},
	})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrPersonNotFound
	}
	return nil
}

// Delete hard-deletes a person by UUID inside the caller's tenant.
// The membership cascade is the service's responsibility.
func (r *PersonRepository) Delete(ctx context.Context, uuid string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrPersonNotFound
	}
	return nil
}

func normalizePersonEmails(p *models.Person) {
	for i := range p.Emails {
		p.Emails[i].Address = NormalizeEmail(p.Emails[i].Address)
	}
}

// denormalizePerson populates the soft-match denorm fields from the
// source-of-truth scalars. Called on Create; Update folds the same
// derivations inline because patches may carry one field but not the
// rest.
func denormalizePerson(p *models.Person) {
	p.FirstNameLower = strings.ToLower(strings.TrimSpace(p.FirstName))
	p.LastNameLower = strings.ToLower(strings.TrimSpace(p.LastName))
	p.PhoneLast10 = computePhoneLast10(p.Phones)
}

// computePhoneLast10 produces the multikey index value for a Person's
// phones — one normalized last-10-digit entry per non-empty phone.
// Duplicates collapse so a person with two encodings of the same
// number doesn't multiply the index size.
func computePhoneLast10(phones []models.PhoneEntry) []string {
	if len(phones) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(phones))
	out := make([]string, 0, len(phones))
	for _, p := range phones {
		n := match.NormalizePhone(p.Number)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

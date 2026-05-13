package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	submodels "github.com/orkestra-cc/orkestra-addon-subscriptions/models"
	"github.com/orkestra/backend/cmd/migrations/0001_unify_clients/migrator"
	pmtmodels "github.com/orkestra/backend/internal/addons/payments/models"
	tenantrepo "github.com/orkestra/backend/internal/core/tenant/repository"
)

// sentinelCollection is where per-row completion records land. A separate
// collection (rather than reusing module_configs or similar) keeps the
// migration boundary explicit and lets us drop it once Phase 5 retires
// clientbilling entirely.
const sentinelCollection = "migrations_applied"

// clientbillingCustomersCollection is the legacy collection name. The
// clientbilling Go surface was deleted in Phase 5 of the Unified Client
// Aggregate refactor — this binary is preserved as a historical artifact
// (the migration runs once per environment), so we duplicate the collection
// name here rather than reaching back into a deleted package.
const clientbillingCustomersCollection = "clientbilling_customers"

type mongoStore struct {
	db *mongo.Database
}

func newMongoStore(db *mongo.Database) *mongoStore { return &mongoStore{db: db} }

func (s *mongoStore) SourceRows(ctx context.Context) ([]migrator.SourceRow, error) {
	cur, err := s.db.Collection(clientbillingCustomersCollection).
		Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var raw []struct {
		ID               primitive.ObjectID `bson:"_id"`
		UserUUID         string             `bson:"userUUID"`
		LegalName        string             `bson:"legalName,omitempty"`
		FirstName        string             `bson:"firstName,omitempty"`
		LastName         string             `bson:"lastName,omitempty"`
		Email            string             `bson:"email,omitempty"`
		VATNumber        string             `bson:"vatNumber,omitempty"`
		FiscalCode       string             `bson:"fiscalCode,omitempty"`
		Country          string             `bson:"country,omitempty"`
		AddressLine1     string             `bson:"addressLine1,omitempty"`
		AddressLine2     string             `bson:"addressLine2,omitempty"`
		City             string             `bson:"city,omitempty"`
		PostalCode       string             `bson:"postalCode,omitempty"`
		Province         string             `bson:"province,omitempty"`
		IsCompany        bool               `bson:"isCompany"`
		StripeCustomerID string             `bson:"stripeCustomerID,omitempty"`
	}
	if err := cur.All(ctx, &raw); err != nil {
		return nil, err
	}
	out := make([]migrator.SourceRow, 0, len(raw))
	for _, r := range raw {
		out = append(out, migrator.SourceRow{
			ID:               r.ID.Hex(),
			UserUUID:         r.UserUUID,
			LegalName:        r.LegalName,
			FirstName:        r.FirstName,
			LastName:         r.LastName,
			Email:            r.Email,
			VATNumber:        r.VATNumber,
			FiscalCode:       r.FiscalCode,
			Country:          r.Country,
			AddressLine1:     r.AddressLine1,
			AddressLine2:     r.AddressLine2,
			City:             r.City,
			PostalCode:       r.PostalCode,
			Province:         r.Province,
			IsCompany:        r.IsCompany,
			StripeCustomerID: r.StripeCustomerID,
		})
	}
	return out, nil
}

func (s *mongoStore) SentinelExists(ctx context.Context, sourceID string) (bool, error) {
	n, err := s.db.Collection(sentinelCollection).CountDocuments(ctx, bson.M{
		"migration": migrator.MigrationName,
		"sourceID":  sourceID,
	}, options.Count().SetLimit(1))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *mongoStore) FindPersonalTenant(ctx context.Context, userUUID string) (*migrator.TenantSnapshot, error) {
	var t struct {
		UUID             string `bson:"uuid"`
		LegalName        string `bson:"legalName,omitempty"`
		VATNumber        string `bson:"vatNumber,omitempty"`
		FiscalCode       string `bson:"fiscalCode,omitempty"`
		StripeCustomerID string `bson:"stripeCustomerID,omitempty"`
		PrimaryContact   struct {
			Email string `bson:"email,omitempty"`
		} `bson:"primaryContact,omitempty"`
		BillingAddress struct {
			Line1      string `bson:"line1,omitempty"`
			Line2      string `bson:"line2,omitempty"`
			City       string `bson:"city,omitempty"`
			Province   string `bson:"province,omitempty"`
			PostalCode string `bson:"postalCode,omitempty"`
			Country    string `bson:"country,omitempty"`
		} `bson:"billingAddress,omitempty"`
	}
	err := s.db.Collection(tenantrepo.CollTenants).FindOne(ctx, bson.M{
		"ownerUserUUID": userUUID,
		"kind":          "external",
		"signupChannel": "self_serve",
		"isCompany":     false,
		"deletedAt":     nil,
	}, options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: 1}})).Decode(&t)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &migrator.TenantSnapshot{
		UUID:             t.UUID,
		LegalName:        t.LegalName,
		VATNumber:        t.VATNumber,
		FiscalCode:       t.FiscalCode,
		Email:            t.PrimaryContact.Email,
		StripeCustomerID: t.StripeCustomerID,
		AddressLine1:     t.BillingAddress.Line1,
		AddressLine2:     t.BillingAddress.Line2,
		City:             t.BillingAddress.City,
		PostalCode:       t.BillingAddress.PostalCode,
		Province:         t.BillingAddress.Province,
		Country:          t.BillingAddress.Country,
	}, nil
}

func (s *mongoStore) CreatePersonalTenant(ctx context.Context, userUUID, name string) (*migrator.TenantSnapshot, error) {
	tenantUUID := migrator.MintTenantUUID()
	now := time.Now().UTC()
	doc := bson.M{
		"uuid":          tenantUUID,
		"kind":          "external",
		"status":        "active",
		"name":          name,
		"slug":          fmt.Sprintf("personal-%s-%s", shortID(userUUID), shortID(tenantUUID)),
		"ownerUserUUID": userUUID,
		"signupChannel": "self_serve",
		"isCompany":     false,
		"region":        "eu-west",
		"plan":          "free",
		"createdAt":     now,
		"updatedAt":     now,
	}
	if _, err := s.db.Collection(tenantrepo.CollTenants).InsertOne(ctx, doc); err != nil {
		return nil, fmt.Errorf("insert tenant: %w", err)
	}
	if _, err := s.db.Collection(tenantrepo.CollAncestors).InsertOne(ctx, bson.M{
		"descendantUUID": tenantUUID,
		"ancestorUUID":   tenantUUID,
		"depth":          0,
		"createdAt":      now,
	}); err != nil {
		return nil, fmt.Errorf("insert self-ancestor: %w", err)
	}
	return &migrator.TenantSnapshot{UUID: tenantUUID}, nil
}

func (s *mongoStore) PatchTenant(ctx context.Context, tenantUUID string, p migrator.TenantPatch) error {
	set := bson.M{"updatedAt": time.Now().UTC()}
	if p.LegalName != "" {
		set["legalName"] = p.LegalName
	}
	if p.VATNumber != "" {
		set["vatNumber"] = p.VATNumber
	}
	if p.FiscalCode != "" {
		set["fiscalCode"] = p.FiscalCode
	}
	if p.Email != "" {
		set["primaryContact.email"] = p.Email
	}
	if p.StripeCustomerID != "" {
		set["stripeCustomerID"] = p.StripeCustomerID
	}
	if p.AddressLine1 != "" {
		set["billingAddress.line1"] = p.AddressLine1
	}
	if p.AddressLine2 != "" {
		set["billingAddress.line2"] = p.AddressLine2
	}
	if p.City != "" {
		set["billingAddress.city"] = p.City
	}
	if p.PostalCode != "" {
		set["billingAddress.postalCode"] = p.PostalCode
	}
	if p.Province != "" {
		set["billingAddress.province"] = p.Province
	}
	if p.Country != "" {
		set["billingAddress.country"] = p.Country
	}
	_, err := s.db.Collection(tenantrepo.CollTenants).UpdateOne(ctx,
		bson.M{"uuid": tenantUUID}, bson.M{"$set": set})
	return err
}

func (s *mongoStore) EnsureMembership(ctx context.Context, userUUID, tenantUUID string) (bool, error) {
	n, err := s.db.Collection(tenantrepo.CollMemberships).CountDocuments(ctx, bson.M{
		"userUUID": userUUID,
		"tenantId": tenantUUID,
	}, options.Count().SetLimit(1))
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	now := time.Now().UTC()
	_, err = s.db.Collection(tenantrepo.CollMemberships).InsertOne(ctx, bson.M{
		"uuid":       migrator.MintTenantUUID(),
		"userUUID":   userUUID,
		"tenantId":   tenantUUID,
		"tenantKind": "external",
		"roles":      []string{"org_owner"},
		"isOwner":    true,
		"joinedAt":   now,
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// PivotOwner rewrites every (ownerKind="user", ownerUUID=userUUID) row to
// (ownerKind="tenant", ownerUUID=tenantUUID) across the five collections
// that carry the polymorphic owner. The same updateMany shape applies to
// each — the underlying field names are identical thanks to the
// post-onboarding refactor.
func (s *mongoStore) PivotOwner(ctx context.Context, userUUID, tenantUUID string) (migrator.PivotCounts, error) {
	var c migrator.PivotCounts
	for _, target := range []struct {
		coll string
		dst  *int64
	}{
		{submodels.SubscriptionsCollection, &c.Subscriptions},
		{submodels.InvoicesCollection, &c.Invoices},
		{pmtmodels.TransactionsCollection, &c.Transactions},
		{pmtmodels.PaymentMethodsCollection, &c.PaymentMethods},
		{tenantrepo.CollEntitlements, &c.Entitlements},
	} {
		res, err := s.db.Collection(target.coll).UpdateMany(ctx,
			bson.M{"ownerKind": "user", "ownerUUID": userUUID},
			bson.M{"$set": bson.M{"ownerKind": "tenant", "ownerUUID": tenantUUID}})
		if err != nil {
			return c, fmt.Errorf("pivot %s: %w", target.coll, err)
		}
		*target.dst = res.ModifiedCount
	}
	return c, nil
}

func (s *mongoStore) MarkSentinel(ctx context.Context, sourceID, userUUID, tenantUUID string, p migrator.PivotCounts) error {
	now := time.Now().UTC()
	_, err := s.db.Collection(sentinelCollection).UpdateOne(ctx,
		bson.M{"migration": migrator.MigrationName, "sourceID": sourceID},
		bson.M{"$set": bson.M{
			"migration":   migrator.MigrationName,
			"sourceID":    sourceID,
			"userUUID":    userUUID,
			"tenantUUID":  tenantUUID,
			"completedAt": now,
			"pivots": bson.M{
				"subscriptions":  p.Subscriptions,
				"invoices":       p.Invoices,
				"transactions":   p.Transactions,
				"paymentMethods": p.PaymentMethods,
				"entitlements":   p.Entitlements,
			},
		}},
		options.Update().SetUpsert(true))
	return err
}

// shortID returns the first 8 chars of a UUID-like string for slug purposes,
// or the whole string when it is shorter than 8 chars (defensive — every
// production UUID is far longer).
func shortID(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

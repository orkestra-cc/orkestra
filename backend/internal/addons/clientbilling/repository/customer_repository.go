// Package repository owns the MongoDB I/O for the clientbilling addon.
package repository

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/addons/clientbilling/models"
)

// ErrNotFound signals the user has no billing profile row yet. Service
// layer translates it to (nil, nil) for cross-module callers — "no profile
// yet" is a normal state, not an error.
var ErrNotFound = errors.New("clientbilling: user billing profile not found")

// CustomerRepository is the storage contract for the user-level billing
// projection. Repositories are interfaces so tests can substitute fakes.
type CustomerRepository interface {
	// GetByUserUUID returns the row for userUUID, or ErrNotFound.
	GetByUserUUID(ctx context.Context, userUUID string) (*models.UserBillingCustomer, error)
	// Upsert creates or replaces the row for c.UserUUID. CreatedAt is
	// preserved on update; UpdatedAt is stamped on every call. Returns the
	// freshly-persisted row so the caller can echo it back to the SPA.
	Upsert(ctx context.Context, c *models.UserBillingCustomer) (*models.UserBillingCustomer, error)
	// SetStripeCustomerID writes only the StripeCustomerID + UpdatedAt
	// fields, leaving the rest of the document untouched. Used by the
	// payment flow on first charge so a concurrent user-driven Upsert does
	// not clobber the freshly-created Stripe id.
	SetStripeCustomerID(ctx context.Context, userUUID, stripeCustomerID string) error
}

type customerRepository struct {
	collection *mongo.Collection
}

// NewCustomerRepository wires the repository to the addon's collection.
func NewCustomerRepository(db *mongo.Database) CustomerRepository {
	return &customerRepository{
		collection: db.Collection(models.CustomersCollection),
	}
}

func (r *customerRepository) GetByUserUUID(ctx context.Context, userUUID string) (*models.UserBillingCustomer, error) {
	var c models.UserBillingCustomer
	//tenantscope:allow user billing profile is keyed by userUUID (polymorphic owner) — no tenant scope applicable
	err := r.collection.FindOne(ctx, bson.M{"userUUID": userUUID}).Decode(&c)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *customerRepository) Upsert(ctx context.Context, c *models.UserBillingCustomer) (*models.UserBillingCustomer, error) {
	now := time.Now().UTC()
	c.UpdatedAt = now
	// $setOnInsert preserves CreatedAt across updates; the bson tag on the
	// model already excludes the zero time when omitempty triggers, but we
	// pin the value explicitly so the document carries the original
	// creation timestamp regardless of what the caller put on c.
	setDoc := bson.M{
		"userUUID":     c.UserUUID,
		"legalName":    c.LegalName,
		"firstName":    c.FirstName,
		"lastName":     c.LastName,
		"email":        c.Email,
		"vatNumber":    c.VATNumber,
		"fiscalCode":   c.FiscalCode,
		"country":      c.Country,
		"addressLine1": c.AddressLine1,
		"addressLine2": c.AddressLine2,
		"city":         c.City,
		"postalCode":   c.PostalCode,
		"province":     c.Province,
		"isCompany":    c.IsCompany,
		"updatedAt":    now,
	}
	update := bson.M{
		"$set":         setDoc,
		"$setOnInsert": bson.M{"createdAt": now},
	}
	//tenantscope:allow user billing profile is keyed by userUUID (polymorphic owner) — no tenant scope applicable
	if _, err := r.collection.UpdateOne(ctx,
		bson.M{"userUUID": c.UserUUID},
		update,
		options.Update().SetUpsert(true),
	); err != nil {
		return nil, err
	}
	return r.GetByUserUUID(ctx, c.UserUUID)
}

func (r *customerRepository) SetStripeCustomerID(ctx context.Context, userUUID, stripeCustomerID string) error {
	now := time.Now().UTC()
	//tenantscope:allow user billing profile is keyed by userUUID (polymorphic owner) — no tenant scope applicable
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"userUUID": userUUID},
		bson.M{"$set": bson.M{
			"stripeCustomerID": stripeCustomerID,
			"updatedAt":        now,
		}},
	)
	return err
}

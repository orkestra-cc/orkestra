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

	tenantrepo "github.com/orkestra/backend/internal/core/tenant/repository"

	"github.com/orkestra/backend/cmd/migrations/0003_billing_customer_to_tenant/migrator"
)

// Collection name constants. The billing.Customer Go surface goes away in
// the same Phase-5 PR, so the migrator can't import its model package — the
// collection name is duplicated here intentionally so this binary stays
// compilable after the deletion.
const (
	billingCustomersCollection = "billing_customers"
	billingInvoicesCollection  = "billing_invoices"
	sentinelCollection         = "migrations_applied"
)

type mongoStore struct {
	db *mongo.Database
}

func newMongoStore(db *mongo.Database) *mongoStore { return &mongoStore{db: db} }

func (s *mongoStore) SourceRows(ctx context.Context) ([]migrator.SourceRow, error) {
	cur, err := s.db.Collection(billingCustomersCollection).
		Find(ctx, bson.M{"deletedAt": nil}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var raw []struct {
		ID                 primitive.ObjectID `bson:"_id"`
		UUID               string             `bson:"uuid"`
		TenantUUID         string             `bson:"tenantUUID,omitempty"`
		FiscalIDCountry    string             `bson:"fiscalIdCountry,omitempty"`
		FiscalIDCode       string             `bson:"fiscalIdCode,omitempty"`
		CodiceFiscale      string             `bson:"codiceFiscale,omitempty"`
		IsCompany          bool               `bson:"isCompany"`
		Denomination       string             `bson:"denomination,omitempty"`
		Name               string             `bson:"name,omitempty"`
		Surname            string             `bson:"surname,omitempty"`
		Address            string             `bson:"address,omitempty"`
		NumeroCivico       string             `bson:"numeroCivico,omitempty"`
		City               string             `bson:"city,omitempty"`
		Province           string             `bson:"province,omitempty"`
		PostalCode         string             `bson:"postalCode,omitempty"`
		Country            string             `bson:"country,omitempty"`
		Email              string             `bson:"email,omitempty"`
		PEC                string             `bson:"pec,omitempty"`
		Phone              string             `bson:"phone,omitempty"`
		CodiceDestinatario string             `bson:"codiceDestinatario,omitempty"`
		PECDestinatario    string             `bson:"pecDestinatario,omitempty"`
		IsPA               bool               `bson:"isPA,omitempty"`
		CodiceUfficio      string             `bson:"codiceUfficio,omitempty"`
		RiferimentoAmm     string             `bson:"riferimentoAmm,omitempty"`
		ConvenzioneNumero  string             `bson:"convenzioneNumero,omitempty"`
	}
	if err := cur.All(ctx, &raw); err != nil {
		return nil, err
	}
	out := make([]migrator.SourceRow, 0, len(raw))
	for _, r := range raw {
		out = append(out, migrator.SourceRow{
			ID:                 r.ID.Hex(),
			UUID:               r.UUID,
			TenantUUID:         r.TenantUUID,
			FiscalIDCountry:    r.FiscalIDCountry,
			FiscalIDCode:       r.FiscalIDCode,
			CodiceFiscale:      r.CodiceFiscale,
			IsCompany:          r.IsCompany,
			Denomination:       r.Denomination,
			Name:               r.Name,
			Surname:            r.Surname,
			Address:            r.Address,
			NumeroCivico:       r.NumeroCivico,
			City:               r.City,
			Province:           r.Province,
			PostalCode:         r.PostalCode,
			Country:            r.Country,
			Email:              r.Email,
			PEC:                r.PEC,
			Phone:              r.Phone,
			CodiceDestinatario: r.CodiceDestinatario,
			PECDestinatario:    r.PECDestinatario,
			IsPA:               r.IsPA,
			CodiceUfficio:      r.CodiceUfficio,
			RiferimentoAmm:     r.RiferimentoAmm,
			ConvenzioneNumero:  r.ConvenzioneNumero,
		})
	}
	return out, nil
}

func (s *mongoStore) SentinelExists(ctx context.Context, customerUUID string) (bool, error) {
	n, err := s.db.Collection(sentinelCollection).CountDocuments(ctx, bson.M{
		"migration":    migrator.MigrationName,
		"customerUUID": customerUUID,
	}, options.Count().SetLimit(1))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *mongoStore) GetTenant(ctx context.Context, tenantUUID string) (*migrator.TenantSnapshot, error) {
	var t struct {
		UUID              string `bson:"uuid"`
		IsCompany         bool   `bson:"isCompany,omitempty"`
		IsItalianBillable bool   `bson:"isItalianBillable,omitempty"`
		LegalName         string `bson:"legalName,omitempty"`
		VATNumber         string `bson:"vatNumber,omitempty"`
		FiscalCode        string `bson:"fiscalCode,omitempty"`
		PrimaryContact    struct {
			Email string `bson:"email,omitempty"`
		} `bson:"primaryContact,omitempty"`
		BillingAddress struct {
			Line1      string `bson:"line1,omitempty"`
			City       string `bson:"city,omitempty"`
			Province   string `bson:"province,omitempty"`
			PostalCode string `bson:"postalCode,omitempty"`
			Country    string `bson:"country,omitempty"`
		} `bson:"billingAddress,omitempty"`
		FatturaPA *struct {
			CodiceDestinatario string `bson:"codiceDestinatario,omitempty"`
			PECDestinatario    string `bson:"pecDestinatario,omitempty"`
		} `bson:"fatturaPA,omitempty"`
	}
	err := s.db.Collection(tenantrepo.CollTenants).FindOne(ctx, bson.M{
		"uuid":      tenantUUID,
		"deletedAt": nil,
	}).Decode(&t)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snap := &migrator.TenantSnapshot{
		UUID:              t.UUID,
		IsCompany:         t.IsCompany,
		IsItalianBillable: t.IsItalianBillable,
		LegalName:         t.LegalName,
		VATNumber:         t.VATNumber,
		FiscalCode:        t.FiscalCode,
		Email:             t.PrimaryContact.Email,
		AddressLine1:      t.BillingAddress.Line1,
		City:              t.BillingAddress.City,
		PostalCode:        t.BillingAddress.PostalCode,
		Province:          t.BillingAddress.Province,
		Country:           t.BillingAddress.Country,
	}
	if t.FatturaPA != nil && (t.FatturaPA.CodiceDestinatario != "" || t.FatturaPA.PECDestinatario != "") {
		snap.HasFatturaPA = true
	}
	return snap, nil
}

func (s *mongoStore) CreateTenantFromCustomer(ctx context.Context, in migrator.CreateTenantInput) (*migrator.TenantSnapshot, error) {
	tenantUUID := migrator.MintTenantUUID()
	now := time.Now().UTC()
	doc := bson.M{
		"uuid":      tenantUUID,
		"kind":      "external",
		"status":    "active",
		"name":      in.Name,
		"slug":      fmt.Sprintf("customer-%s", shortID(tenantUUID)),
		"isCompany": in.IsCompany,
		"region":    "eu-west",
		"plan":      "free",
		"createdAt": now,
		"updatedAt": now,
	}
	if in.LegalName != "" {
		doc["legalName"] = in.LegalName
	}
	if in.VATNumber != "" {
		doc["vatNumber"] = in.VATNumber
	}
	if in.FiscalCode != "" {
		doc["fiscalCode"] = in.FiscalCode
	}
	contact := bson.M{}
	if in.Email != "" {
		contact["email"] = in.Email
	}
	if in.Phone != "" {
		contact["phone"] = in.Phone
	}
	if len(contact) > 0 {
		doc["primaryContact"] = contact
	}
	addr := bson.M{}
	if in.Address.Line1 != "" {
		addr["line1"] = in.Address.Line1
	}
	if in.Address.City != "" {
		addr["city"] = in.Address.City
	}
	if in.Address.Province != "" {
		addr["province"] = in.Address.Province
	}
	if in.Address.PostalCode != "" {
		addr["postalCode"] = in.Address.PostalCode
	}
	if in.Address.Country != "" {
		addr["country"] = in.Address.Country
	}
	if len(addr) > 0 {
		doc["billingAddress"] = addr
	}
	if in.FatturaPA != nil {
		doc["fatturaPA"] = fatturaPABSON(in.FatturaPA)
		doc["isItalianBillable"] = true
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
	return &migrator.TenantSnapshot{
		UUID:              tenantUUID,
		IsCompany:         in.IsCompany,
		IsItalianBillable: in.FatturaPA != nil,
		LegalName:         in.LegalName,
		VATNumber:         in.VATNumber,
		FiscalCode:        in.FiscalCode,
		Email:             in.Email,
		AddressLine1:      in.Address.Line1,
		City:              in.Address.City,
		PostalCode:        in.Address.PostalCode,
		Province:          in.Address.Province,
		Country:           in.Address.Country,
		HasFatturaPA:      in.FatturaPA != nil,
	}, nil
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
	if p.AddressLine1 != "" {
		set["billingAddress.line1"] = p.AddressLine1
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
	if p.PromoteToCompany {
		set["isCompany"] = true
	}
	if p.FatturaPA != nil {
		set["fatturaPA"] = fatturaPABSON(p.FatturaPA)
	}
	if p.SetItalianBillable {
		set["isItalianBillable"] = true
	}
	_, err := s.db.Collection(tenantrepo.CollTenants).UpdateOne(ctx,
		bson.M{"uuid": tenantUUID}, bson.M{"$set": set})
	return err
}

func (s *mongoStore) BackfillInvoiceTenant(ctx context.Context, customerUUID, tenantUUID string) (int64, error) {
	res, err := s.db.Collection(billingInvoicesCollection).UpdateMany(ctx,
		bson.M{
			"customerId": customerUUID,
			"$or": []bson.M{
				{"tenantUUID": bson.M{"$exists": false}},
				{"tenantUUID": ""},
			},
		},
		bson.M{"$set": bson.M{"tenantUUID": tenantUUID}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func (s *mongoStore) MarkSentinel(ctx context.Context, customerUUID, tenantUUID string, invoicesUpdated int64) error {
	now := time.Now().UTC()
	_, err := s.db.Collection(sentinelCollection).UpdateOne(ctx,
		bson.M{"migration": migrator.MigrationName, "customerUUID": customerUUID},
		bson.M{"$set": bson.M{
			"migration":       migrator.MigrationName,
			"customerUUID":    customerUUID,
			"tenantUUID":      tenantUUID,
			"invoicesUpdated": invoicesUpdated,
			"completedAt":     now,
		}},
		options.Update().SetUpsert(true))
	return err
}

func fatturaPABSON(p *migrator.FatturaPAProfile) bson.M {
	doc := bson.M{}
	if p.CodiceDestinatario != "" {
		doc["codiceDestinatario"] = p.CodiceDestinatario
	}
	if p.PECDestinatario != "" {
		doc["pecDestinatario"] = p.PECDestinatario
	}
	if p.IsPA {
		doc["isPA"] = true
	}
	if p.CodiceUfficio != "" {
		doc["codiceUfficio"] = p.CodiceUfficio
	}
	if p.RiferimentoAmm != "" {
		doc["riferimentoAmm"] = p.RiferimentoAmm
	}
	if p.ConvenzioneNumero != "" {
		doc["convenzioneNumero"] = p.ConvenzioneNumero
	}
	return doc
}

// shortID returns the first 8 chars of a UUID-like string for slug purposes,
// or the whole string when shorter than 8 chars.
func shortID(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

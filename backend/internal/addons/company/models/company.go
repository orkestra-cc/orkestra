package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// CompanyLookup represents a company lookup result stored in MongoDB
type CompanyLookup struct {
	ID               primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID             string             `bson:"uuid" json:"uuid"`
	TaxCode          string             `bson:"taxCode" json:"taxCode"`
	CompanyName      string             `bson:"companyName" json:"companyName"`
	VATCode          string             `bson:"vatCode" json:"vatCode"`
	ActivityStatus   string             `bson:"activityStatus" json:"activityStatus"`
	SDICode          string             `bson:"sdiCode" json:"sdiCode"`
	RegistrationDate string             `bson:"registrationDate" json:"registrationDate"`
	Address          Address            `bson:"address" json:"address"`
	SourceID         string             `bson:"sourceId" json:"sourceId"`
	CreatedAt        time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt        time.Time          `bson:"updatedAt" json:"updatedAt"`

	// Tracking: which enrichment types have been fetched and when
	FetchedTypes map[string]time.Time `bson:"fetchedTypes,omitempty" json:"fetchedTypes,omitempty"`

	// Enrichment data (nil = not yet fetched)
	Advanced     *AdvancedData     `bson:"advanced,omitempty" json:"advanced,omitempty"`
	Marketing    *MarketingData    `bson:"marketing,omitempty" json:"marketing,omitempty"`
	Stakeholders *StakeholdersData `bson:"stakeholders,omitempty" json:"stakeholders,omitempty"`
	AML          *AMLData          `bson:"aml,omitempty" json:"aml,omitempty"`
}

// Address represents a company's registered office address
type Address struct {
	Street       string `bson:"street" json:"street"`
	StreetNumber string `bson:"streetNumber" json:"streetNumber"`
	Town         string `bson:"town" json:"town"`
	Province     string `bson:"province" json:"province"`
	ZipCode      string `bson:"zipCode" json:"zipCode"`
	Region       string `bson:"region" json:"region"`
}

// PaginationParams holds pagination parameters
type PaginationParams struct {
	Page     int
	PageSize int
}

package models

import "time"

// Vehicle represents a vehicle in the system
type Vehicle struct {
	ID                     string     `json:"id" bson:"_id,omitempty"`
	UUID                   string     `json:"uuid" bson:"uuid"`
	Nome                   string     `json:"nome" bson:"nome"`
	Targa                  string     `json:"targa" bson:"targa"`
	LicensePlate           string     `json:"license_plate" bson:"license_plate"`
	Brand                  string     `json:"brand" bson:"brand"`
	Model                  string     `json:"model" bson:"model"`
	Year                   int        `json:"year" bson:"year"`
	VIN                    string     `json:"vin" bson:"vin"`
	ScadenzaRevisione      *time.Time `json:"scadenza_revisione,omitempty" bson:"scadenza_revisione,omitempty"`
	RevisioneProgrammata   *time.Time `json:"revisione_programmata,omitempty" bson:"revisione_programmata,omitempty"`
	InsuranceExpiry        *time.Time `json:"insurance_expiry,omitempty" bson:"insurance_expiry,omitempty"`
	CarTaxExpiry           *time.Time `json:"car_tax_expiry,omitempty" bson:"car_tax_expiry,omitempty"`
	CreatedAt              time.Time  `json:"created_at" bson:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at" bson:"updated_at"`
}

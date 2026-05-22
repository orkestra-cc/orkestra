package match

import (
	"testing"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

func TestSoftMatchPerson(t *testing.T) {
	cases := []struct {
		name     string
		incoming *models.Person
		existing *models.Person
		want     bool
	}{
		{
			"exact first+last+phone match",
			&models.Person{FirstName: "Jane", LastName: "Doe", Phones: []models.PhoneEntry{{Number: "+39 012 345 6789"}}},
			&models.Person{FirstName: "jane", LastName: "doe", Phones: []models.PhoneEntry{{Number: "01-234-56789"}}},
			true,
		},
		{
			"name match but no phone overlap",
			&models.Person{FirstName: "Jane", LastName: "Doe", Phones: []models.PhoneEntry{{Number: "+39 011 111 1111"}}},
			&models.Person{FirstName: "Jane", LastName: "Doe", Phones: []models.PhoneEntry{{Number: "+39 022 222 2222"}}},
			false,
		},
		{
			"different first name",
			&models.Person{FirstName: "John", LastName: "Doe", Phones: []models.PhoneEntry{{Number: "01234567890"}}},
			&models.Person{FirstName: "Jane", LastName: "Doe", Phones: []models.PhoneEntry{{Number: "01234567890"}}},
			false,
		},
		{
			"empty incoming first name disables match",
			&models.Person{LastName: "Doe", Phones: []models.PhoneEntry{{Number: "01234567890"}}},
			&models.Person{LastName: "Doe", Phones: []models.PhoneEntry{{Number: "01234567890"}}},
			false,
		},
		{
			"both phones empty",
			&models.Person{FirstName: "Jane", LastName: "Doe"},
			&models.Person{FirstName: "Jane", LastName: "Doe"},
			false,
		},
		{
			"nil incoming",
			nil,
			&models.Person{FirstName: "Jane", LastName: "Doe"},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SoftMatchPerson(tc.incoming, tc.existing)
			if got != tc.want {
				t.Fatalf("SoftMatchPerson = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSoftMatchOrganization(t *testing.T) {
	cases := []struct {
		name     string
		incoming *models.Organization
		existing *models.Organization
		want     bool
	}{
		{
			"exact match after case fold",
			&models.Organization{LegalName: "ACME S.p.A."},
			&models.Organization{LegalName: "acme s.p.a."},
			true,
		},
		{
			"whitespace collapse",
			&models.Organization{LegalName: "ACME   S.p.A."},
			&models.Organization{LegalName: "acme s.p.a."},
			true,
		},
		{
			"whitespace around",
			&models.Organization{LegalName: "  ACME SPA  "},
			&models.Organization{LegalName: "acme spa"},
			true,
		},
		{
			"different name",
			&models.Organization{LegalName: "ACME S.p.A."},
			&models.Organization{LegalName: "ACME GmbH"},
			false,
		},
		{
			"empty disables",
			&models.Organization{LegalName: ""},
			&models.Organization{LegalName: ""},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SoftMatchOrganization(tc.incoming, tc.existing)
			if got != tc.want {
				t.Fatalf("SoftMatchOrganization = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// 12 digits → last 10
		{"+39 012-345-6789", "0123456789"},
		// 9 digits — shorter than 10, returned verbatim
		{"(02) 1234567", "021234567"},
		// 11 digits → last 10
		{"+1 415 555 0199", "4155550199"},
		// Fewer than 7 digits → "" (extension code noise)
		{"01234", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := NormalizePhone(tc.in)
			if got != tc.want {
				t.Fatalf("NormalizePhone(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeLegalName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ACME S.p.A.", "acme s.p.a."},
		{"  Foo   Bar  ", "foo bar"},
		{"", ""},
		{"Foo\t\tBar", "foo bar"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := NormalizeLegalName(tc.in)
			if got != tc.want {
				t.Fatalf("NormalizeLegalName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

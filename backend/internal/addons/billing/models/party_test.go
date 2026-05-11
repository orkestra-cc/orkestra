package models

import "testing"

func TestPartyData_GetDisplayName(t *testing.T) {
	cases := []struct {
		name string
		p    PartyData
		want string
	}{
		{
			"company with denomination",
			PartyData{IsCompany: true, Denomination: "Acme S.r.l.", Name: "ignored", Surname: "ignored"},
			"Acme S.r.l.",
		},
		{
			"person full name",
			PartyData{IsCompany: false, Name: "Mario", Surname: "Rossi"},
			"Mario Rossi",
		},
		{
			"person name only",
			PartyData{IsCompany: false, Name: "Mario"},
			"Mario",
		},
		{
			"company without denomination falls back to fiscal id",
			PartyData{IsCompany: true, FiscalIDCode: "02081880490"},
			"02081880490",
		},
		{
			"person with surname only falls back to fiscal id",
			PartyData{IsCompany: false, Surname: "Rossi", FiscalIDCode: "RSSMRA85T10A562S"},
			"RSSMRA85T10A562S",
		},
		{
			"empty",
			PartyData{},
			"",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.GetDisplayName(); got != c.want {
				t.Errorf("GetDisplayName() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSupplier_ToPartyData_CopiesFields(t *testing.T) {
	s := &Supplier{
		FiscalIDCountry: "IT",
		FiscalIDCode:    "02081880490",
		CodiceFiscale:   "02081880490",
		IsCompany:       true,
		Denomination:    "Fornitore S.r.l.",
		Name:            "n",
		Surname:         "s",
		RegimeFiscale:   RegimeFiscale("RF01"),
		Address:         "Via Roma 1",
		NumeroCivico:    "1",
		City:            "Roma",
		Province:        "RM",
		PostalCode:      "00100",
		Country:         "IT",
		Email:           "ops@example.com",
		PEC:             "pec@pec.example.com",
		Phone:           "+39 06 1234567",
		IBAN:            "IT60X0542811101000000123456", // NOT copied — bank details
		BIC:             "BCITITMM",                    // NOT copied
	}

	got := s.ToPartyData()
	if got == nil {
		t.Fatal("ToPartyData returned nil")
	}
	if got.FiscalIDCountry != s.FiscalIDCountry ||
		got.FiscalIDCode != s.FiscalIDCode ||
		got.CodiceFiscale != s.CodiceFiscale ||
		got.IsCompany != s.IsCompany ||
		got.Denomination != s.Denomination ||
		got.Name != s.Name ||
		got.Surname != s.Surname ||
		got.RegimeFiscale != s.RegimeFiscale ||
		got.Address != s.Address ||
		got.NumeroCivico != s.NumeroCivico ||
		got.City != s.City ||
		got.Province != s.Province ||
		got.PostalCode != s.PostalCode ||
		got.Country != s.Country ||
		got.Email != s.Email ||
		got.PEC != s.PEC {
		t.Errorf("PartyData mismatch:\n got=%+v\nfrom=%+v", got, s)
	}

	// CodiceDestinatario / PECDestinatario are buyer-only fields and must not leak from a supplier.
	if got.CodiceDestinatario != "" || got.PECDestinatario != "" {
		t.Errorf("unexpected buyer fields on supplier-derived PartyData: %+v", got)
	}
}

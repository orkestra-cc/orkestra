package models

import "testing"

func TestCompany_GetDisplayName(t *testing.T) {
	c := &Company{Denomination: "Acme S.r.l.", FiscalIDCode: "02081880490"}
	if got := c.GetDisplayName(); got != "Acme S.r.l." {
		t.Errorf("GetDisplayName = %q, want denomination", got)
	}
	c2 := &Company{FiscalIDCode: "02081880490"}
	if got := c2.GetDisplayName(); got != "02081880490" {
		t.Errorf("GetDisplayName fallback = %q, want fiscal id", got)
	}
}

func TestCompany_ToPartyData_FullREA(t *testing.T) {
	capitale := 50_000.00
	c := &Company{
		FiscalIDCountry:   "IT",
		FiscalIDCode:      "02081880490",
		CodiceFiscale:     "02081880490",
		Denomination:      "Acme S.r.l.",
		RegimeFiscale:     RegimeFiscale("RF01"),
		Address:           "Via Roma 1",
		NumeroCivico:      "1",
		City:              "Roma",
		Province:          "RM",
		PostalCode:        "00100",
		Country:           "IT",
		REAOffice:         "RM",
		REANumber:         "RM-12345",
		CapitaleSociale:   &capitale,
		SocioUnico:        "SU",
		StatoLiquidazione: "LN",
		Email:             "ops@example.com",
		PEC:               "pec@pec.example.com",
		Phone:             "+390612345678",
	}
	p := c.ToPartyData()
	if p == nil {
		t.Fatal("ToPartyData returned nil")
	}
	if !p.IsCompany {
		t.Errorf("PartyData.IsCompany must be true for company-derived data")
	}
	if p.Denomination != "Acme S.r.l." || p.CodiceFiscale != "02081880490" {
		t.Errorf("denomination/CF not copied: %+v", p)
	}
	if p.IscrizioneREA == nil {
		t.Fatal("IscrizioneREA should be populated when all REA fields are set")
	}
	if p.IscrizioneREA.Ufficio != "RM" || p.IscrizioneREA.NumeroREA != "RM-12345" ||
		p.IscrizioneREA.CapitaleSociale != 50_000.00 ||
		p.IscrizioneREA.SocioUnico != "SU" ||
		p.IscrizioneREA.StatoLiquidazione != "LN" {
		t.Errorf("IscrizioneREA mismatch: %+v", p.IscrizioneREA)
	}
}

func TestCompany_ToPartyData_PartialREAOmitted(t *testing.T) {
	// Per Article 2250 Civil Code, partial REA is invalid; PartyData should
	// leave IscrizioneREA nil rather than emit incomplete data.
	cases := []*Company{
		{REAOffice: "RM", REANumber: "RM-1"},                   // missing StatoLiquidazione
		{REAOffice: "RM", StatoLiquidazione: "LN"},              // missing NumeroREA
		{REANumber: "RM-1", StatoLiquidazione: "LN"},            // missing REAOffice
		{},                                                      // none set
	}
	for i, c := range cases {
		p := c.ToPartyData()
		if p.IscrizioneREA != nil {
			t.Errorf("case %d: expected nil IscrizioneREA, got %+v", i, p.IscrizioneREA)
		}
	}
}

func TestCompany_ToPartyData_NilCapitaleSociale(t *testing.T) {
	c := &Company{
		REAOffice:         "RM",
		REANumber:         "RM-1",
		StatoLiquidazione: "LN",
		// CapitaleSociale intentionally nil
	}
	p := c.ToPartyData()
	if p.IscrizioneREA == nil {
		t.Fatal("IscrizioneREA must populate even without share capital")
	}
	if p.IscrizioneREA.CapitaleSociale != 0 {
		t.Errorf("nil CapitaleSociale should default to 0, got %v", p.IscrizioneREA.CapitaleSociale)
	}
}

func TestDefaultPagination(t *testing.T) {
	p := DefaultPagination()
	if p.Page != 1 {
		t.Errorf("Page default = %d, want 1", p.Page)
	}
	if p.PageSize != 20 {
		t.Errorf("PageSize default = %d, want 20", p.PageSize)
	}
}

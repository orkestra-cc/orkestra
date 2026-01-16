package services

import (
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/billing/config"
	"github.com/orkestra/backend/internal/billing/models"
)

func TestValidateCodiceDestinatario(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		wantErr bool
	}{
		{"valid 7 chars", "ABCD123", false},
		{"valid 6 chars", "ABC123", false},
		{"empty allowed", "", false},
		{"too short", "ABC", true},
		{"too long", "ABCD12345", true},
		{"lowercase gets normalized", "abcd123", false}, // Should normalize to uppercase
		{"invalid chars", "ABC-123", true},
		{"special code 7 zeros", "0000000", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCodiceDestinatario(tt.code)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCodiceDestinatario(%q) error = %v, wantErr %v", tt.code, err, tt.wantErr)
			}
		})
	}
}

func TestValidateCAP(t *testing.T) {
	tests := []struct {
		name    string
		cap     string
		wantErr bool
	}{
		{"valid", "00100", false},
		{"valid leading zero", "01234", false},
		{"empty allowed", "", false},
		{"too short", "0010", true},
		{"too long", "001000", true},
		{"with letters", "0010A", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCAP(tt.cap)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCAP(%q) error = %v, wantErr %v", tt.cap, err, tt.wantErr)
			}
		})
	}
}

func TestValidateProvincia(t *testing.T) {
	tests := []struct {
		name    string
		prov    string
		wantErr bool
	}{
		{"valid Rome", "RM", false},
		{"valid Milan", "MI", false},
		{"empty allowed", "", false},
		{"lowercase gets normalized", "rm", false},
		{"too short", "R", true},
		{"too long", "RMX", true},
		{"with numbers", "R1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProvincia(tt.prov)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProvincia(%q) error = %v, wantErr %v", tt.prov, err, tt.wantErr)
			}
		})
	}
}

func TestValidateDocumentType(t *testing.T) {
	tests := []struct {
		name    string
		dt      string
		wantErr bool
	}{
		{"TD01 fattura", "TD01", false},
		{"TD04 nota credito", "TD04", false},
		{"TD24 fattura differita", "TD24", false},
		{"TD29 v1.9", "TD29", false},
		{"empty error", "", true},
		{"invalid TD13", "TD13", true},
		{"invalid format", "XX01", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDocumentType(tt.dt)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDocumentType(%q) error = %v, wantErr %v", tt.dt, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePaymentMethod(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		wantErr bool
	}{
		{"MP01 contanti", "MP01", false},
		{"MP05 bonifico", "MP05", false},
		{"MP23 PagoPA", "MP23", false},
		{"empty allowed", "", false},
		{"invalid", "MP99", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePaymentMethod(tt.method)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePaymentMethod(%q) error = %v, wantErr %v", tt.method, err, tt.wantErr)
			}
		})
	}
}

func TestValidateVATNature(t *testing.T) {
	tests := []struct {
		name    string
		natura  string
		wantErr bool
	}{
		{"N1 escluse", "N1", false},
		{"N2.1 non soggette", "N2.1", false},
		{"N4 esenti", "N4", false},
		{"N6.9 reverse charge", "N6.9", false},
		{"N7 altro stato UE", "N7", false},
		{"empty allowed", "", false},
		{"invalid N8", "N8", true},
		{"invalid format", "X1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVATNature(tt.natura)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateVATNature(%q) error = %v, wantErr %v", tt.natura, err, tt.wantErr)
			}
		})
	}
}

func TestValidateRegimeFiscale(t *testing.T) {
	tests := []struct {
		name    string
		regime  string
		wantErr bool
	}{
		{"RF01 ordinario", "RF01", false},
		{"RF19 forfettario", "RF19", false},
		{"RF20 v1.9", "RF20", false},
		{"empty error", "", true},
		{"invalid RF03", "RF03", true},
		{"invalid RF21", "RF21", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRegimeFiscale(tt.regime)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRegimeFiscale(%q) error = %v, wantErr %v", tt.regime, err, tt.wantErr)
			}
		})
	}
}

func TestValidateIBAN(t *testing.T) {
	tests := []struct {
		name    string
		iban    string
		wantErr bool
	}{
		{"valid Italian", "IT60X0542811101000000123456", false},
		{"valid with spaces gets normalized", "IT60 X054 2811 1010 0000 0123 456", false},
		{"empty allowed", "", false},
		{"too short", "IT60X054", true},
		{"invalid format", "INVALID123456", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIBAN(tt.iban)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIBAN(%q) error = %v, wantErr %v", tt.iban, err, tt.wantErr)
			}
		})
	}
}

func TestValidateTipoRitenuta(t *testing.T) {
	tests := []struct {
		name    string
		tipo    string
		wantErr bool
	}{
		{"RT01 persone fisiche", "RT01", false},
		{"RT02 persone giuridiche", "RT02", false},
		{"RT06 altro", "RT06", false},
		{"empty error", "", true},
		{"invalid RT07", "RT07", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTipoRitenuta(tt.tipo)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTipoRitenuta(%q) error = %v, wantErr %v", tt.tipo, err, tt.wantErr)
			}
		})
	}
}

func TestValidateTipoCassa(t *testing.T) {
	tests := []struct {
		name    string
		tipo    string
		wantErr bool
	}{
		{"TC01", "TC01", false},
		{"TC22", "TC22", false},
		{"empty error", "", true},
		{"invalid TC23", "TC23", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTipoCassa(tt.tipo)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTipoCassa(%q) error = %v, wantErr %v", tt.tipo, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeFunctions(t *testing.T) {
	t.Run("NormalizeCAP", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"00100", "00100"},
			{"100", "00100"},
			{"1", "00001"},
			{"", ""},
			{" 00100 ", "00100"},
		}
		for _, tt := range tests {
			result := NormalizeCAP(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeCAP(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		}
	})

	t.Run("NormalizeProvincia", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"RM", "RM"},
			{"rm", "RM"},
			{" rm ", "RM"},
			{"", ""},
		}
		for _, tt := range tests {
			result := NormalizeProvincia(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeProvincia(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		}
	})

	t.Run("NormalizeNazione", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"IT", "IT"},
			{"it", "IT"},
			{"", "IT"}, // Default to IT
		}
		for _, tt := range tests {
			result := NormalizeNazione(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeNazione(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		}
	})

	t.Run("NormalizeIBAN", func(t *testing.T) {
		result := NormalizeIBAN("IT60 X054 2811 1010")
		expected := "IT60X05428111010" // All spaces removed, uppercase
		if result != expected {
			t.Errorf("NormalizeIBAN(%q) = %q, want %q", "IT60 X054 2811 1010", result, expected)
		}
	})
}

// Helper function to create a minimal valid invoice for testing
func createTestInvoice() *models.Invoice {
	now := time.Now()
	return &models.Invoice{
		UUID:             "test-uuid",
		Direction:        models.DirectionIssued,
		DocumentType:     models.DocTypeFattura,
		ProgressivoInvio: "TEST00001",
		Number:           "1/2024",
		Date:             now,
		Currency:         "EUR",
		CedentePrestatore: &models.PartyData{
			FiscalIDCountry: "IT",
			FiscalIDCode:    "12345678901",
			CodiceFiscale:   "12345678901",
			IsCompany:       true,
			Denomination:    "Test Company SRL",
			Address:         "Via Roma 1",
			PostalCode:      "00100",
			City:            "Roma",
			Province:        "RM",
			Country:         "IT",
			RegimeFiscale:   models.RegimeOrdinario,
		},
		CessionarioCommittente: &models.PartyData{
			FiscalIDCountry:    "IT",
			FiscalIDCode:       "98765432109",
			CodiceFiscale:      "98765432109",
			IsCompany:          true,
			Denomination:       "Customer SPA",
			Address:            "Via Milano 2",
			PostalCode:         "20100",
			City:               "Milano",
			Province:           "MI",
			Country:            "IT",
			CodiceDestinatario: "ABCD123",
		},
		Lines: []models.InvoiceLine{
			{
				LineNumber:    1,
				Description:   "Test service",
				Quantity:      1,
				UnitOfMeasure: models.UnitPiece,
				UnitPrice:     100.00,
				TotalPrice:    100.00,
				VATRate:       22.00,
				VATAmount:     22.00,
			},
		},
		VATSummary: []models.VATSummaryLine{
			{
				VATRate:       22.00,
				TaxableAmount: 100.00,
				VATAmount:     22.00,
			},
		},
		TotalTaxableAmount: 100.00,
		TotalVATAmount:     22.00,
		TotalAmount:        122.00,
		PaymentTerms: &models.PaymentTerms{
			Condition:     models.PaymentConditionCompleto,
			PaymentMethod: models.PaymentBonificoSepa,
			IBAN:          "IT60X0542811101000000123456",
		},
		Status: models.StatusDraft,
	}
}

func TestBuildXMLProlog(t *testing.T) {
	cfg := &config.OpenAPIConfig{
		FiscalID:      "IT12345678901",
		RecipientCode: "JKKZDGR",
	}
	builder := NewXMLBuilder(cfg)

	invoice := createTestInvoice()

	xml, err := builder.Build(invoice)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Verify XML prolog is at byte 0 with correct format
	expectedProlog := `<?xml version="1.0" encoding="UTF-8"?>`
	if !strings.HasPrefix(xml, expectedProlog) {
		// Show first 100 bytes for debugging
		preview := xml
		if len(preview) > 100 {
			preview = preview[:100]
		}
		t.Errorf("XML should start with prolog %q, but starts with: %q", expectedProlog, preview)
	}

	// Verify no BOM or whitespace before prolog
	if len(xml) > 0 && xml[0] != '<' {
		t.Errorf("XML should start with '<', but first byte is %q (0x%X)", string(xml[0]), xml[0])
	}
}

func TestBuildMinimalB2BInvoice(t *testing.T) {
	cfg := &config.OpenAPIConfig{
		FiscalID:      "IT12345678901",
		RecipientCode: "JKKZDGR",
	}
	builder := NewXMLBuilder(cfg)

	invoice := createTestInvoice()

	xml, err := builder.Build(invoice)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Verify essential elements are present
	checks := []string{
		`<p:FatturaElettronica`,
		`SistemaEmittente="ORKESTRA"`,
		`versione="FPR12"`,
		`<FatturaElettronicaHeader>`,
		`<DatiTrasmissione>`,
		`<IdTrasmittente>`,
		`<CedentePrestatore>`,
		`<CessionarioCommittente>`,
		`<FatturaElettronicaBody>`,
		`<DatiGenerali>`,
		`<DatiGeneraliDocumento>`,
		`<TipoDocumento>TD01</TipoDocumento>`,
		`<DatiBeniServizi>`,
		`<DettaglioLinee>`,
		`<DatiRiepilogo>`,
		`<DatiPagamento>`,
	}

	for _, check := range checks {
		if !strings.Contains(xml, check) {
			t.Errorf("XML missing expected content: %s", check)
		}
	}
}

func TestBuildInvoiceWithWithholdingTax(t *testing.T) {
	cfg := &config.OpenAPIConfig{
		FiscalID:      "IT12345678901",
		RecipientCode: "JKKZDGR",
	}
	builder := NewXMLBuilder(cfg)

	invoice := createTestInvoice()
	invoice.DatiRitenuta = []models.DatiRitenutaInput{
		{
			TipoRitenuta:     "RT01",
			ImportoRitenuta:  20.00,
			AliquotaRitenuta: 20.00,
			CausalePagamento: "A",
		},
	}

	xml, err := builder.Build(invoice)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Verify DatiRitenuta is present
	checks := []string{
		`<DatiRitenuta>`,
		`<TipoRitenuta>RT01</TipoRitenuta>`,
		`<ImportoRitenuta>20.00</ImportoRitenuta>`,
		`<AliquotaRitenuta>20.00</AliquotaRitenuta>`,
		`<CausalePagamento>A</CausalePagamento>`,
	}

	for _, check := range checks {
		if !strings.Contains(xml, check) {
			t.Errorf("XML missing expected content: %s", check)
		}
	}
}

func TestBuildInvoiceWithStampDuty(t *testing.T) {
	cfg := &config.OpenAPIConfig{
		FiscalID:      "IT12345678901",
		RecipientCode: "JKKZDGR",
	}
	builder := NewXMLBuilder(cfg)

	invoice := createTestInvoice()
	invoice.DatiBollo = &models.DatiBolloInput{
		ImportoBollo: 2.00,
	}

	xml, err := builder.Build(invoice)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Verify DatiBollo is present
	checks := []string{
		`<DatiBollo>`,
		`<BolloVirtuale>SI</BolloVirtuale>`,
		`<ImportoBollo>2.00</ImportoBollo>`,
	}

	for _, check := range checks {
		if !strings.Contains(xml, check) {
			t.Errorf("XML missing expected content: %s", check)
		}
	}
}

func TestBuildInvoiceWithSocialSecurityFund(t *testing.T) {
	cfg := &config.OpenAPIConfig{
		FiscalID:      "IT12345678901",
		RecipientCode: "JKKZDGR",
	}
	builder := NewXMLBuilder(cfg)

	invoice := createTestInvoice()
	invoice.DatiCassaPrevidenziale = []models.DatiCassaInput{
		{
			TipoCassa:              "TC01",
			AlCassa:                4.00,
			ImportoContributoCassa: 4.00,
			AliquotaIVA:            22.00,
		},
	}

	xml, err := builder.Build(invoice)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Verify DatiCassaPrevidenziale is present
	checks := []string{
		`<DatiCassaPrevidenziale>`,
		`<TipoCassa>TC01</TipoCassa>`,
		`<AlCassa>4.00</AlCassa>`,
		`<ImportoContributoCassa>4.00</ImportoContributoCassa>`,
	}

	for _, check := range checks {
		if !strings.Contains(xml, check) {
			t.Errorf("XML missing expected content: %s", check)
		}
	}
}

func TestBuildInvoiceWithREA(t *testing.T) {
	cfg := &config.OpenAPIConfig{
		FiscalID:      "IT12345678901",
		RecipientCode: "JKKZDGR",
	}
	builder := NewXMLBuilder(cfg)

	invoice := createTestInvoice()
	invoice.CedentePrestatore.IscrizioneREA = &models.IscrizioneREAInput{
		Ufficio:           "RM",
		NumeroREA:         "123456",
		CapitaleSociale:   10000.00,
		SocioUnico:        "SU",
		StatoLiquidazione: "LN",
	}

	xml, err := builder.Build(invoice)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Verify IscrizioneREA is present
	checks := []string{
		`<IscrizioneREA>`,
		`<Ufficio>RM</Ufficio>`,
		`<NumeroREA>123456</NumeroREA>`,
		`<CapitaleSociale>10000.00</CapitaleSociale>`,
		`<SocioUnico>SU</SocioUnico>`,
		`<StatoLiquidazione>LN</StatoLiquidazione>`,
	}

	for _, check := range checks {
		if !strings.Contains(xml, check) {
			t.Errorf("XML missing expected content: %s", check)
		}
	}
}

func TestBuildInvoiceWithMultipleProductCodes(t *testing.T) {
	cfg := &config.OpenAPIConfig{
		FiscalID:      "IT12345678901",
		RecipientCode: "JKKZDGR",
	}
	builder := NewXMLBuilder(cfg)

	invoice := createTestInvoice()
	invoice.Lines[0].CodiciArticolo = []models.ProductCode{
		{CodiceTipo: "INTERNO", CodiceValore: "SKU001"},
		{CodiceTipo: "EAN", CodiceValore: "1234567890123"},
	}

	xml, err := builder.Build(invoice)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Verify multiple CodiceArticolo elements
	if strings.Count(xml, "<CodiceArticolo>") != 2 {
		t.Error("Expected 2 CodiceArticolo elements")
	}

	checks := []string{
		`<CodiceTipo>INTERNO</CodiceTipo>`,
		`<CodiceValore>SKU001</CodiceValore>`,
		`<CodiceTipo>EAN</CodiceTipo>`,
		`<CodiceValore>1234567890123</CodiceValore>`,
	}

	for _, check := range checks {
		if !strings.Contains(xml, check) {
			t.Errorf("XML missing expected content: %s", check)
		}
	}
}

func TestBuildInvoiceWithPaymentDetails(t *testing.T) {
	cfg := &config.OpenAPIConfig{
		FiscalID:      "IT12345678901",
		RecipientCode: "JKKZDGR",
	}
	builder := NewXMLBuilder(cfg)

	invoice := createTestInvoice()
	invoice.PaymentTerms.Beneficiario = "Test Company SRL"
	invoice.PaymentTerms.IstitutoFinanziario = "Banca Test"
	invoice.PaymentTerms.ABI = "01234"
	invoice.PaymentTerms.CAB = "56789"
	invoice.PaymentTerms.BIC = "TESTITMM"

	xml, err := builder.Build(invoice)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	checks := []string{
		`<Beneficiario>Test Company SRL</Beneficiario>`,
		`<IstitutoFinanziario>Banca Test</IstitutoFinanziario>`,
		`<ABI>01234</ABI>`,
		`<CAB>56789</CAB>`,
		`<BIC>TESTITMM</BIC>`,
	}

	for _, check := range checks {
		if !strings.Contains(xml, check) {
			t.Errorf("XML missing expected content: %s", check)
		}
	}
}

func TestBuildValidationErrors(t *testing.T) {
	cfg := &config.OpenAPIConfig{
		FiscalID:      "IT12345678901",
		RecipientCode: "JKKZDGR",
	}
	builder := NewXMLBuilder(cfg)

	t.Run("invalid document type", func(t *testing.T) {
		invoice := createTestInvoice()
		invoice.DocumentType = "INVALID"

		_, err := builder.Build(invoice)
		if err == nil {
			t.Error("Expected validation error for invalid document type")
		}
	})

	t.Run("invalid regime fiscale", func(t *testing.T) {
		invoice := createTestInvoice()
		invoice.CedentePrestatore.RegimeFiscale = "RF99"

		_, err := builder.Build(invoice)
		if err == nil {
			t.Error("Expected validation error for invalid regime fiscale")
		}
	})

	t.Run("invalid CAP format", func(t *testing.T) {
		invoice := createTestInvoice()
		invoice.CedentePrestatore.PostalCode = "ABCDE"

		_, err := builder.Build(invoice)
		if err == nil {
			t.Error("Expected validation error for invalid CAP")
		}
	})

	t.Run("invalid payment method", func(t *testing.T) {
		invoice := createTestInvoice()
		invoice.PaymentTerms.PaymentMethod = "MP99"

		_, err := builder.Build(invoice)
		if err == nil {
			t.Error("Expected validation error for invalid payment method")
		}
	})
}

func TestBuildPAInvoice(t *testing.T) {
	cfg := &config.OpenAPIConfig{
		FiscalID:      "IT12345678901",
		RecipientCode: "JKKZDGR",
	}
	builder := NewXMLBuilder(cfg)

	invoice := createTestInvoice()
	// PA invoices have 6-char recipient codes
	invoice.CessionarioCommittente.CodiceDestinatario = "ABC123"

	xml, err := builder.Build(invoice)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// PA format should be FPA12
	if !strings.Contains(xml, `versione="FPA12"`) {
		t.Error("Expected FPA12 format for PA invoice")
	}
}

func TestValidateInvoiceForXML(t *testing.T) {
	t.Run("valid invoice", func(t *testing.T) {
		invoice := createTestInvoice()
		err := ValidateInvoiceForXML(invoice)
		if err != nil {
			t.Errorf("Expected no error for valid invoice, got: %v", err)
		}
	})

	t.Run("nil invoice", func(t *testing.T) {
		err := ValidateInvoiceForXML(nil)
		if err == nil {
			t.Error("Expected error for nil invoice")
		}
	})

	t.Run("invalid withholding tax type", func(t *testing.T) {
		invoice := createTestInvoice()
		invoice.DatiRitenuta = []models.DatiRitenutaInput{
			{TipoRitenuta: "RT99"},
		}
		err := ValidateInvoiceForXML(invoice)
		if err == nil {
			t.Error("Expected error for invalid TipoRitenuta")
		}
	})

	t.Run("invalid social security fund type", func(t *testing.T) {
		invoice := createTestInvoice()
		invoice.DatiCassaPrevidenziale = []models.DatiCassaInput{
			{TipoCassa: "TC99"},
		}
		err := ValidateInvoiceForXML(invoice)
		if err == nil {
			t.Error("Expected error for invalid TipoCassa")
		}
	})
}

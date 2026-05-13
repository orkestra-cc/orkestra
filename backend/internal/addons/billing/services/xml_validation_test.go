package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/addons/billing/models"
)

// -- single-field validators -----------------------------------------------

func TestValidateCodiceDestinatario_More(t *testing.T) {
	cases := []struct {
		in      string
		wantErr error
	}{
		{"", nil},                               // optional
		{"ABC123", nil},                         // 6 chars upper
		{"abc123", nil},                         // 6 chars lower — uppercased then matches
		{"ABCDEFG", nil},                        // 7 chars upper
		{"ABCDE", ErrInvalidCodiceDestinatario}, // too short
		{"ABCDEFGH", ErrInvalidCodiceDestinatario}, // too long
		{"ABC-12", ErrInvalidCodiceDestinatario},   // bad char
	}
	for _, c := range cases {
		err := ValidateCodiceDestinatario(c.in)
		if c.wantErr == nil {
			if err != nil {
				t.Errorf("ValidateCodiceDestinatario(%q): unexpected err %v", c.in, err)
			}
			continue
		}
		if !errors.Is(err, c.wantErr) {
			t.Errorf("ValidateCodiceDestinatario(%q): err = %v, want wraps %v", c.in, err, c.wantErr)
		}
	}
}

func TestValidateCAP_More(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"", true},
		{"00100", true},
		{"99999", true},
		{"1234", false},
		{"123456", false},
		{"abcde", false},
	}
	for _, c := range cases {
		err := ValidateCAP(c.in)
		if (err == nil) != c.ok {
			t.Errorf("ValidateCAP(%q): err=%v ok=%v", c.in, err, c.ok)
		}
	}
}

func TestValidateProvincia_More(t *testing.T) {
	if err := ValidateProvincia(""); err != nil {
		t.Errorf("empty provincia should be OK, got %v", err)
	}
	if err := ValidateProvincia("MI"); err != nil {
		t.Errorf("MI should validate, got %v", err)
	}
	if err := ValidateProvincia("mi"); err != nil {
		t.Errorf("lowercase mi should normalize and validate, got %v", err)
	}
	if err := ValidateProvincia("M1"); !errors.Is(err, ErrInvalidProvincia) {
		t.Errorf("M1 must fail, got %v", err)
	}
	if err := ValidateProvincia("MIL"); !errors.Is(err, ErrInvalidProvincia) {
		t.Errorf("3-char provincia must fail, got %v", err)
	}
}

func TestValidateNazione(t *testing.T) {
	if err := ValidateNazione(""); err != nil {
		t.Errorf("empty country should be OK (defaults to IT), got %v", err)
	}
	if err := ValidateNazione("IT"); err != nil {
		t.Errorf("IT must validate, got %v", err)
	}
	if err := ValidateNazione("it"); err != nil {
		t.Errorf("lowercase it should validate after uppercasing, got %v", err)
	}
	if err := ValidateNazione("ITA"); !errors.Is(err, ErrInvalidNazione) {
		t.Errorf("ITA must fail, got %v", err)
	}
}

func TestValidateCodiceFiscale(t *testing.T) {
	if err := ValidateCodiceFiscale(""); err != nil {
		t.Errorf("empty CF should be OK, got %v", err)
	}
	if err := ValidateCodiceFiscale("RSSMRA85T10A562S"); err != nil {
		t.Errorf("valid 16-char personal CF must pass, got %v", err)
	}
	if err := ValidateCodiceFiscale("rssmra85t10a562s"); err != nil {
		t.Errorf("lowercase CF should validate after uppercasing, got %v", err)
	}
	if err := ValidateCodiceFiscale("12345"); !errors.Is(err, ErrInvalidCodiceFiscale) {
		t.Errorf("5-char CF should fail")
	}
	if err := ValidateCodiceFiscale("12345678901234567"); !errors.Is(err, ErrInvalidCodiceFiscale) {
		t.Errorf("17-char CF should fail")
	}
}

func TestValidateCodiceFiscaleRequired(t *testing.T) {
	if err := ValidateCodiceFiscaleRequired("", "cedente"); err == nil ||
		!strings.Contains(err.Error(), "codice fiscale is required") {
		t.Errorf("empty CF must be rejected with 'required' message, got %v", err)
	}
	if err := ValidateCodiceFiscaleRequired("RSSMRA85T10A562S", "cedente"); err != nil {
		t.Errorf("valid CF must pass, got %v", err)
	}
	err := ValidateCodiceFiscaleRequired("bad", "cedente_prestatore")
	if err == nil || !errors.Is(err, ErrInvalidCodiceFiscale) {
		t.Errorf("invalid CF must wrap ErrInvalidCodiceFiscale, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "cedente_prestatore") {
		t.Errorf("error must include party type prefix, got %v", err)
	}
}

func TestValidatePartitaIVA(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"", true},
		{"02081880490", true},
		{"1234567890", false},   // 10 digits
		{"123456789012", false}, // 12 digits
		{"abcdefghijk", false},
	}
	for _, c := range cases {
		err := ValidatePartitaIVA(c.in)
		if (err == nil) != c.ok {
			t.Errorf("ValidatePartitaIVA(%q): err=%v ok=%v", c.in, err, c.ok)
		}
	}
}

func TestValidateIBAN_More(t *testing.T) {
	// Optional
	if err := ValidateIBAN(""); err != nil {
		t.Errorf("empty IBAN must pass, got %v", err)
	}
	// Valid Italian IBAN (length 27)
	if err := ValidateIBAN("IT60X0542811101000000123456"); err != nil {
		t.Errorf("valid IT IBAN must pass, got %v", err)
	}
	// Spaces should be removed
	if err := ValidateIBAN("IT60 X054 2811 1010 0000 0123 456"); err != nil {
		t.Errorf("IBAN with spaces must pass after normalization, got %v", err)
	}
	// Lowercase should be uppercased
	if err := ValidateIBAN("it60x0542811101000000123456"); err != nil {
		t.Errorf("lowercase IBAN must pass, got %v", err)
	}
	// Bad pattern
	if err := ValidateIBAN("12IT0542811101000000123456"); !errors.Is(err, ErrInvalidIBAN) {
		t.Errorf("invalid IBAN must wrap ErrInvalidIBAN, got %v", err)
	}
}

func TestValidateBIC(t *testing.T) {
	if err := ValidateBIC(""); err != nil {
		t.Errorf("empty BIC must pass, got %v", err)
	}
	if err := ValidateBIC("BCITITMM"); err != nil {
		t.Errorf("8-char BIC must pass, got %v", err)
	}
	if err := ValidateBIC("BCITITMMXXX"); err != nil {
		t.Errorf("11-char BIC must pass, got %v", err)
	}
	if err := ValidateBIC("bcititmm"); err != nil {
		t.Errorf("lowercase BIC should validate after uppercasing, got %v", err)
	}
	if err := ValidateBIC("BAD"); !errors.Is(err, ErrInvalidBIC) {
		t.Errorf("short BIC must fail")
	}
}

func TestValidateProgressivoInvio(t *testing.T) {
	if err := ValidateProgressivoInvio(""); err == nil {
		t.Error("empty progressivo must fail (required)")
	}
	if err := ValidateProgressivoInvio("ABC123"); err != nil {
		t.Errorf("alphanumeric within 10 chars must pass, got %v", err)
	}
	if err := ValidateProgressivoInvio("1234567890"); err != nil {
		t.Errorf("10-digit progressivo must pass, got %v", err)
	}
	if err := ValidateProgressivoInvio("12345678901"); !errors.Is(err, ErrInvalidProgressivoInvio) {
		t.Errorf("11-digit progressivo must fail")
	}
	if err := ValidateProgressivoInvio("AB-12"); !errors.Is(err, ErrInvalidProgressivoInvio) {
		t.Errorf("non-alnum progressivo must fail")
	}
}

func TestValidateABI_CAB(t *testing.T) {
	for _, in := range []string{"", "12345"} {
		if err := ValidateABI(in); err != nil {
			t.Errorf("ABI(%q) should be OK, got %v", in, err)
		}
		if err := ValidateCABCode(in); err != nil {
			t.Errorf("CAB(%q) should be OK, got %v", in, err)
		}
	}
	if !errors.Is(ValidateABI("1234"), ErrInvalidABI) {
		t.Error("4-digit ABI must fail")
	}
	if !errors.Is(ValidateCABCode("123456"), ErrInvalidCAB) {
		t.Error("6-digit CAB must fail")
	}
}

func TestValidateDocumentType_More(t *testing.T) {
	if err := ValidateDocumentType(""); err == nil {
		t.Error("empty DocumentType must fail (required)")
	}
	for _, dt := range []string{"TD01", "TD16", "TD29"} {
		if err := ValidateDocumentType(dt); err != nil {
			t.Errorf("%q must pass, got %v", dt, err)
		}
	}
	// TD13/14/15 are explicitly excluded by the spec
	for _, dt := range []string{"TD00", "TD13", "TD14", "TD15", "TD30", "td01"} {
		if err := ValidateDocumentType(dt); !errors.Is(err, ErrInvalidDocumentType) {
			t.Errorf("%q must fail with ErrInvalidDocumentType, got %v", dt, err)
		}
	}
}

func TestValidatePaymentCondition(t *testing.T) {
	if err := ValidatePaymentCondition(""); err != nil {
		t.Error("empty payment condition must be OK (optional)")
	}
	for _, c := range []string{"TP01", "TP02", "TP03"} {
		if err := ValidatePaymentCondition(c); err != nil {
			t.Errorf("%q must pass", c)
		}
	}
	if !errors.Is(ValidatePaymentCondition("TP04"), ErrInvalidPaymentCondition) {
		t.Error("TP04 must fail")
	}
}

func TestValidatePaymentMethod_More(t *testing.T) {
	if err := ValidatePaymentMethod(""); err != nil {
		t.Error("empty payment method must be OK (optional)")
	}
	for _, m := range []string{"MP01", "MP05", "MP23"} {
		if err := ValidatePaymentMethod(m); err != nil {
			t.Errorf("%q must pass", m)
		}
	}
	if !errors.Is(ValidatePaymentMethod("MP24"), ErrInvalidPaymentMethod) {
		t.Error("MP24 must fail")
	}
}

func TestValidateVATNature_More(t *testing.T) {
	if err := ValidateVATNature(""); err != nil {
		t.Error("empty VATNature must be OK (only required when rate=0)")
	}
	for _, n := range []string{"N1", "N2", "N2.1", "N3.6", "N6.9", "N7"} {
		if err := ValidateVATNature(n); err != nil {
			t.Errorf("%q must pass, got %v", n, err)
		}
	}
	for _, n := range []string{"N0", "N8", "N2.3"} {
		if !errors.Is(ValidateVATNature(n), ErrInvalidVATNature) {
			t.Errorf("%q must fail", n)
		}
	}
}

func TestValidateRegimeFiscale_More(t *testing.T) {
	if err := ValidateRegimeFiscale(""); err == nil {
		t.Error("empty regime fiscale must fail (required)")
	}
	for _, r := range []string{"RF01", "RF02", "RF19", "RF20"} {
		if err := ValidateRegimeFiscale(r); err != nil {
			t.Errorf("%q must pass, got %v", r, err)
		}
	}
	// RF03 is not in the spec
	for _, r := range []string{"RF03", "RF00", "RF21"} {
		if !errors.Is(ValidateRegimeFiscale(r), ErrInvalidRegimeFiscale) {
			t.Errorf("%q must fail", r)
		}
	}
}

func TestValidateTipoRitenuta_More(t *testing.T) {
	if err := ValidateTipoRitenuta(""); err == nil {
		t.Error("empty TipoRitenuta must fail (required)")
	}
	for _, r := range []string{"RT01", "RT02", "RT03", "RT04", "RT05", "RT06"} {
		if err := ValidateTipoRitenuta(r); err != nil {
			t.Errorf("%q must pass, got %v", r, err)
		}
	}
	if !errors.Is(ValidateTipoRitenuta("RT07"), ErrInvalidTipoRitenuta) {
		t.Error("RT07 must fail")
	}
}

func TestValidateTipoCassa_More(t *testing.T) {
	if err := ValidateTipoCassa(""); err == nil {
		t.Error("empty TipoCassa must fail (required)")
	}
	for _, c := range []string{"TC01", "TC11", "TC22"} {
		if err := ValidateTipoCassa(c); err != nil {
			t.Errorf("%q must pass, got %v", c, err)
		}
	}
	if !errors.Is(ValidateTipoCassa("TC23"), ErrInvalidTipoCassa) {
		t.Error("TC23 must fail")
	}
}

// -- InvoiceValidationError ------------------------------------------------

func TestInvoiceValidationError_EmptyAndAdd(t *testing.T) {
	e := &InvoiceValidationError{}
	if e.HasErrors() {
		t.Fatal("fresh error bag must have no errors")
	}
	if e.Error() != "no validation errors" {
		t.Fatalf("empty Error() = %q", e.Error())
	}
	e.Add(nil) // nil must not be appended
	if e.HasErrors() {
		t.Fatal("Add(nil) must be a no-op")
	}
	e.Add(errors.New("oops"))
	e.Add(errors.New("again"))
	if !e.HasErrors() {
		t.Fatal("HasErrors must be true after Add")
	}
	msg := e.Error()
	if !strings.Contains(msg, "oops") || !strings.Contains(msg, "again") {
		t.Fatalf("Error() = %q, want both messages joined", msg)
	}
	if !strings.HasPrefix(msg, "validation errors: ") {
		t.Fatalf("Error() = %q, want prefix", msg)
	}
}

// -- ValidateInvoiceForXML -------------------------------------------------

func minimalValidInvoice() *models.Invoice {
	return &models.Invoice{
		DocumentType:     models.DocTypeFattura,
		ProgressivoInvio: "ABC001",
		CedentePrestatore: &models.PartyData{
			FiscalIDCountry: "IT",
			FiscalIDCode:    "02081880490",
			CodiceFiscale:   "02081880490",
			RegimeFiscale:   models.RegimeFiscale("RF01"),
			PostalCode:      "00100",
			Province:        "RM",
			Country:         "IT",
		},
		CessionarioCommittente: &models.PartyData{
			FiscalIDCountry:    "IT",
			FiscalIDCode:       "12345678901",
			CodiceDestinatario: "ABC123",
			PostalCode:         "20100",
			Province:           "MI",
			Country:            "IT",
		},
	}
}

func TestValidateInvoiceForXML_Nil(t *testing.T) {
	if err := ValidateInvoiceForXML(nil); err == nil {
		t.Fatal("nil invoice must fail")
	}
}

func TestValidateInvoiceForXML_HappyPath(t *testing.T) {
	if err := ValidateInvoiceForXML(minimalValidInvoice()); err != nil {
		t.Fatalf("minimal valid invoice must pass, got %v", err)
	}
}

func TestValidateInvoiceForXML_ItalianCedenteDefaultsCFFromPIVA(t *testing.T) {
	inv := minimalValidInvoice()
	inv.CedentePrestatore.CodiceFiscale = "" // empty
	// Country is IT and FiscalIDCode is set → validation should fall back.
	if err := ValidateInvoiceForXML(inv); err != nil {
		t.Fatalf("IT cedente without CF should default to FiscalIDCode, got %v", err)
	}
}

func TestValidateInvoiceForXML_ForeignCedenteMissingCFFails(t *testing.T) {
	inv := minimalValidInvoice()
	inv.CedentePrestatore.CodiceFiscale = ""
	inv.CedentePrestatore.FiscalIDCountry = "DE" // not IT → no default
	err := ValidateInvoiceForXML(inv)
	if err == nil {
		t.Fatal("non-IT cedente without CF must fail")
	}
	if !strings.Contains(err.Error(), "codice fiscale is required") {
		t.Fatalf("error must mention required CF, got %v", err)
	}
}

func TestValidateInvoiceForXML_AggregatesPartyErrors(t *testing.T) {
	inv := minimalValidInvoice()
	inv.CedentePrestatore.PostalCode = "12"      // bad CAP — prefixed by validateParty
	inv.CedentePrestatore.RegimeFiscale = "RF99" // bad regime — prefixed by validateParty
	inv.CessionarioCommittente.PostalCode = "9"  // bad CAP on cessionario too — prefixed by validateParty
	err := ValidateInvoiceForXML(inv)
	if err == nil {
		t.Fatal("multi-error invoice must fail")
	}
	msg := err.Error()
	for _, want := range []string{"cedente_prestatore", "cessionario_committente", "invalid CAP", "invalid regime"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing fragment %q: %v", want, err)
		}
	}
}

func TestValidateInvoiceForXML_PaymentTermsValidated(t *testing.T) {
	inv := minimalValidInvoice()
	inv.PaymentTerms = &models.PaymentTerms{
		Condition:     models.PaymentCondition("TP04"), // invalid
		PaymentMethod: models.PaymentMethod("MP01"),
		IBAN:          "BAD",
		BIC:           "BAD",
		ABI:           "12",
		CAB:           "12",
	}
	err := ValidateInvoiceForXML(inv)
	if err == nil {
		t.Fatal("invalid payment terms must fail")
	}
	for _, want := range []string{"payment condition", "IBAN", "BIC", "ABI", "CAB"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

func TestValidateInvoiceForXML_LineAndSummaryVATNature(t *testing.T) {
	inv := minimalValidInvoice()
	inv.Lines = []models.InvoiceLine{
		{VATRate: 0, VATNature: models.VATNature("N99")}, // invalid nature at rate 0
	}
	inv.VATSummary = []models.VATSummaryLine{
		{VATRate: 0, VATNature: models.VATNature("BAD")},
	}
	err := ValidateInvoiceForXML(inv)
	if err == nil {
		t.Fatal("invalid VAT nature must fail")
	}
	if !strings.Contains(err.Error(), "line 1") || !strings.Contains(err.Error(), "VAT summary 1") {
		t.Errorf("expected line + summary error prefixes, got %v", err)
	}
}

func TestValidateInvoiceForXML_LineRateNonZeroSkipsNatureCheck(t *testing.T) {
	inv := minimalValidInvoice()
	inv.Lines = []models.InvoiceLine{
		// rate != 0 → natura check is skipped even if invalid
		{VATRate: 22.0, VATNature: models.VATNature("INVALID")},
	}
	if err := ValidateInvoiceForXML(inv); err != nil {
		t.Fatalf("non-zero VATRate should bypass natura check, got %v", err)
	}
}

func TestValidateInvoiceForXML_DatiRitenutaAndCassa(t *testing.T) {
	inv := minimalValidInvoice()
	inv.DatiRitenuta = []models.DatiRitenutaInput{{TipoRitenuta: "RT99"}}
	inv.DatiCassaPrevidenziale = []models.DatiCassaInput{
		{TipoCassa: "TC99", AliquotaIVA: 0, Natura: "INVALID"},
	}
	err := ValidateInvoiceForXML(inv)
	if err == nil {
		t.Fatal("invalid ritenuta + cassa must fail")
	}
	for _, want := range []string{"dati_ritenuta 1", "dati_cassa 1", "dati_cassa 1 natura"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

func TestValidateInvoiceForXML_NilParties(t *testing.T) {
	inv := &models.Invoice{
		DocumentType:     models.DocTypeFattura,
		ProgressivoInvio: "ABC001",
	}
	// Both parties nil → no party validation runs, and base validators pass.
	if err := ValidateInvoiceForXML(inv); err != nil {
		t.Fatalf("nil parties must skip party validation, got %v", err)
	}
}

// -- normalizers -----------------------------------------------------------

func TestNormalizeCAP(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"100", "00100"},
		{"  100  ", "00100"},
		{"00100", "00100"},
		{"1234567", "12345"}, // truncates
		{"12", "00012"},
	}
	for _, c := range cases {
		if got := NormalizeCAP(c.in); got != c.want {
			t.Errorf("NormalizeCAP(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeProvincia(t *testing.T) {
	cases := map[string]string{
		"":     "",
		"mi":   "MI",
		" rm ": "RM",
		"To":   "TO",
	}
	for in, want := range cases {
		if got := NormalizeProvincia(in); got != want {
			t.Errorf("NormalizeProvincia(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeNazione(t *testing.T) {
	if got := NormalizeNazione(""); got != "IT" {
		t.Errorf("empty NormalizeNazione should default to IT, got %q", got)
	}
	if got := NormalizeNazione("de"); got != "DE" {
		t.Errorf("NormalizeNazione('de') = %q", got)
	}
	if got := NormalizeNazione(" fr "); got != "FR" {
		t.Errorf("NormalizeNazione(' fr ') = %q", got)
	}
}

func TestNormalizeIBAN(t *testing.T) {
	if got := NormalizeIBAN(""); got != "" {
		t.Errorf("empty IBAN should normalize to empty, got %q", got)
	}
	got := NormalizeIBAN(" it60 X054 ")
	if got != "IT60X054" {
		t.Errorf("NormalizeIBAN strips spaces + uppercases, got %q", got)
	}
}

func TestNormalizeBIC(t *testing.T) {
	if got := NormalizeBIC(""); got != "" {
		t.Errorf("empty BIC stays empty, got %q", got)
	}
	if got := NormalizeBIC("  bcititmm  "); got != "BCITITMM" {
		t.Errorf("NormalizeBIC = %q", got)
	}
}

func TestNormalizeCodiceDestinatario(t *testing.T) {
	if got := NormalizeCodiceDestinatario(""); got != "" {
		t.Errorf("empty code stays empty, got %q", got)
	}
	if got := NormalizeCodiceDestinatario("  abc123  "); got != "ABC123" {
		t.Errorf("NormalizeCodiceDestinatario = %q", got)
	}
}

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"+39 333 1234567", "3331234567"},
		{"0039 333-12 34567", "3331234567"},
		{"(02) 1234567", "021234567"},
		// Only Italian +39 / 0039 country codes are stripped; the lone "+"
		// gets removed last (per the implementation), so "+1 555 0100" keeps "1".
		{"+1 555 0100", "15550100"},
		{"+390212345678", "0212345678"},
		// Length cap at 12
		{"1234567890123456", "123456789012"},
	}
	for _, c := range cases {
		if got := NormalizePhone(c.in); got != c.want {
			t.Errorf("NormalizePhone(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

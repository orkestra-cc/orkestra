package services

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/orkestra/backend/internal/billing/models"
)

// Validation errors
var (
	ErrInvalidCodiceDestinatario = fmt.Errorf("invalid codice destinatario: must be 6-7 uppercase alphanumeric characters")
	ErrInvalidCAP                = fmt.Errorf("invalid CAP: must be exactly 5 digits")
	ErrInvalidProvincia          = fmt.Errorf("invalid provincia: must be exactly 2 uppercase letters")
	ErrInvalidNazione            = fmt.Errorf("invalid nazione: must be exactly 2 uppercase letters (ISO 3166-1 alpha-2)")
	ErrInvalidCodiceFiscale      = fmt.Errorf("invalid codice fiscale: must be 11-16 uppercase alphanumeric characters")
	ErrInvalidPartitaIVA         = fmt.Errorf("invalid partita IVA: must be exactly 11 digits")
	ErrInvalidIBAN               = fmt.Errorf("invalid IBAN: must match pattern [A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}")
	ErrInvalidBIC                = fmt.Errorf("invalid BIC: must match pattern [A-Z]{6}[A-Z2-9][A-NP-Z0-9]([A-Z0-9]{3})?")
	ErrInvalidProgressivoInvio   = fmt.Errorf("invalid progressivo invio: must be 1-10 alphanumeric characters")
	ErrInvalidDocumentType       = fmt.Errorf("invalid document type: must be TD01-TD29")
	ErrInvalidPaymentCondition   = fmt.Errorf("invalid payment condition: must be TP01, TP02, or TP03")
	ErrInvalidPaymentMethod      = fmt.Errorf("invalid payment method: must be MP01-MP23")
	ErrInvalidVATNature          = fmt.Errorf("invalid VAT nature: must be N1-N7.x")
	ErrInvalidRegimeFiscale      = fmt.Errorf("invalid regime fiscale: must be RF01-RF20")
	ErrInvalidTipoRitenuta       = fmt.Errorf("invalid tipo ritenuta: must be RT01-RT06")
	ErrInvalidTipoCassa          = fmt.Errorf("invalid tipo cassa: must be TC01-TC22")
	ErrInvalidABI                = fmt.Errorf("invalid ABI: must be exactly 5 digits")
	ErrInvalidCAB                = fmt.Errorf("invalid CAB: must be exactly 5 digits")
)

// Compiled regex patterns for XSD validation
var (
	// CodiceDestinatario: [A-Z0-9]{6,7}
	regexCodiceDestinatario = regexp.MustCompile(`^[A-Z0-9]{6,7}$`)

	// CAP: [0-9]{5}
	regexCAP = regexp.MustCompile(`^[0-9]{5}$`)

	// Provincia: [A-Z]{2}
	regexProvincia = regexp.MustCompile(`^[A-Z]{2}$`)

	// Nazione: [A-Z]{2} (ISO 3166-1 alpha-2)
	regexNazione = regexp.MustCompile(`^[A-Z]{2}$`)

	// CodiceFiscale: [A-Z0-9]{11,16}
	regexCodiceFiscale = regexp.MustCompile(`^[A-Z0-9]{11,16}$`)

	// PartitaIVA: [0-9]{11}
	regexPartitaIVA = regexp.MustCompile(`^[0-9]{11}$`)

	// IBAN: [a-zA-Z]{2}[0-9]{2}[a-zA-Z0-9]{11,30}
	regexIBAN = regexp.MustCompile(`^[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}$`)

	// BIC: [A-Z]{6}[A-Z2-9][A-NP-Z0-9]([A-Z0-9]{3})?
	regexBIC = regexp.MustCompile(`^[A-Z]{6}[A-Z2-9][A-NP-Z0-9]([A-Z0-9]{3})?$`)

	// ProgressivoInvio: 1-10 alphanumeric (BasicLatin)
	regexProgressivoInvio = regexp.MustCompile(`^[A-Za-z0-9]{1,10}$`)

	// ABI: [0-9]{5}
	regexABI = regexp.MustCompile(`^[0-9]{5}$`)

	// CAB: [0-9]{5}
	regexCAB = regexp.MustCompile(`^[0-9]{5}$`)
)

// Valid document types per FatturaPA v1.2.3
var validDocumentTypes = map[string]bool{
	"TD01": true, "TD02": true, "TD03": true, "TD04": true, "TD05": true,
	"TD06": true, "TD07": true, "TD08": true, "TD09": true, "TD10": true,
	"TD11": true, "TD12": true,
	// TD13-TD15 not defined in schema
	"TD16": true, "TD17": true, "TD18": true, "TD19": true, "TD20": true,
	"TD21": true, "TD22": true, "TD23": true, "TD24": true, "TD25": true,
	"TD26": true, "TD27": true, "TD28": true, "TD29": true,
}

// Valid payment methods per FatturaPA v1.2.3
var validPaymentMethods = map[string]bool{
	"MP01": true, "MP02": true, "MP03": true, "MP04": true, "MP05": true,
	"MP06": true, "MP07": true, "MP08": true, "MP09": true, "MP10": true,
	"MP11": true, "MP12": true, "MP13": true, "MP14": true, "MP15": true,
	"MP16": true, "MP17": true, "MP18": true, "MP19": true, "MP20": true,
	"MP21": true, "MP22": true, "MP23": true,
}

// Valid payment conditions per FatturaPA v1.2.3
var validPaymentConditions = map[string]bool{
	"TP01": true, "TP02": true, "TP03": true,
}

// Valid VAT nature codes per FatturaPA v1.2.3
var validVATNatures = map[string]bool{
	"N1": true,
	"N2": true, "N2.1": true, "N2.2": true,
	"N3": true, "N3.1": true, "N3.2": true, "N3.3": true, "N3.4": true, "N3.5": true, "N3.6": true,
	"N4": true,
	"N5": true,
	"N6": true, "N6.1": true, "N6.2": true, "N6.3": true, "N6.4": true, "N6.5": true, "N6.6": true, "N6.7": true, "N6.8": true, "N6.9": true,
	"N7": true,
}

// Valid regime fiscale codes per FatturaPA v1.2.3 (including RF20 from v1.9)
var validRegimeFiscale = map[string]bool{
	"RF01": true, "RF02": true,
	// RF03 not defined
	"RF04": true, "RF05": true, "RF06": true, "RF07": true, "RF08": true,
	"RF09": true, "RF10": true, "RF11": true, "RF12": true, "RF13": true,
	"RF14": true, "RF15": true, "RF16": true, "RF17": true, "RF18": true,
	"RF19": true, "RF20": true,
}

// Valid tipo ritenuta codes per FatturaPA v1.2.3
var validTipoRitenuta = map[string]bool{
	"RT01": true, // Ritenuta persone fisiche
	"RT02": true, // Ritenuta persone giuridiche
	"RT03": true, // Contributo INPS
	"RT04": true, // Contributo ENASARCO
	"RT05": true, // Contributo ENPAM
	"RT06": true, // Altro contributo previdenziale
}

// Valid tipo cassa codes per FatturaPA v1.2.3
var validTipoCassa = map[string]bool{
	"TC01": true, "TC02": true, "TC03": true, "TC04": true, "TC05": true,
	"TC06": true, "TC07": true, "TC08": true, "TC09": true, "TC10": true,
	"TC11": true, "TC12": true, "TC13": true, "TC14": true, "TC15": true,
	"TC16": true, "TC17": true, "TC18": true, "TC19": true, "TC20": true,
	"TC21": true, "TC22": true,
}

// ValidateCodiceDestinatario validates recipient code format
// Pattern: [A-Z0-9]{6,7}
func ValidateCodiceDestinatario(code string) error {
	if code == "" {
		return nil // Optional field
	}
	upper := strings.ToUpper(code)
	if !regexCodiceDestinatario.MatchString(upper) {
		return fmt.Errorf("%w: got '%s'", ErrInvalidCodiceDestinatario, code)
	}
	return nil
}

// ValidateCAP validates Italian postal code format
// Pattern: [0-9]{5}
func ValidateCAP(cap string) error {
	if cap == "" {
		return nil // Optional field
	}
	if !regexCAP.MatchString(cap) {
		return fmt.Errorf("%w: got '%s'", ErrInvalidCAP, cap)
	}
	return nil
}

// ValidateProvincia validates Italian province code format
// Pattern: [A-Z]{2}
func ValidateProvincia(prov string) error {
	if prov == "" {
		return nil // Optional field
	}
	upper := strings.ToUpper(prov)
	if !regexProvincia.MatchString(upper) {
		return fmt.Errorf("%w: got '%s'", ErrInvalidProvincia, prov)
	}
	return nil
}

// ValidateNazione validates country code format (ISO 3166-1 alpha-2)
// Pattern: [A-Z]{2}
func ValidateNazione(nation string) error {
	if nation == "" {
		return nil // Will default to IT
	}
	upper := strings.ToUpper(nation)
	if !regexNazione.MatchString(upper) {
		return fmt.Errorf("%w: got '%s'", ErrInvalidNazione, nation)
	}
	return nil
}

// ValidateCodiceFiscale validates Italian fiscal code format
// Pattern: [A-Z0-9]{11,16}
func ValidateCodiceFiscale(cf string) error {
	if cf == "" {
		return nil // Optional field
	}
	upper := strings.ToUpper(cf)
	if !regexCodiceFiscale.MatchString(upper) {
		return fmt.Errorf("%w: got '%s'", ErrInvalidCodiceFiscale, cf)
	}
	return nil
}

// ValidateCodiceFiscaleRequired validates that CodiceFiscale is present and valid
// Used for CedentePrestatore where CodiceFiscale is required per D.P.R. 605-1973
func ValidateCodiceFiscaleRequired(cf string, partyType string) error {
	if cf == "" {
		return fmt.Errorf("%s: codice fiscale is required (D.P.R. 605-1973)", partyType)
	}
	upper := strings.ToUpper(cf)
	if !regexCodiceFiscale.MatchString(upper) {
		return fmt.Errorf("%s: %w: got '%s'", partyType, ErrInvalidCodiceFiscale, cf)
	}
	return nil
}

// ValidatePartitaIVA validates Italian VAT number format
// Pattern: [0-9]{11}
func ValidatePartitaIVA(piva string) error {
	if piva == "" {
		return nil // Optional field
	}
	if !regexPartitaIVA.MatchString(piva) {
		return fmt.Errorf("%w: got '%s'", ErrInvalidPartitaIVA, piva)
	}
	return nil
}

// ValidateIBAN validates IBAN format
// Pattern: [a-zA-Z]{2}[0-9]{2}[a-zA-Z0-9]{11,30}
func ValidateIBAN(iban string) error {
	if iban == "" {
		return nil // Optional field
	}
	upper := strings.ToUpper(strings.ReplaceAll(iban, " ", ""))
	if !regexIBAN.MatchString(upper) {
		return fmt.Errorf("%w: got '%s'", ErrInvalidIBAN, iban)
	}
	return nil
}

// ValidateBIC validates BIC/SWIFT code format
// Pattern: [A-Z]{6}[A-Z2-9][A-NP-Z0-9]([A-Z0-9]{3})?
func ValidateBIC(bic string) error {
	if bic == "" {
		return nil // Optional field
	}
	upper := strings.ToUpper(bic)
	if !regexBIC.MatchString(upper) {
		return fmt.Errorf("%w: got '%s'", ErrInvalidBIC, bic)
	}
	return nil
}

// ValidateProgressivoInvio validates progressive send number format
// Pattern: 1-10 alphanumeric characters
func ValidateProgressivoInvio(prog string) error {
	if prog == "" {
		return fmt.Errorf("%w: cannot be empty", ErrInvalidProgressivoInvio)
	}
	if !regexProgressivoInvio.MatchString(prog) {
		return fmt.Errorf("%w: got '%s'", ErrInvalidProgressivoInvio, prog)
	}
	return nil
}

// ValidateABI validates ABI code format
// Pattern: [0-9]{5}
func ValidateABI(abi string) error {
	if abi == "" {
		return nil // Optional field
	}
	if !regexABI.MatchString(abi) {
		return fmt.Errorf("%w: got '%s'", ErrInvalidABI, abi)
	}
	return nil
}

// ValidateCABCode validates CAB code format (named differently to avoid conflict with ValidateCAP)
// Pattern: [0-9]{5}
func ValidateCABCode(cab string) error {
	if cab == "" {
		return nil // Optional field
	}
	if !regexCAB.MatchString(cab) {
		return fmt.Errorf("%w: got '%s'", ErrInvalidCAB, cab)
	}
	return nil
}

// ValidateDocumentType validates FatturaPA document type
func ValidateDocumentType(dt string) error {
	if dt == "" {
		return fmt.Errorf("%w: cannot be empty", ErrInvalidDocumentType)
	}
	if !validDocumentTypes[dt] {
		return fmt.Errorf("%w: got '%s'", ErrInvalidDocumentType, dt)
	}
	return nil
}

// ValidatePaymentCondition validates FatturaPA payment condition
func ValidatePaymentCondition(cond string) error {
	if cond == "" {
		return nil // Optional field
	}
	if !validPaymentConditions[cond] {
		return fmt.Errorf("%w: got '%s'", ErrInvalidPaymentCondition, cond)
	}
	return nil
}

// ValidatePaymentMethod validates FatturaPA payment method
func ValidatePaymentMethod(method string) error {
	if method == "" {
		return nil // Optional field
	}
	if !validPaymentMethods[method] {
		return fmt.Errorf("%w: got '%s'", ErrInvalidPaymentMethod, method)
	}
	return nil
}

// ValidateVATNature validates FatturaPA VAT nature code
func ValidateVATNature(natura string) error {
	if natura == "" {
		return nil // Optional field (only required when VAT rate is 0)
	}
	if !validVATNatures[natura] {
		return fmt.Errorf("%w: got '%s'", ErrInvalidVATNature, natura)
	}
	return nil
}

// ValidateRegimeFiscale validates FatturaPA fiscal regime code
func ValidateRegimeFiscale(regime string) error {
	if regime == "" {
		return fmt.Errorf("%w: cannot be empty", ErrInvalidRegimeFiscale)
	}
	if !validRegimeFiscale[regime] {
		return fmt.Errorf("%w: got '%s'", ErrInvalidRegimeFiscale, regime)
	}
	return nil
}

// ValidateTipoRitenuta validates withholding tax type
func ValidateTipoRitenuta(tipo string) error {
	if tipo == "" {
		return fmt.Errorf("%w: cannot be empty", ErrInvalidTipoRitenuta)
	}
	if !validTipoRitenuta[tipo] {
		return fmt.Errorf("%w: got '%s'", ErrInvalidTipoRitenuta, tipo)
	}
	return nil
}

// ValidateTipoCassa validates social security fund type
func ValidateTipoCassa(tipo string) error {
	if tipo == "" {
		return fmt.Errorf("%w: cannot be empty", ErrInvalidTipoCassa)
	}
	if !validTipoCassa[tipo] {
		return fmt.Errorf("%w: got '%s'", ErrInvalidTipoCassa, tipo)
	}
	return nil
}

// InvoiceValidationError collects multiple validation errors
type InvoiceValidationError struct {
	Errors []error
}

func (e *InvoiceValidationError) Error() string {
	if len(e.Errors) == 0 {
		return "no validation errors"
	}
	var msgs []string
	for _, err := range e.Errors {
		msgs = append(msgs, err.Error())
	}
	return fmt.Sprintf("validation errors: %s", strings.Join(msgs, "; "))
}

func (e *InvoiceValidationError) Add(err error) {
	if err != nil {
		e.Errors = append(e.Errors, err)
	}
}

func (e *InvoiceValidationError) HasErrors() bool {
	return len(e.Errors) > 0
}

// ValidateInvoiceForXML performs comprehensive validation of an invoice before XML generation
func ValidateInvoiceForXML(invoice *models.Invoice) error {
	if invoice == nil {
		return fmt.Errorf("invoice cannot be nil")
	}

	errs := &InvoiceValidationError{}

	// Validate document type
	errs.Add(ValidateDocumentType(string(invoice.DocumentType)))

	// Validate progressivo invio
	errs.Add(ValidateProgressivoInvio(invoice.ProgressivoInvio))

	// Validate cedente prestatore (seller)
	if invoice.CedentePrestatore != nil {
		errs.Add(validateParty(invoice.CedentePrestatore, "cedente_prestatore"))
		// Require CodiceFiscale for CedentePrestatore (Italian regulation D.P.R. 605-1973)
		// For Italian companies, CodiceFiscale can default to FiscalIDCode (P.IVA)
		cedenteCodiceFiscale := invoice.CedentePrestatore.CodiceFiscale
		if cedenteCodiceFiscale == "" && invoice.CedentePrestatore.FiscalIDCountry == "IT" {
			cedenteCodiceFiscale = invoice.CedentePrestatore.FiscalIDCode
		}
		errs.Add(ValidateCodiceFiscaleRequired(cedenteCodiceFiscale, "cedente_prestatore"))
	}

	// Validate cessionario committente (buyer)
	if invoice.CessionarioCommittente != nil {
		errs.Add(validateParty(invoice.CessionarioCommittente, "cessionario_committente"))
		errs.Add(ValidateCodiceDestinatario(invoice.CessionarioCommittente.CodiceDestinatario))
	}

	// Validate payment terms
	if invoice.PaymentTerms != nil {
		errs.Add(ValidatePaymentCondition(string(invoice.PaymentTerms.Condition)))
		errs.Add(ValidatePaymentMethod(string(invoice.PaymentTerms.PaymentMethod)))
		errs.Add(ValidateIBAN(invoice.PaymentTerms.IBAN))
		errs.Add(ValidateBIC(invoice.PaymentTerms.BIC))
		errs.Add(ValidateABI(invoice.PaymentTerms.ABI))
		errs.Add(ValidateCABCode(invoice.PaymentTerms.CAB))
	}

	// Validate line items
	for i, line := range invoice.Lines {
		if line.VATRate == 0 && line.VATNature != "" {
			if err := ValidateVATNature(string(line.VATNature)); err != nil {
				errs.Add(fmt.Errorf("line %d: %w", i+1, err))
			}
		}
	}

	// Validate VAT summary
	for i, vs := range invoice.VATSummary {
		if vs.VATRate == 0 && vs.VATNature != "" {
			if err := ValidateVATNature(string(vs.VATNature)); err != nil {
				errs.Add(fmt.Errorf("VAT summary %d: %w", i+1, err))
			}
		}
	}

	// Validate withholding tax if present
	for i, dr := range invoice.DatiRitenuta {
		if err := ValidateTipoRitenuta(dr.TipoRitenuta); err != nil {
			errs.Add(fmt.Errorf("dati_ritenuta %d: %w", i+1, err))
		}
	}

	// Validate social security fund if present
	for i, dc := range invoice.DatiCassaPrevidenziale {
		if err := ValidateTipoCassa(dc.TipoCassa); err != nil {
			errs.Add(fmt.Errorf("dati_cassa %d: %w", i+1, err))
		}
		if dc.AliquotaIVA == 0 && dc.Natura != "" {
			if err := ValidateVATNature(dc.Natura); err != nil {
				errs.Add(fmt.Errorf("dati_cassa %d natura: %w", i+1, err))
			}
		}
	}

	if errs.HasErrors() {
		return errs
	}
	return nil
}

// validateParty validates party data (cedente/cessionario)
func validateParty(party *models.PartyData, prefix string) error {
	if party == nil {
		return nil
	}

	errs := &InvoiceValidationError{}

	// Validate regime fiscale (required for cedente)
	if party.RegimeFiscale != "" {
		errs.Add(ValidateRegimeFiscale(string(party.RegimeFiscale)))
	}

	// Validate address fields
	errs.Add(ValidateCAP(party.PostalCode))
	errs.Add(ValidateProvincia(party.Province))
	errs.Add(ValidateNazione(party.Country))

	// Validate fiscal ID country (IdPaese) - SDI error 00300 if invalid
	if err := ValidateNazione(party.FiscalIDCountry); err != nil {
		errs.Add(fmt.Errorf("fiscal ID country (IdPaese): %w", err))
	}

	// Validate codice fiscale
	errs.Add(ValidateCodiceFiscale(party.CodiceFiscale))

	// Validate partita IVA (the numeric part)
	if party.FiscalIDCode != "" {
		errs.Add(ValidatePartitaIVA(party.FiscalIDCode))
	}

	if errs.HasErrors() {
		// Prefix all errors with party type
		prefixedErrs := &InvoiceValidationError{}
		for _, err := range errs.Errors {
			prefixedErrs.Add(fmt.Errorf("%s: %w", prefix, err))
		}
		return prefixedErrs
	}
	return nil
}

// NormalizeCAP ensures CAP is 5 digits, padding with leading zeros if needed
func NormalizeCAP(cap string) string {
	if cap == "" {
		return ""
	}
	// Remove any spaces
	cap = strings.TrimSpace(cap)
	// Pad with leading zeros if needed
	for len(cap) < 5 {
		cap = "0" + cap
	}
	// Truncate if too long (shouldn't happen with valid input)
	if len(cap) > 5 {
		cap = cap[:5]
	}
	return cap
}

// NormalizeProvincia ensures provincia is uppercase 2 letters
func NormalizeProvincia(prov string) string {
	if prov == "" {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(prov))
}

// NormalizeNazione ensures nazione is uppercase 2 letters, defaults to IT
func NormalizeNazione(nation string) string {
	if nation == "" {
		return "IT"
	}
	return strings.ToUpper(strings.TrimSpace(nation))
}

// NormalizeIBAN removes spaces and converts to uppercase
func NormalizeIBAN(iban string) string {
	if iban == "" {
		return ""
	}
	return strings.ToUpper(strings.ReplaceAll(iban, " ", ""))
}

// NormalizeBIC converts to uppercase
func NormalizeBIC(bic string) string {
	if bic == "" {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(bic))
}

// NormalizeCodiceDestinatario ensures uppercase
func NormalizeCodiceDestinatario(code string) string {
	if code == "" {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(code))
}

// NormalizePhone normalizes phone numbers for FatturaPA (5-12 chars max)
// Removes country code prefix (+39, 0039) and non-digit characters
func NormalizePhone(phone string) string {
	if phone == "" {
		return ""
	}

	// Remove spaces, dashes, parentheses
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, "-", "")
	phone = strings.ReplaceAll(phone, "(", "")
	phone = strings.ReplaceAll(phone, ")", "")

	// Remove Italian country code prefix
	phone = strings.TrimPrefix(phone, "+39")
	phone = strings.TrimPrefix(phone, "0039")
	phone = strings.TrimPrefix(phone, "+")

	// Truncate to max 12 characters (FatturaPA XSD limit)
	if len(phone) > 12 {
		phone = phone[:12]
	}

	return phone
}

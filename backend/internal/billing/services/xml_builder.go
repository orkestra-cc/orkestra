package services

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/billing/config"
	"github.com/orkestra/backend/internal/billing/models"
)

// XMLBuilder builds FatturaPA XML documents
type XMLBuilder interface {
	Build(invoice *models.Invoice) (string, error)
}

type xmlBuilder struct {
	config *config.OpenAPIConfig
}

// NewXMLBuilder creates a new XML builder
func NewXMLBuilder(cfg *config.OpenAPIConfig) XMLBuilder {
	return &xmlBuilder{
		config: cfg,
	}
}

func (b *xmlBuilder) Build(invoice *models.Invoice) (string, error) {
	// Validate invoice before building XML
	if err := ValidateInvoiceForXML(invoice); err != nil {
		return "", fmt.Errorf("invoice validation failed: %w", err)
	}

	// Determine format based on recipient type
	format := models.FormatFPR12 // Private sector
	if invoice.CessionarioCommittente != nil && len(invoice.CessionarioCommittente.CodiceDestinatario) == 6 {
		format = models.FormatFPA12 // Public Administration
	}

	fatturaPA := models.NewFatturaElettronica(format)

	// Set SistemaEmittente (issuing system identifier, max 10 chars)
	fatturaPA.SistemaEmittente = "ORKESTRA"

	// Build header
	fatturaPA.FatturaElettronicaHeader = b.buildHeader(invoice, format)

	// Build body
	fatturaPA.FatturaElettronicaBody = []models.FatturaElettronicaBody{
		b.buildBody(invoice),
	}

	// Marshal to XML
	output, err := xml.MarshalIndent(fatturaPA, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal XML: %w", err)
	}

	// Add XML declaration
	xmlContent := xml.Header + string(output)

	return xmlContent, nil
}

func (b *xmlBuilder) buildHeader(invoice *models.Invoice, format models.TransmissionFormat) models.FatturaElettronicaHeader {
	header := models.FatturaElettronicaHeader{
		DatiTrasmissione:       b.buildDatiTrasmissione(invoice, format),
		CedentePrestatore:      b.buildCedentePrestatore(invoice.CedentePrestatore),
		CessionarioCommittente: b.buildCessionarioCommittente(invoice.CessionarioCommittente),
	}

	return header
}

func (b *xmlBuilder) buildDatiTrasmissione(invoice *models.Invoice, format models.TransmissionFormat) models.DatiTrasmissione {
	// Use transmitter's fiscal ID from the company (CedentePrestatore)
	// This is the seller/provider company's fiscal ID
	idPaese := "IT"
	idCodice := ""

	// First try to get from invoice's CedentePrestatore (company data)
	if invoice.CedentePrestatore != nil && invoice.CedentePrestatore.FiscalIDCode != "" {
		idPaese = NormalizeNazione(invoice.CedentePrestatore.FiscalIDCountry)
		// IdTrasmittente requires CodiceFiscale, with fallback to P.IVA for Italian companies
		idCodice = strings.TrimSpace(invoice.CedentePrestatore.CodiceFiscale)
		if idCodice == "" && idPaese == "IT" {
			idCodice = strings.TrimSpace(invoice.CedentePrestatore.FiscalIDCode)
		}
	} else if b.config.FiscalID != "" {
		// Fallback to config (for backwards compatibility)
		fiscalID := strings.TrimSpace(b.config.FiscalID)
		idCodice = fiscalID
		if len(fiscalID) >= 2 {
			// Check if first 2 chars are letters (country code prefix like "IT")
			if isAlpha(fiscalID[:2]) {
				idPaese = strings.ToUpper(fiscalID[:2])
				idCodice = strings.TrimSpace(fiscalID[2:])
			}
		}
	}

	idTrasmittente := models.IdFiscale{
		IdPaese:  idPaese,
		IdCodice: idCodice,
	}

	// Determine recipient code (normalize to uppercase)
	codiceDestinatario := NormalizeCodiceDestinatario(b.config.RecipientCode) // Default to our OpenAPI recipient code
	pecDestinatario := ""

	if invoice.CessionarioCommittente != nil {
		if invoice.CessionarioCommittente.CodiceDestinatario != "" {
			codiceDestinatario = NormalizeCodiceDestinatario(invoice.CessionarioCommittente.CodiceDestinatario)
		} else if invoice.CessionarioCommittente.PECDestinatario != "" {
			codiceDestinatario = "0000000" // When using PEC, code is 7 zeros
			pecDestinatario = invoice.CessionarioCommittente.PECDestinatario
		}
	}

	dt := models.DatiTrasmissione{
		IdTrasmittente:      idTrasmittente,
		ProgressivoInvio:    invoice.ProgressivoInvio,
		FormatoTrasmissione: format,
		CodiceDestinatario:  codiceDestinatario,
	}

	if pecDestinatario != "" {
		dt.PECDestinatario = pecDestinatario
	}

	// Add transmitter contacts for communication channel (recommended per FatturaPA spec)
	if invoice.CedentePrestatore != nil {
		if invoice.CedentePrestatore.Email != "" || invoice.CedentePrestatore.Phone != "" {
			dt.ContattiTrasmit = &models.ContattiTrasmit{
				Email:    invoice.CedentePrestatore.Email,
				Telefono: NormalizePhone(invoice.CedentePrestatore.Phone),
			}
		}
	}

	return dt
}

func (b *xmlBuilder) buildCedentePrestatore(party *models.PartyData) models.CedentePrestatore {
	if party == nil {
		return models.CedentePrestatore{}
	}

	anagrafica := models.Anagrafica{}
	if party.IsCompany {
		anagrafica.Denominazione = party.Denomination
	} else {
		anagrafica.Nome = party.Name
		anagrafica.Cognome = party.Surname
	}

	// Ensure IdPaese is uppercase 2-letter country code
	idPaese := NormalizeNazione(party.FiscalIDCountry)

	// Validate RegimeFiscale - must be a valid code (RF01-RF19, RF20)
	regimeFiscale := party.RegimeFiscale
	if regimeFiscale == "" {
		regimeFiscale = models.RegimeOrdinario // Default to RF01
	}

	// Normalize address fields per XSD requirements
	nazione := NormalizeNazione(party.Country)
	cap := NormalizeCAP(party.PostalCode)
	provincia := NormalizeProvincia(party.Province)

	// For Italian companies, CodiceFiscale defaults to FiscalIDCode (P.IVA)
	// This is valid because for Italian companies, the Codice Fiscale is often
	// identical to the P.IVA (11 digits) per D.P.R. 605-1973
	codiceFiscale := party.CodiceFiscale
	if codiceFiscale == "" && idPaese == "IT" {
		codiceFiscale = party.FiscalIDCode
	}

	cp := models.CedentePrestatore{
		DatiAnagrafici: models.DatiAnagraficiCedente{
			IdFiscaleIVA: models.IdFiscale{
				IdPaese:  idPaese,
				IdCodice: party.FiscalIDCode,
			},
			CodiceFiscale: codiceFiscale,
			Anagrafica:    anagrafica,
			RegimeFiscale: regimeFiscale,
		},
		Sede: models.Indirizzo{
			Indirizzo:    party.Address,
			NumeroCivico: party.NumeroCivico, // Street number (separate per XSD)
			CAP:          cap,
			Comune:       party.City,
			Provincia:    provincia,
			Nazione:      nazione,
		},
	}

	// Add IscrizioneREA only if ALL required fields are present (per Article 2250 Civil Code)
	// If any required field is empty, omit the entire element to avoid SDI validation errors
	if party.IscrizioneREA != nil &&
		party.IscrizioneREA.Ufficio != "" &&
		party.IscrizioneREA.NumeroREA != "" &&
		party.IscrizioneREA.StatoLiquidazione != "" {
		cp.IscrizioneREA = &models.IscrizioneREA{
			Ufficio:           NormalizeProvincia(party.IscrizioneREA.Ufficio),
			NumeroREA:         party.IscrizioneREA.NumeroREA,
			StatoLiquidazione: party.IscrizioneREA.StatoLiquidazione,
		}
		// Add optional fields
		if party.IscrizioneREA.CapitaleSociale > 0 {
			cp.IscrizioneREA.CapitaleSociale = formatAmount(party.IscrizioneREA.CapitaleSociale)
		}
		if party.IscrizioneREA.SocioUnico != "" {
			cp.IscrizioneREA.SocioUnico = party.IscrizioneREA.SocioUnico
		}
	}

	// Add contacts if available
	if party.Phone != "" || party.Email != "" {
		cp.Contatti = &models.Contatti{
			Telefono: NormalizePhone(party.Phone),
			Email:    party.Email,
		}
	}

	return cp
}

func (b *xmlBuilder) buildCessionarioCommittente(party *models.PartyData) models.CessionarioCommittente {
	if party == nil {
		return models.CessionarioCommittente{}
	}

	anagrafica := models.Anagrafica{}
	if party.IsCompany {
		anagrafica.Denominazione = party.Denomination
	} else {
		anagrafica.Nome = party.Name
		anagrafica.Cognome = party.Surname
	}

	// Normalize address fields per XSD requirements
	nazione := NormalizeNazione(party.Country)
	cap := NormalizeCAP(party.PostalCode)
	provincia := NormalizeProvincia(party.Province)

	cc := models.CessionarioCommittente{
		DatiAnagrafici: models.DatiAnagraficiCessionario{
			Anagrafica: anagrafica,
		},
		Sede: models.Indirizzo{
			Indirizzo:    party.Address,
			NumeroCivico: party.NumeroCivico, // Street number (separate per XSD)
			CAP:          cap,
			Comune:       party.City,
			Provincia:    provincia,
			Nazione:      nazione,
		},
	}

	// Add fiscal ID if available
	if party.FiscalIDCode != "" {
		idPaese := NormalizeNazione(party.FiscalIDCountry)
		cc.DatiAnagrafici.IdFiscaleIVA = &models.IdFiscale{
			IdPaese:  idPaese,
			IdCodice: party.FiscalIDCode,
		}
	}

	// Add codice fiscale if different
	if party.CodiceFiscale != "" {
		cc.DatiAnagrafici.CodiceFiscale = party.CodiceFiscale
	}

	return cc
}

func (b *xmlBuilder) buildBody(invoice *models.Invoice) models.FatturaElettronicaBody {
	body := models.FatturaElettronicaBody{
		DatiGenerali:    b.buildDatiGenerali(invoice),
		DatiBeniServizi: b.buildDatiBeniServizi(invoice),
	}

	// Add payment data if present (skip for credit notes TD04)
	if invoice.PaymentTerms != nil && invoice.DocumentType != models.DocTypeNotaCredito {
		body.DatiPagamento = b.buildDatiPagamento(invoice)
	}

	// Add attachments if present
	if len(invoice.Attachments) > 0 {
		body.Allegati = b.buildAllegati(invoice.Attachments)
	}

	return body
}

func (b *xmlBuilder) buildDatiGenerali(invoice *models.Invoice) models.DatiGenerali {
	dg := models.DatiGenerali{
		DatiGeneraliDocumento: models.DatiGeneraliDocumento{
			TipoDocumento:          invoice.DocumentType,
			Divisa:                 invoice.Currency,
			Data:                   invoice.Date.Format("2006-01-02"),
			Numero:                 invoice.Number,
			ImportoTotaleDocumento: formatAmount(invoice.TotalAmount),
		},
	}

	// Add DatiRitenuta (withholding tax) if present
	for _, dr := range invoice.DatiRitenuta {
		datiRitenuta := &models.DatiRitenuta{
			TipoRitenuta:     dr.TipoRitenuta,
			ImportoRitenuta:  formatAmount(dr.ImportoRitenuta),
			AliquotaRitenuta: formatAmount(dr.AliquotaRitenuta),
		}
		if dr.CausalePagamento != "" {
			datiRitenuta.CausalePagamento = dr.CausalePagamento
		}
		dg.DatiGeneraliDocumento.DatiRitenuta = datiRitenuta
		break // XSD only allows one DatiRitenuta in v1.2.3 (but our model supports multiple for future)
	}

	// Add DatiBollo (stamp duty) - either explicitly set or auto-detected per DPR 642/1972
	if invoice.DatiBollo != nil {
		// Explicitly set stamp duty
		dg.DatiGeneraliDocumento.DatiBollo = &models.DatiBollo{
			BolloVirtuale: "SI", // Always "SI" for virtual stamp duty
		}
		if invoice.DatiBollo.ImportoBollo > 0 {
			dg.DatiGeneraliDocumento.DatiBollo.ImportoBollo = formatAmount(invoice.DatiBollo.ImportoBollo)
		} else {
			dg.DatiGeneraliDocumento.DatiBollo.ImportoBollo = "2.00" // Default €2.00
		}
	} else if shouldApplyStampDuty(invoice) {
		// Auto-detect stamp duty requirement per Italian law
		dg.DatiGeneraliDocumento.DatiBollo = &models.DatiBollo{
			BolloVirtuale: "SI",
			ImportoBollo:  "2.00", // Standard €2.00 stamp duty
		}
	}

	// Add DatiCassaPrevidenziale (social security fund) if present
	for _, dc := range invoice.DatiCassaPrevidenziale {
		datiCassa := models.DatiCassa{
			TipoCassa:              dc.TipoCassa,
			AlCassa:                formatAmount(dc.AlCassa),
			ImportoContributoCassa: formatAmount(dc.ImportoContributoCassa),
			AliquotaIVA:            formatAmount(dc.AliquotaIVA),
		}
		if dc.ImponibileCassa > 0 {
			datiCassa.ImponibileCassa = formatAmount(dc.ImponibileCassa)
		}
		if dc.Ritenuta {
			datiCassa.Ritenuta = "SI"
		}
		if dc.Natura != "" {
			datiCassa.Natura = dc.Natura
		}
		if dc.RiferimentoAmm != "" {
			datiCassa.RiferimentoAmm = dc.RiferimentoAmm
		}
		dg.DatiGeneraliDocumento.DatiCassaPrevidenziale = append(dg.DatiGeneraliDocumento.DatiCassaPrevidenziale, datiCassa)
	}

	// Add rounding if present
	if invoice.Rounding != 0 {
		dg.DatiGeneraliDocumento.Arrotondamento = formatAmount(invoice.Rounding)
	}

	// Add causale (description)
	if len(invoice.Causale) > 0 {
		dg.DatiGeneraliDocumento.Causale = invoice.Causale
	}

	// Add related documents
	for _, rd := range invoice.RelatedDocuments {
		docCorr := models.DatiDocumentoCorrelato{
			IdDocumento: rd.Number,
		}
		if rd.Date != nil {
			docCorr.Data = rd.Date.Format("2006-01-02")
		}
		if rd.CIG != "" {
			docCorr.CodiceCIG = rd.CIG
		}
		if rd.CUP != "" {
			docCorr.CodiceCUP = rd.CUP
		}

		switch rd.Type {
		case "ordine":
			dg.DatiOrdineAcquisto = append(dg.DatiOrdineAcquisto, docCorr)
		case "contratto":
			dg.DatiContratto = append(dg.DatiContratto, docCorr)
		case "convenzione":
			dg.DatiConvenzione = append(dg.DatiConvenzione, docCorr)
		case "ricezione":
			dg.DatiRicezione = append(dg.DatiRicezione, docCorr)
		case "fattura":
			dg.DatiFattureCollegate = append(dg.DatiFattureCollegate, docCorr)
		}
	}

	return dg
}

func (b *xmlBuilder) buildDatiBeniServizi(invoice *models.Invoice) models.DatiBeniServizi {
	dbs := models.DatiBeniServizi{
		DettaglioLinee: make([]models.DettaglioLinea, 0, len(invoice.Lines)),
		DatiRiepilogo:  make([]models.DatiRiepilogo, 0, len(invoice.VATSummary)),
	}

	// Build line details
	for i, line := range invoice.Lines {
		dl := models.DettaglioLinea{
			NumeroLinea:    i + 1,
			Descrizione:    truncateString(line.Description, 1000),
			PrezzoUnitario: formatAmount(line.UnitPrice),
			PrezzoTotale:   formatAmount(line.TotalPrice),
			AliquotaIVA:    formatAmount(line.VATRate),
		}

		// Add quantity if present
		if line.Quantity > 0 {
			dl.Quantita = formatQuantity(line.Quantity)
		}

		// Add unit of measure if present
		if line.UnitOfMeasure != "" {
			dl.UnitaMisura = string(line.UnitOfMeasure)
		}

		// Add natura IVA if rate is 0
		if line.VATRate == 0 && line.VATNature != "" {
			dl.Natura = string(line.VATNature)
		}

		// Add period dates if present
		if line.StartDate != nil {
			dl.DataInizioPeriodo = line.StartDate.Format("2006-01-02")
		}
		if line.EndDate != nil {
			dl.DataFinePeriodo = line.EndDate.Format("2006-01-02")
		}

		// Add Ritenuta flag if applicable
		if line.Ritenuta {
			dl.Ritenuta = "SI"
		}

		// Add administrative reference if present
		if line.AdministrativeRef != "" {
			dl.RiferimentoAmm = line.AdministrativeRef
		}

		// Add product codes (support multiple codes per XSD unbounded)
		if len(line.CodiciArticolo) > 0 {
			// Use the new multiple codes structure
			for _, code := range line.CodiciArticolo {
				dl.CodiceArticolo = append(dl.CodiceArticolo, models.CodArticolo{
					CodiceTipo:   code.CodiceTipo,
					CodiceValore: code.CodiceValore,
				})
			}
		} else if line.ProductCode != "" {
			// Fallback to legacy single product code
			dl.CodiceArticolo = []models.CodArticolo{
				{
					CodiceTipo:   "INTERNO",
					CodiceValore: line.ProductCode,
				},
			}
		}

		// Add discounts/markups
		for _, discount := range line.Discounts {
			sm := models.ScontoMagg{
				Tipo: discount.Type,
			}
			if discount.Percentage > 0 {
				sm.Percentuale = formatAmount(discount.Percentage)
			}
			if discount.Amount > 0 {
				sm.Importo = formatAmount(discount.Amount)
			}
			dl.ScontoMaggiorazione = append(dl.ScontoMaggiorazione, sm)
		}

		// Add AltriDatiGestionali (additional management data) if present
		for _, adg := range line.AltriDatiGestionali {
			altriDati := models.AltriDati{
				TipoDato: adg.TipoDato,
			}
			if adg.RiferimentoTesto != "" {
				altriDati.RiferimentoTesto = adg.RiferimentoTesto
			}
			if adg.RiferimentoNumero != 0 {
				altriDati.RiferimentoNumero = formatAmount(adg.RiferimentoNumero)
			}
			if adg.RiferimentoData != nil {
				altriDati.RiferimentoData = adg.RiferimentoData.Format("2006-01-02")
			}
			dl.AltriDatiGestionali = append(dl.AltriDatiGestionali, altriDati)
		}

		dbs.DettaglioLinee = append(dbs.DettaglioLinee, dl)
	}

	// Build VAT summary
	for _, vs := range invoice.VATSummary {
		dr := models.DatiRiepilogo{
			AliquotaIVA:       formatAmount(vs.VATRate),
			ImponibileImporto: formatAmount(vs.TaxableAmount),
			Imposta:           formatAmount(vs.VATAmount),
		}

		// Add natura if rate is 0
		if vs.VATRate == 0 && vs.VATNature != "" {
			dr.Natura = string(vs.VATNature)
		}

		// Add esigibilità if present
		if vs.VATExigibility != "" {
			dr.EsigibilitaIVA = vs.VATExigibility
		}

		// Add riferimento normativo
		// Priority: explicit NormativeRef > automatic for RF19 Forfettario
		if vs.NormativeRef != "" {
			dr.RiferimentoNormativo = vs.NormativeRef
		} else if invoice.CedentePrestatore != nil &&
			invoice.CedentePrestatore.RegimeFiscale == models.RegimeForfettario {
			dr.RiferimentoNormativo = "Non soggetta art. 1/54-89 L. 190/2014 e succ. modifiche/integrazioni"
		}

		dbs.DatiRiepilogo = append(dbs.DatiRiepilogo, dr)
	}

	return dbs
}

func (b *xmlBuilder) buildDatiPagamento(invoice *models.Invoice) *models.DatiPagamento {
	if invoice.PaymentTerms == nil {
		return nil
	}

	pt := invoice.PaymentTerms

	dp := &models.DatiPagamento{
		CondizioniPagamento: string(pt.Condition),
		DettaglioPagamento:  []models.DettaglioPagamento{},
	}

	// Helper function to populate payment detail common fields
	populatePaymentDetail := func(detail *models.DettaglioPagamento) {
		if pt.Beneficiario != "" {
			detail.Beneficiario = pt.Beneficiario
		}
		if pt.IstitutoFinanziario != "" {
			detail.IstitutoFinanziario = pt.IstitutoFinanziario
		}
		if pt.IBAN != "" {
			detail.IBAN = NormalizeIBAN(pt.IBAN)
		}
		if pt.ABI != "" {
			detail.ABI = pt.ABI
		}
		if pt.CAB != "" {
			detail.CAB = pt.CAB
		}
		if pt.BIC != "" {
			detail.BIC = NormalizeBIC(pt.BIC)
		}
	}

	// For installment payments
	if pt.Condition == models.PaymentConditionRata && len(pt.Installments) > 0 {
		for _, inst := range pt.Installments {
			detail := models.DettaglioPagamento{
				ModalitaPagamento:     string(pt.PaymentMethod),
				DataScadenzaPagamento: inst.DueDate.Format("2006-01-02"),
				ImportoPagamento:      formatAmount(inst.Amount),
			}
			populatePaymentDetail(&detail)
			dp.DettaglioPagamento = append(dp.DettaglioPagamento, detail)
		}
	} else {
		// Single payment
		detail := models.DettaglioPagamento{
			ModalitaPagamento: string(pt.PaymentMethod),
			ImportoPagamento:  formatAmount(invoice.TotalAmount),
		}
		if pt.DueDate != nil {
			detail.DataScadenzaPagamento = pt.DueDate.Format("2006-01-02")
		}
		populatePaymentDetail(&detail)
		dp.DettaglioPagamento = append(dp.DettaglioPagamento, detail)
	}

	return dp
}

func (b *xmlBuilder) buildAllegati(attachments []models.InvoiceAttachment) []models.Allegato {
	allegati := make([]models.Allegato, 0, len(attachments))

	for _, att := range attachments {
		allegato := models.Allegato{
			NomeAttachment:        att.Name,
			DescrizioneAttachment: att.Description,
			FormatoAttachment:     att.Format,
			Attachment:            att.Content, // Base64 encoded
		}
		allegati = append(allegati, allegato)
	}

	return allegati
}

// Helper functions

func formatAmount(amount float64) string {
	// Format with 2 decimal places, using dot as decimal separator
	return strconv.FormatFloat(amount, 'f', 2, 64)
}

func formatQuantity(qty float64) string {
	// FatturaPA requires minimum 2 decimals, maximum 8 decimals for Quantita
	s := strconv.FormatFloat(qty, 'f', 8, 64)
	// Remove trailing zeros but keep at least 2 decimal places
	s = strings.TrimRight(s, "0")
	// Ensure minimum 2 decimal places
	parts := strings.Split(s, ".")
	if len(parts) == 1 {
		// No decimal point, add .00
		return s + ".00"
	}
	if len(parts[1]) < 2 {
		// Less than 2 decimals, pad with zeros
		return s + strings.Repeat("0", 2-len(parts[1]))
	}
	return s
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// isAlpha checks if a string contains only alphabetic characters
func isAlpha(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return len(s) > 0
}

// ensureIdPaese is deprecated - use NormalizeNazione instead
// Keeping for backward compatibility but internally uses NormalizeNazione
func ensureIdPaese(s string) string {
	return NormalizeNazione(s)
}

// shouldApplyStampDuty determines if stamp duty (bollo) should be applied per DPR 642/1972
// Stamp duty of €2.00 applies when:
// 1. Invoice total exceeds €77.47
// 2. Invoice contains exempt/non-taxable amounts (VAT nature codes N1-N7.x)
func shouldApplyStampDuty(invoice *models.Invoice) bool {
	const stampDutyThreshold = 77.47

	// Check total amount threshold
	if invoice.TotalAmount <= stampDutyThreshold {
		return false
	}

	// Check if any VAT summary has exempt/non-taxable nature (N1-N7.x)
	for _, vs := range invoice.VATSummary {
		if vs.VATRate == 0 && vs.VATNature != "" {
			natura := string(vs.VATNature)
			// N1-N7 nature codes indicate exempt/non-taxable operations
			if len(natura) >= 2 && natura[0] == 'N' && natura[1] >= '1' && natura[1] <= '7' {
				return true
			}
		}
	}

	// Also check line items for natura codes
	for _, line := range invoice.Lines {
		if line.VATRate == 0 && line.VATNature != "" {
			natura := string(line.VATNature)
			if len(natura) >= 2 && natura[0] == 'N' && natura[1] >= '1' && natura[1] <= '7' {
				return true
			}
		}
	}

	return false
}


// GenerateProgressivoInvio generates a unique progressivo invio
// Max 10 characters alphanumeric as per FatturaPA spec
func GenerateProgressivoInvio() string {
	// Format: 5 chars from timestamp + 5 chars random = 10 chars max
	now := time.Now()
	// Use last 5 digits of unix timestamp + 5 random digits
	return fmt.Sprintf("%05d%05d", now.Unix()%100000, now.UnixNano()%100000)
}

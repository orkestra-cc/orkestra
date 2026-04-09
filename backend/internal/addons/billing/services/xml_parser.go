package services

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/orkestra/backend/internal/addons/billing/models"
)

// XML parsing errors
var (
	ErrInvalidXML         = errors.New("invalid XML content")
	ErrEmptyXML           = errors.New("XML content is empty")
	ErrMissingInvoiceBody = errors.New("no invoice body found in XML")
	ErrMissingCedente     = errors.New("missing CedentePrestatore (supplier) in XML")
	ErrMissingCessionario = errors.New("missing CessionarioCommittente (buyer) in XML")
)

// XMLParser defines the interface for parsing FatturaPA XML
type XMLParser interface {
	// Parse parses FatturaPA XML content into a FatturaElettronica struct
	Parse(xmlContent []byte) (*models.FatturaElettronica, error)
}

type xmlParser struct{}

// NewXMLParser creates a new XMLParser
func NewXMLParser() XMLParser {
	return &xmlParser{}
}

// Parse parses FatturaPA XML content
func (p *xmlParser) Parse(xmlContent []byte) (*models.FatturaElettronica, error) {
	if len(xmlContent) == 0 {
		return nil, ErrEmptyXML
	}

	// Clean XML content - handle BOM and whitespace
	content := cleanXMLContent(xmlContent)

	// Try to parse with the standard namespace
	fattura, err := p.parseWithNamespace(content)
	if err != nil {
		// Try without namespace (for cases where namespace is different or missing)
		fattura, err = p.parseWithoutNamespace(content)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidXML, err)
		}
	}

	// Validate basic structure
	if err := p.validateStructure(fattura); err != nil {
		return nil, err
	}

	return fattura, nil
}

// parseWithNamespace parses XML with the official FatturaPA namespace
func (p *xmlParser) parseWithNamespace(content []byte) (*models.FatturaElettronica, error) {
	var fattura models.FatturaElettronica
	decoder := xml.NewDecoder(bytes.NewReader(content))
	decoder.CharsetReader = identityCharsetReader

	if err := decoder.Decode(&fattura); err != nil {
		return nil, err
	}

	return &fattura, nil
}

// parseWithoutNamespace parses XML without strict namespace validation
// This handles cases where the namespace might be slightly different or prefixed
func (p *xmlParser) parseWithoutNamespace(content []byte) (*models.FatturaElettronica, error) {
	// Create a struct without the namespace requirement
	type FatturaElettronicaNoNS struct {
		XMLName                  xml.Name                        `xml:"FatturaElettronica"`
		Versione                 models.TransmissionFormat       `xml:"versione,attr"`
		SistemaEmittente         string                          `xml:"SistemaEmittente,attr,omitempty"`
		FatturaElettronicaHeader models.FatturaElettronicaHeader `xml:"FatturaElettronicaHeader"`
		FatturaElettronicaBody   []models.FatturaElettronicaBody `xml:"FatturaElettronicaBody"`
	}

	var fatturaNoNS FatturaElettronicaNoNS
	decoder := xml.NewDecoder(bytes.NewReader(content))
	decoder.CharsetReader = identityCharsetReader

	if err := decoder.Decode(&fatturaNoNS); err != nil {
		return nil, err
	}

	// Convert to proper FatturaElettronica
	fattura := &models.FatturaElettronica{
		Versione:                 fatturaNoNS.Versione,
		SistemaEmittente:         fatturaNoNS.SistemaEmittente,
		FatturaElettronicaHeader: fatturaNoNS.FatturaElettronicaHeader,
		FatturaElettronicaBody:   fatturaNoNS.FatturaElettronicaBody,
	}

	return fattura, nil
}

// validateStructure validates the basic structure of the parsed FatturaPA
func (p *xmlParser) validateStructure(fattura *models.FatturaElettronica) error {
	// Check for invoice bodies
	if len(fattura.FatturaElettronicaBody) == 0 {
		return ErrMissingInvoiceBody
	}

	// Check for cedente/prestatore (supplier)
	if fattura.FatturaElettronicaHeader.CedentePrestatore.DatiAnagrafici.IdFiscaleIVA.IdCodice == "" &&
		fattura.FatturaElettronicaHeader.CedentePrestatore.DatiAnagrafici.CodiceFiscale == "" {
		return ErrMissingCedente
	}

	// Check for cessionario/committente (buyer)
	if fattura.FatturaElettronicaHeader.CessionarioCommittente.DatiAnagrafici.IdFiscaleIVA == nil &&
		fattura.FatturaElettronicaHeader.CessionarioCommittente.DatiAnagrafici.CodiceFiscale == "" {
		return ErrMissingCessionario
	}

	// Validate each invoice body
	for i, body := range fattura.FatturaElettronicaBody {
		if body.DatiGenerali.DatiGeneraliDocumento.Numero == "" {
			return fmt.Errorf("invoice body %d: missing invoice number", i+1)
		}
		if body.DatiGenerali.DatiGeneraliDocumento.Data == "" {
			return fmt.Errorf("invoice body %d: missing invoice date", i+1)
		}
	}

	return nil
}

// cleanXMLContent removes BOM and trims whitespace from XML content
func cleanXMLContent(content []byte) []byte {
	// Remove UTF-8 BOM if present (EF BB BF)
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		content = content[3:]
	}

	// Trim leading/trailing whitespace
	return bytes.TrimSpace(content)
}

// identityCharsetReader is a no-op charset reader that returns the input as-is
// FatturaPA uses UTF-8, so we don't need charset conversion
func identityCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	charset = strings.ToLower(charset)
	if charset == "utf-8" || charset == "utf8" || charset == "" {
		return input, nil
	}
	// For other charsets, still return the input as most FatturaPA are UTF-8
	return input, nil
}

package services

import (
	"encoding/base64"
	"testing"
)

// Sample valid FatturaPA XML for testing
const validFatturaXML = `<?xml version="1.0" encoding="UTF-8"?>
<FatturaElettronica xmlns="http://ivaservizi.agenziaentrate.gov.it/docs/xsd/fatture/v1.2" versione="FPR12">
  <FatturaElettronicaHeader>
    <DatiTrasmissione>
      <IdTrasmittente>
        <IdPaese>IT</IdPaese>
        <IdCodice>01234567890</IdCodice>
      </IdTrasmittente>
      <ProgressivoInvio>00001</ProgressivoInvio>
      <FormatoTrasmissione>FPR12</FormatoTrasmissione>
      <CodiceDestinatario>JKKZDGR</CodiceDestinatario>
    </DatiTrasmissione>
    <CedentePrestatore>
      <DatiAnagrafici>
        <IdFiscaleIVA>
          <IdPaese>IT</IdPaese>
          <IdCodice>01234567890</IdCodice>
        </IdFiscaleIVA>
        <CodiceFiscale>01234567890</CodiceFiscale>
        <Anagrafica>
          <Denominazione>ACME S.r.l.</Denominazione>
        </Anagrafica>
        <RegimeFiscale>RF01</RegimeFiscale>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Roma 1</Indirizzo>
        <CAP>00100</CAP>
        <Comune>Roma</Comune>
        <Provincia>RM</Provincia>
        <Nazione>IT</Nazione>
      </Sede>
    </CedentePrestatore>
    <CessionarioCommittente>
      <DatiAnagrafici>
        <IdFiscaleIVA>
          <IdPaese>IT</IdPaese>
          <IdCodice>98765432109</IdCodice>
        </IdFiscaleIVA>
        <CodiceFiscale>98765432109</CodiceFiscale>
        <Anagrafica>
          <Denominazione>Cliente Test S.p.A.</Denominazione>
        </Anagrafica>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Milano 10</Indirizzo>
        <CAP>20100</CAP>
        <Comune>Milano</Comune>
        <Provincia>MI</Provincia>
        <Nazione>IT</Nazione>
      </Sede>
    </CessionarioCommittente>
  </FatturaElettronicaHeader>
  <FatturaElettronicaBody>
    <DatiGenerali>
      <DatiGeneraliDocumento>
        <TipoDocumento>TD01</TipoDocumento>
        <Divisa>EUR</Divisa>
        <Data>2024-01-15</Data>
        <Numero>FV-2024/001</Numero>
        <ImportoTotaleDocumento>1220.00</ImportoTotaleDocumento>
        <Causale>Servizi di consulenza</Causale>
      </DatiGeneraliDocumento>
    </DatiGenerali>
    <DatiBeniServizi>
      <DettaglioLinee>
        <NumeroLinea>1</NumeroLinea>
        <Descrizione>Consulenza informatica</Descrizione>
        <Quantita>10.00</Quantita>
        <UnitaMisura>ORA</UnitaMisura>
        <PrezzoUnitario>100.00</PrezzoUnitario>
        <PrezzoTotale>1000.00</PrezzoTotale>
        <AliquotaIVA>22.00</AliquotaIVA>
      </DettaglioLinee>
      <DatiRiepilogo>
        <AliquotaIVA>22.00</AliquotaIVA>
        <ImponibileImporto>1000.00</ImponibileImporto>
        <Imposta>220.00</Imposta>
        <EsigibilitaIVA>I</EsigibilitaIVA>
      </DatiRiepilogo>
    </DatiBeniServizi>
    <DatiPagamento>
      <CondizioniPagamento>TP02</CondizioniPagamento>
      <DettaglioPagamento>
        <ModalitaPagamento>MP05</ModalitaPagamento>
        <ImportoPagamento>1220.00</ImportoPagamento>
        <IBAN>IT60X0542811101000000123456</IBAN>
        <DataScadenzaPagamento>2024-02-15</DataScadenzaPagamento>
      </DettaglioPagamento>
    </DatiPagamento>
  </FatturaElettronicaBody>
</FatturaElettronica>`

// XML without namespace prefix (some systems generate this format)
const validFatturaXMLNoNamespace = `<?xml version="1.0" encoding="UTF-8"?>
<FatturaElettronica versione="FPR12">
  <FatturaElettronicaHeader>
    <DatiTrasmissione>
      <IdTrasmittente>
        <IdPaese>IT</IdPaese>
        <IdCodice>01234567890</IdCodice>
      </IdTrasmittente>
      <ProgressivoInvio>00001</ProgressivoInvio>
      <FormatoTrasmissione>FPR12</FormatoTrasmissione>
      <CodiceDestinatario>JKKZDGR</CodiceDestinatario>
    </DatiTrasmissione>
    <CedentePrestatore>
      <DatiAnagrafici>
        <IdFiscaleIVA>
          <IdPaese>IT</IdPaese>
          <IdCodice>01234567890</IdCodice>
        </IdFiscaleIVA>
        <Anagrafica>
          <Denominazione>Test Supplier</Denominazione>
        </Anagrafica>
        <RegimeFiscale>RF01</RegimeFiscale>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Test 1</Indirizzo>
        <CAP>00100</CAP>
        <Comune>Roma</Comune>
        <Nazione>IT</Nazione>
      </Sede>
    </CedentePrestatore>
    <CessionarioCommittente>
      <DatiAnagrafici>
        <IdFiscaleIVA>
          <IdPaese>IT</IdPaese>
          <IdCodice>98765432109</IdCodice>
        </IdFiscaleIVA>
        <Anagrafica>
          <Denominazione>Buyer Test</Denominazione>
        </Anagrafica>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Buyer 1</Indirizzo>
        <CAP>20100</CAP>
        <Comune>Milano</Comune>
        <Nazione>IT</Nazione>
      </Sede>
    </CessionarioCommittente>
  </FatturaElettronicaHeader>
  <FatturaElettronicaBody>
    <DatiGenerali>
      <DatiGeneraliDocumento>
        <TipoDocumento>TD01</TipoDocumento>
        <Divisa>EUR</Divisa>
        <Data>2024-01-15</Data>
        <Numero>2024/001</Numero>
      </DatiGeneraliDocumento>
    </DatiGenerali>
    <DatiBeniServizi>
      <DettaglioLinee>
        <NumeroLinea>1</NumeroLinea>
        <Descrizione>Test Service</Descrizione>
        <PrezzoUnitario>100.00</PrezzoUnitario>
        <PrezzoTotale>100.00</PrezzoTotale>
        <AliquotaIVA>22.00</AliquotaIVA>
      </DettaglioLinee>
      <DatiRiepilogo>
        <AliquotaIVA>22.00</AliquotaIVA>
        <ImponibileImporto>100.00</ImponibileImporto>
        <Imposta>22.00</Imposta>
      </DatiRiepilogo>
    </DatiBeniServizi>
  </FatturaElettronicaBody>
</FatturaElettronica>`

// XML with multiple invoice bodies (batch invoice)
const validFatturaXMLBatch = `<?xml version="1.0" encoding="UTF-8"?>
<FatturaElettronica xmlns="http://ivaservizi.agenziaentrate.gov.it/docs/xsd/fatture/v1.2" versione="FPR12">
  <FatturaElettronicaHeader>
    <DatiTrasmissione>
      <IdTrasmittente>
        <IdPaese>IT</IdPaese>
        <IdCodice>01234567890</IdCodice>
      </IdTrasmittente>
      <ProgressivoInvio>00001</ProgressivoInvio>
      <FormatoTrasmissione>FPR12</FormatoTrasmissione>
      <CodiceDestinatario>JKKZDGR</CodiceDestinatario>
    </DatiTrasmissione>
    <CedentePrestatore>
      <DatiAnagrafici>
        <IdFiscaleIVA>
          <IdPaese>IT</IdPaese>
          <IdCodice>01234567890</IdCodice>
        </IdFiscaleIVA>
        <Anagrafica>
          <Denominazione>Batch Supplier</Denominazione>
        </Anagrafica>
        <RegimeFiscale>RF01</RegimeFiscale>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Batch 1</Indirizzo>
        <CAP>00100</CAP>
        <Comune>Roma</Comune>
        <Nazione>IT</Nazione>
      </Sede>
    </CedentePrestatore>
    <CessionarioCommittente>
      <DatiAnagrafici>
        <IdFiscaleIVA>
          <IdPaese>IT</IdPaese>
          <IdCodice>98765432109</IdCodice>
        </IdFiscaleIVA>
        <Anagrafica>
          <Denominazione>Batch Buyer</Denominazione>
        </Anagrafica>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Buyer 1</Indirizzo>
        <CAP>20100</CAP>
        <Comune>Milano</Comune>
        <Nazione>IT</Nazione>
      </Sede>
    </CessionarioCommittente>
  </FatturaElettronicaHeader>
  <FatturaElettronicaBody>
    <DatiGenerali>
      <DatiGeneraliDocumento>
        <TipoDocumento>TD01</TipoDocumento>
        <Divisa>EUR</Divisa>
        <Data>2024-01-15</Data>
        <Numero>BATCH-001</Numero>
      </DatiGeneraliDocumento>
    </DatiGenerali>
    <DatiBeniServizi>
      <DettaglioLinee>
        <NumeroLinea>1</NumeroLinea>
        <Descrizione>Service A</Descrizione>
        <PrezzoUnitario>100.00</PrezzoUnitario>
        <PrezzoTotale>100.00</PrezzoTotale>
        <AliquotaIVA>22.00</AliquotaIVA>
      </DettaglioLinee>
      <DatiRiepilogo>
        <AliquotaIVA>22.00</AliquotaIVA>
        <ImponibileImporto>100.00</ImponibileImporto>
        <Imposta>22.00</Imposta>
      </DatiRiepilogo>
    </DatiBeniServizi>
  </FatturaElettronicaBody>
  <FatturaElettronicaBody>
    <DatiGenerali>
      <DatiGeneraliDocumento>
        <TipoDocumento>TD01</TipoDocumento>
        <Divisa>EUR</Divisa>
        <Data>2024-01-16</Data>
        <Numero>BATCH-002</Numero>
      </DatiGeneraliDocumento>
    </DatiGenerali>
    <DatiBeniServizi>
      <DettaglioLinee>
        <NumeroLinea>1</NumeroLinea>
        <Descrizione>Service B</Descrizione>
        <PrezzoUnitario>200.00</PrezzoUnitario>
        <PrezzoTotale>200.00</PrezzoTotale>
        <AliquotaIVA>22.00</AliquotaIVA>
      </DettaglioLinee>
      <DatiRiepilogo>
        <AliquotaIVA>22.00</AliquotaIVA>
        <ImponibileImporto>200.00</ImponibileImporto>
        <Imposta>44.00</Imposta>
      </DatiRiepilogo>
    </DatiBeniServizi>
  </FatturaElettronicaBody>
</FatturaElettronica>`

// XML with credit note (TD04)
const validFatturaXMLCreditNote = `<?xml version="1.0" encoding="UTF-8"?>
<FatturaElettronica xmlns="http://ivaservizi.agenziaentrate.gov.it/docs/xsd/fatture/v1.2" versione="FPR12">
  <FatturaElettronicaHeader>
    <DatiTrasmissione>
      <IdTrasmittente>
        <IdPaese>IT</IdPaese>
        <IdCodice>01234567890</IdCodice>
      </IdTrasmittente>
      <ProgressivoInvio>00001</ProgressivoInvio>
      <FormatoTrasmissione>FPR12</FormatoTrasmissione>
      <CodiceDestinatario>JKKZDGR</CodiceDestinatario>
    </DatiTrasmissione>
    <CedentePrestatore>
      <DatiAnagrafici>
        <IdFiscaleIVA>
          <IdPaese>IT</IdPaese>
          <IdCodice>01234567890</IdCodice>
        </IdFiscaleIVA>
        <Anagrafica>
          <Denominazione>Credit Note Supplier</Denominazione>
        </Anagrafica>
        <RegimeFiscale>RF01</RegimeFiscale>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Credit 1</Indirizzo>
        <CAP>00100</CAP>
        <Comune>Roma</Comune>
        <Nazione>IT</Nazione>
      </Sede>
    </CedentePrestatore>
    <CessionarioCommittente>
      <DatiAnagrafici>
        <CodiceFiscale>RSSMRA80A01H501Z</CodiceFiscale>
        <Anagrafica>
          <Nome>Mario</Nome>
          <Cognome>Rossi</Cognome>
        </Anagrafica>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Buyer 1</Indirizzo>
        <CAP>20100</CAP>
        <Comune>Milano</Comune>
        <Nazione>IT</Nazione>
      </Sede>
    </CessionarioCommittente>
  </FatturaElettronicaHeader>
  <FatturaElettronicaBody>
    <DatiGenerali>
      <DatiGeneraliDocumento>
        <TipoDocumento>TD04</TipoDocumento>
        <Divisa>EUR</Divisa>
        <Data>2024-01-20</Data>
        <Numero>NC-2024/001</Numero>
      </DatiGeneraliDocumento>
    </DatiGenerali>
    <DatiBeniServizi>
      <DettaglioLinee>
        <NumeroLinea>1</NumeroLinea>
        <Descrizione>Storno parziale fattura FV-2024/001</Descrizione>
        <PrezzoUnitario>-50.00</PrezzoUnitario>
        <PrezzoTotale>-50.00</PrezzoTotale>
        <AliquotaIVA>22.00</AliquotaIVA>
      </DettaglioLinee>
      <DatiRiepilogo>
        <AliquotaIVA>22.00</AliquotaIVA>
        <ImponibileImporto>-50.00</ImponibileImporto>
        <Imposta>-11.00</Imposta>
      </DatiRiepilogo>
    </DatiBeniServizi>
  </FatturaElettronicaBody>
</FatturaElettronica>`

// XML with person (not company) as supplier
const validFatturaXMLPerson = `<?xml version="1.0" encoding="UTF-8"?>
<FatturaElettronica xmlns="http://ivaservizi.agenziaentrate.gov.it/docs/xsd/fatture/v1.2" versione="FPR12">
  <FatturaElettronicaHeader>
    <DatiTrasmissione>
      <IdTrasmittente>
        <IdPaese>IT</IdPaese>
        <IdCodice>RSSMRA80A01H501Z</IdCodice>
      </IdTrasmittente>
      <ProgressivoInvio>00001</ProgressivoInvio>
      <FormatoTrasmissione>FPR12</FormatoTrasmissione>
      <CodiceDestinatario>JKKZDGR</CodiceDestinatario>
    </DatiTrasmissione>
    <CedentePrestatore>
      <DatiAnagrafici>
        <IdFiscaleIVA>
          <IdPaese>IT</IdPaese>
          <IdCodice>RSSMRA80A01H501Z</IdCodice>
        </IdFiscaleIVA>
        <CodiceFiscale>RSSMRA80A01H501Z</CodiceFiscale>
        <Anagrafica>
          <Nome>Mario</Nome>
          <Cognome>Rossi</Cognome>
        </Anagrafica>
        <RegimeFiscale>RF19</RegimeFiscale>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Privata 1</Indirizzo>
        <CAP>00100</CAP>
        <Comune>Roma</Comune>
        <Provincia>RM</Provincia>
        <Nazione>IT</Nazione>
      </Sede>
    </CedentePrestatore>
    <CessionarioCommittente>
      <DatiAnagrafici>
        <IdFiscaleIVA>
          <IdPaese>IT</IdPaese>
          <IdCodice>98765432109</IdCodice>
        </IdFiscaleIVA>
        <Anagrafica>
          <Denominazione>Buyer Company</Denominazione>
        </Anagrafica>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Buyer 1</Indirizzo>
        <CAP>20100</CAP>
        <Comune>Milano</Comune>
        <Nazione>IT</Nazione>
      </Sede>
    </CessionarioCommittente>
  </FatturaElettronicaHeader>
  <FatturaElettronicaBody>
    <DatiGenerali>
      <DatiGeneraliDocumento>
        <TipoDocumento>TD01</TipoDocumento>
        <Divisa>EUR</Divisa>
        <Data>2024-01-15</Data>
        <Numero>1/2024</Numero>
      </DatiGeneraliDocumento>
    </DatiGenerali>
    <DatiBeniServizi>
      <DettaglioLinee>
        <NumeroLinea>1</NumeroLinea>
        <Descrizione>Prestazione professionale</Descrizione>
        <PrezzoUnitario>500.00</PrezzoUnitario>
        <PrezzoTotale>500.00</PrezzoTotale>
        <AliquotaIVA>22.00</AliquotaIVA>
      </DettaglioLinee>
      <DatiRiepilogo>
        <AliquotaIVA>22.00</AliquotaIVA>
        <ImponibileImporto>500.00</ImponibileImporto>
        <Imposta>110.00</Imposta>
      </DatiRiepilogo>
    </DatiBeniServizi>
  </FatturaElettronicaBody>
</FatturaElettronica>`

func TestXMLParser_Parse_ValidInvoice(t *testing.T) {
	parser := NewXMLParser()

	fattura, err := parser.Parse([]byte(validFatturaXML))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Validate header
	if fattura.Versione != "FPR12" {
		t.Errorf("expected versione FPR12, got: %s", fattura.Versione)
	}

	// Validate cedente (supplier)
	cedente := fattura.FatturaElettronicaHeader.CedentePrestatore
	if cedente.DatiAnagrafici.IdFiscaleIVA.IdCodice != "01234567890" {
		t.Errorf("expected cedente IdCodice 01234567890, got: %s", cedente.DatiAnagrafici.IdFiscaleIVA.IdCodice)
	}
	if cedente.DatiAnagrafici.Anagrafica.Denominazione != "ACME S.r.l." {
		t.Errorf("expected cedente Denominazione 'ACME S.r.l.', got: %s", cedente.DatiAnagrafici.Anagrafica.Denominazione)
	}

	// Validate body
	if len(fattura.FatturaElettronicaBody) != 1 {
		t.Fatalf("expected 1 body, got: %d", len(fattura.FatturaElettronicaBody))
	}

	body := fattura.FatturaElettronicaBody[0]
	if body.DatiGenerali.DatiGeneraliDocumento.Numero != "FV-2024/001" {
		t.Errorf("expected invoice number 'FV-2024/001', got: %s", body.DatiGenerali.DatiGeneraliDocumento.Numero)
	}
	if body.DatiGenerali.DatiGeneraliDocumento.TipoDocumento != "TD01" {
		t.Errorf("expected document type TD01, got: %s", body.DatiGenerali.DatiGeneraliDocumento.TipoDocumento)
	}

	// Validate line items
	if len(body.DatiBeniServizi.DettaglioLinee) != 1 {
		t.Fatalf("expected 1 line, got: %d", len(body.DatiBeniServizi.DettaglioLinee))
	}
	if body.DatiBeniServizi.DettaglioLinee[0].Descrizione != "Consulenza informatica" {
		t.Errorf("expected description 'Consulenza informatica', got: %s", body.DatiBeniServizi.DettaglioLinee[0].Descrizione)
	}

	// Validate payment
	if body.DatiPagamento == nil {
		t.Fatal("expected payment data, got nil")
	}
	if body.DatiPagamento.CondizioniPagamento != "TP02" {
		t.Errorf("expected payment condition TP02, got: %s", body.DatiPagamento.CondizioniPagamento)
	}
}

func TestXMLParser_Parse_NoNamespace(t *testing.T) {
	parser := NewXMLParser()

	fattura, err := parser.Parse([]byte(validFatturaXMLNoNamespace))
	if err != nil {
		t.Fatalf("expected no error parsing XML without namespace, got: %v", err)
	}

	if fattura.FatturaElettronicaHeader.CedentePrestatore.DatiAnagrafici.Anagrafica.Denominazione != "Test Supplier" {
		t.Errorf("expected 'Test Supplier', got: %s", fattura.FatturaElettronicaHeader.CedentePrestatore.DatiAnagrafici.Anagrafica.Denominazione)
	}

	if len(fattura.FatturaElettronicaBody) != 1 {
		t.Errorf("expected 1 body, got: %d", len(fattura.FatturaElettronicaBody))
	}
}

func TestXMLParser_Parse_BatchInvoice(t *testing.T) {
	parser := NewXMLParser()

	fattura, err := parser.Parse([]byte(validFatturaXMLBatch))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(fattura.FatturaElettronicaBody) != 2 {
		t.Fatalf("expected 2 bodies (batch invoice), got: %d", len(fattura.FatturaElettronicaBody))
	}

	// First invoice
	if fattura.FatturaElettronicaBody[0].DatiGenerali.DatiGeneraliDocumento.Numero != "BATCH-001" {
		t.Errorf("expected first invoice number 'BATCH-001', got: %s", fattura.FatturaElettronicaBody[0].DatiGenerali.DatiGeneraliDocumento.Numero)
	}

	// Second invoice
	if fattura.FatturaElettronicaBody[1].DatiGenerali.DatiGeneraliDocumento.Numero != "BATCH-002" {
		t.Errorf("expected second invoice number 'BATCH-002', got: %s", fattura.FatturaElettronicaBody[1].DatiGenerali.DatiGeneraliDocumento.Numero)
	}
}

func TestXMLParser_Parse_CreditNote(t *testing.T) {
	parser := NewXMLParser()

	fattura, err := parser.Parse([]byte(validFatturaXMLCreditNote))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	body := fattura.FatturaElettronicaBody[0]
	if body.DatiGenerali.DatiGeneraliDocumento.TipoDocumento != "TD04" {
		t.Errorf("expected document type TD04 (credit note), got: %s", body.DatiGenerali.DatiGeneraliDocumento.TipoDocumento)
	}

	// Verify buyer is a person (CodiceFiscale, no P.IVA)
	cessionario := fattura.FatturaElettronicaHeader.CessionarioCommittente
	if cessionario.DatiAnagrafici.CodiceFiscale != "RSSMRA80A01H501Z" {
		t.Errorf("expected buyer CF 'RSSMRA80A01H501Z', got: %s", cessionario.DatiAnagrafici.CodiceFiscale)
	}
	if cessionario.DatiAnagrafici.Anagrafica.Nome != "Mario" {
		t.Errorf("expected buyer name 'Mario', got: %s", cessionario.DatiAnagrafici.Anagrafica.Nome)
	}
}

func TestXMLParser_Parse_PersonSupplier(t *testing.T) {
	parser := NewXMLParser()

	fattura, err := parser.Parse([]byte(validFatturaXMLPerson))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	cedente := fattura.FatturaElettronicaHeader.CedentePrestatore
	if cedente.DatiAnagrafici.Anagrafica.Nome != "Mario" {
		t.Errorf("expected supplier name 'Mario', got: %s", cedente.DatiAnagrafici.Anagrafica.Nome)
	}
	if cedente.DatiAnagrafici.Anagrafica.Cognome != "Rossi" {
		t.Errorf("expected supplier surname 'Rossi', got: %s", cedente.DatiAnagrafici.Anagrafica.Cognome)
	}
	if cedente.DatiAnagrafici.Anagrafica.Denominazione != "" {
		t.Errorf("expected no denomination for person, got: %s", cedente.DatiAnagrafici.Anagrafica.Denominazione)
	}
	if cedente.DatiAnagrafici.RegimeFiscale != "RF19" {
		t.Errorf("expected regime fiscale RF19, got: %s", cedente.DatiAnagrafici.RegimeFiscale)
	}
}

func TestXMLParser_Parse_EmptyXML(t *testing.T) {
	parser := NewXMLParser()

	_, err := parser.Parse([]byte{})
	if err != ErrEmptyXML {
		t.Errorf("expected ErrEmptyXML, got: %v", err)
	}
}

func TestXMLParser_Parse_MalformedXML(t *testing.T) {
	parser := NewXMLParser()

	testCases := []struct {
		name string
		xml  string
	}{
		{
			name: "Not XML at all",
			xml:  "This is not XML",
		},
		{
			name: "Unclosed tag",
			xml:  `<?xml version="1.0"?><root><unclosed>`,
		},
		{
			name: "Invalid encoding",
			xml:  `<?xml version="1.0" encoding="INVALID"?><root></root>`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parser.Parse([]byte(tc.xml))
			if err == nil {
				t.Errorf("expected error for malformed XML: %s", tc.name)
			}
		})
	}
}

func TestXMLParser_Parse_MissingCedente(t *testing.T) {
	parser := NewXMLParser()

	xmlNoCedente := `<?xml version="1.0" encoding="UTF-8"?>
<FatturaElettronica versione="FPR12">
  <FatturaElettronicaHeader>
    <DatiTrasmissione>
      <IdTrasmittente>
        <IdPaese>IT</IdPaese>
        <IdCodice>01234567890</IdCodice>
      </IdTrasmittente>
      <ProgressivoInvio>00001</ProgressivoInvio>
      <FormatoTrasmissione>FPR12</FormatoTrasmissione>
      <CodiceDestinatario>JKKZDGR</CodiceDestinatario>
    </DatiTrasmissione>
    <CedentePrestatore>
      <DatiAnagrafici>
        <IdFiscaleIVA>
          <IdPaese>IT</IdPaese>
          <IdCodice></IdCodice>
        </IdFiscaleIVA>
        <Anagrafica>
          <Denominazione>Test</Denominazione>
        </Anagrafica>
        <RegimeFiscale>RF01</RegimeFiscale>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Test</Indirizzo>
        <CAP>00100</CAP>
        <Comune>Roma</Comune>
        <Nazione>IT</Nazione>
      </Sede>
    </CedentePrestatore>
    <CessionarioCommittente>
      <DatiAnagrafici>
        <CodiceFiscale>12345678901</CodiceFiscale>
        <Anagrafica>
          <Denominazione>Buyer</Denominazione>
        </Anagrafica>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Buyer</Indirizzo>
        <CAP>20100</CAP>
        <Comune>Milano</Comune>
        <Nazione>IT</Nazione>
      </Sede>
    </CessionarioCommittente>
  </FatturaElettronicaHeader>
  <FatturaElettronicaBody>
    <DatiGenerali>
      <DatiGeneraliDocumento>
        <TipoDocumento>TD01</TipoDocumento>
        <Divisa>EUR</Divisa>
        <Data>2024-01-15</Data>
        <Numero>001</Numero>
      </DatiGeneraliDocumento>
    </DatiGenerali>
    <DatiBeniServizi>
      <DettaglioLinee>
        <NumeroLinea>1</NumeroLinea>
        <Descrizione>Test</Descrizione>
        <PrezzoUnitario>100.00</PrezzoUnitario>
        <PrezzoTotale>100.00</PrezzoTotale>
        <AliquotaIVA>22.00</AliquotaIVA>
      </DettaglioLinee>
      <DatiRiepilogo>
        <AliquotaIVA>22.00</AliquotaIVA>
        <ImponibileImporto>100.00</ImponibileImporto>
        <Imposta>22.00</Imposta>
      </DatiRiepilogo>
    </DatiBeniServizi>
  </FatturaElettronicaBody>
</FatturaElettronica>`

	_, err := parser.Parse([]byte(xmlNoCedente))
	if err != ErrMissingCedente {
		t.Errorf("expected ErrMissingCedente, got: %v", err)
	}
}

func TestXMLParser_Parse_MissingInvoiceBody(t *testing.T) {
	parser := NewXMLParser()

	xmlNoBody := `<?xml version="1.0" encoding="UTF-8"?>
<FatturaElettronica versione="FPR12">
  <FatturaElettronicaHeader>
    <DatiTrasmissione>
      <IdTrasmittente>
        <IdPaese>IT</IdPaese>
        <IdCodice>01234567890</IdCodice>
      </IdTrasmittente>
      <ProgressivoInvio>00001</ProgressivoInvio>
      <FormatoTrasmissione>FPR12</FormatoTrasmissione>
      <CodiceDestinatario>JKKZDGR</CodiceDestinatario>
    </DatiTrasmissione>
    <CedentePrestatore>
      <DatiAnagrafici>
        <IdFiscaleIVA>
          <IdPaese>IT</IdPaese>
          <IdCodice>01234567890</IdCodice>
        </IdFiscaleIVA>
        <Anagrafica>
          <Denominazione>Test</Denominazione>
        </Anagrafica>
        <RegimeFiscale>RF01</RegimeFiscale>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Test</Indirizzo>
        <CAP>00100</CAP>
        <Comune>Roma</Comune>
        <Nazione>IT</Nazione>
      </Sede>
    </CedentePrestatore>
    <CessionarioCommittente>
      <DatiAnagrafici>
        <CodiceFiscale>12345678901</CodiceFiscale>
        <Anagrafica>
          <Denominazione>Buyer</Denominazione>
        </Anagrafica>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Buyer</Indirizzo>
        <CAP>20100</CAP>
        <Comune>Milano</Comune>
        <Nazione>IT</Nazione>
      </Sede>
    </CessionarioCommittente>
  </FatturaElettronicaHeader>
</FatturaElettronica>`

	_, err := parser.Parse([]byte(xmlNoBody))
	if err != ErrMissingInvoiceBody {
		t.Errorf("expected ErrMissingInvoiceBody, got: %v", err)
	}
}

func TestXMLParser_Parse_WithBOM(t *testing.T) {
	parser := NewXMLParser()

	// Add UTF-8 BOM to the beginning of the XML
	xmlWithBOM := append([]byte{0xEF, 0xBB, 0xBF}, []byte(validFatturaXMLNoNamespace)...)

	fattura, err := parser.Parse(xmlWithBOM)
	if err != nil {
		t.Fatalf("expected parser to handle BOM, got error: %v", err)
	}

	if len(fattura.FatturaElettronicaBody) != 1 {
		t.Errorf("expected 1 body after parsing XML with BOM, got: %d", len(fattura.FatturaElettronicaBody))
	}
}

func TestXMLParser_Parse_Base64Encoded(t *testing.T) {
	// This test verifies that the XML parser works with decoded base64
	// (the actual base64 decoding is done in the service layer)
	parser := NewXMLParser()

	encoded := base64.StdEncoding.EncodeToString([]byte(validFatturaXML))
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}

	fattura, err := parser.Parse(decoded)
	if err != nil {
		t.Fatalf("expected no error parsing base64-decoded XML, got: %v", err)
	}

	if fattura.FatturaElettronicaBody[0].DatiGenerali.DatiGeneraliDocumento.Numero != "FV-2024/001" {
		t.Errorf("expected invoice number 'FV-2024/001' after base64 decode, got: %s",
			fattura.FatturaElettronicaBody[0].DatiGenerali.DatiGeneraliDocumento.Numero)
	}
}

func TestXMLParser_Parse_DocumentTypes(t *testing.T) {
	// Test that various document types (TD01-TD29) are handled correctly
	documentTypes := []struct {
		code string
		desc string
	}{
		{"TD01", "Fattura"},
		{"TD02", "Acconto/Anticipo su fattura"},
		{"TD03", "Acconto/Anticipo su parcella"},
		{"TD04", "Nota di Credito"},
		{"TD05", "Nota di Debito"},
		{"TD06", "Parcella"},
		{"TD16", "Integrazione fattura reverse charge interno"},
		{"TD17", "Integrazione/autofattura per acquisto servizi dall'estero"},
		{"TD18", "Integrazione per acquisto di beni intracomunitari"},
		{"TD19", "Integrazione/autofattura per acquisto di beni ex art.17 c.2 DPR 633/72"},
		{"TD20", "Autofattura per regolarizzazione e integrazione delle fatture"},
		{"TD21", "Autofattura per splafonamento"},
		{"TD22", "Estrazione beni da Deposito IVA"},
		{"TD23", "Estrazione beni da Deposito IVA con versamento dell'IVA"},
		{"TD24", "Fattura differita di cui all'art. 21, comma 4, lett. a)"},
		{"TD25", "Fattura differita di cui all'art. 21, comma 4, terzo periodo lett. b)"},
		{"TD26", "Cessione di beni ammortizzabili e passaggi interni"},
		{"TD27", "Fattura per autoconsumo o cessioni gratuite senza rivalsa"},
	}

	parser := NewXMLParser()

	for _, dt := range documentTypes {
		t.Run(dt.code, func(t *testing.T) {
			xml := generateXMLWithDocType(dt.code)
			fattura, err := parser.Parse([]byte(xml))
			if err != nil {
				t.Fatalf("failed to parse XML with document type %s: %v", dt.code, err)
			}

			if string(fattura.FatturaElettronicaBody[0].DatiGenerali.DatiGeneraliDocumento.TipoDocumento) != dt.code {
				t.Errorf("expected document type %s, got: %s", dt.code,
					fattura.FatturaElettronicaBody[0].DatiGenerali.DatiGeneraliDocumento.TipoDocumento)
			}
		})
	}
}

// Helper function to generate XML with a specific document type
func generateXMLWithDocType(docType string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<FatturaElettronica versione="FPR12">
  <FatturaElettronicaHeader>
    <DatiTrasmissione>
      <IdTrasmittente>
        <IdPaese>IT</IdPaese>
        <IdCodice>01234567890</IdCodice>
      </IdTrasmittente>
      <ProgressivoInvio>00001</ProgressivoInvio>
      <FormatoTrasmissione>FPR12</FormatoTrasmissione>
      <CodiceDestinatario>JKKZDGR</CodiceDestinatario>
    </DatiTrasmissione>
    <CedentePrestatore>
      <DatiAnagrafici>
        <IdFiscaleIVA>
          <IdPaese>IT</IdPaese>
          <IdCodice>01234567890</IdCodice>
        </IdFiscaleIVA>
        <Anagrafica>
          <Denominazione>Test Supplier</Denominazione>
        </Anagrafica>
        <RegimeFiscale>RF01</RegimeFiscale>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Test 1</Indirizzo>
        <CAP>00100</CAP>
        <Comune>Roma</Comune>
        <Nazione>IT</Nazione>
      </Sede>
    </CedentePrestatore>
    <CessionarioCommittente>
      <DatiAnagrafici>
        <IdFiscaleIVA>
          <IdPaese>IT</IdPaese>
          <IdCodice>98765432109</IdCodice>
        </IdFiscaleIVA>
        <Anagrafica>
          <Denominazione>Test Buyer</Denominazione>
        </Anagrafica>
      </DatiAnagrafici>
      <Sede>
        <Indirizzo>Via Buyer 1</Indirizzo>
        <CAP>20100</CAP>
        <Comune>Milano</Comune>
        <Nazione>IT</Nazione>
      </Sede>
    </CessionarioCommittente>
  </FatturaElettronicaHeader>
  <FatturaElettronicaBody>
    <DatiGenerali>
      <DatiGeneraliDocumento>
        <TipoDocumento>` + docType + `</TipoDocumento>
        <Divisa>EUR</Divisa>
        <Data>2024-01-15</Data>
        <Numero>TEST-001</Numero>
      </DatiGeneraliDocumento>
    </DatiGenerali>
    <DatiBeniServizi>
      <DettaglioLinee>
        <NumeroLinea>1</NumeroLinea>
        <Descrizione>Test</Descrizione>
        <PrezzoUnitario>100.00</PrezzoUnitario>
        <PrezzoTotale>100.00</PrezzoTotale>
        <AliquotaIVA>22.00</AliquotaIVA>
      </DettaglioLinee>
      <DatiRiepilogo>
        <AliquotaIVA>22.00</AliquotaIVA>
        <ImponibileImporto>100.00</ImponibileImporto>
        <Imposta>22.00</Imposta>
      </DatiRiepilogo>
    </DatiBeniServizi>
  </FatturaElettronicaBody>
</FatturaElettronica>`
}

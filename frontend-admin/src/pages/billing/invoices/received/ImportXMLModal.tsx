import React, { useState, useRef } from 'react';
import { Modal, Button, Form, Alert, Nav, Tab, Spinner, Table } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useImportXMLInvoiceMutation } from 'store/api/billingApi';
import type { ImportXMLInvoiceResponse, ImportedInvoiceSummary, SkippedInvoice } from 'types/billing';
import { DOCUMENT_TYPE_LABELS, formatCurrency, formatItalianDate } from 'types/billing';
import FalconCloseButton from 'components/common/FalconCloseButton';

interface ImportXMLModalProps {
  show: boolean;
  onHide: () => void;
  onSuccess?: () => void;
}

const ImportXMLModal: React.FC<ImportXMLModalProps> = ({
  show,
  onHide,
  onSuccess
}) => {
  const [importXMLInvoice, { isLoading }] = useImportXMLInvoiceMutation();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [activeTab, setActiveTab] = useState<string>('file');
  const [error, setError] = useState<string>('');
  const [xmlContent, setXmlContent] = useState<string>('');
  const [fileName, setFileName] = useState<string>('');
  const [skipDuplicates, setSkipDuplicates] = useState<boolean>(true);
  const [result, setResult] = useState<ImportXMLInvoiceResponse | null>(null);

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    setFileName(file.name);
    setError('');
    setResult(null);

    const reader = new FileReader();
    reader.onload = (event) => {
      const content = event.target?.result as string;
      setXmlContent(content);
    };
    reader.onerror = () => {
      setError('Errore durante la lettura del file');
    };
    reader.readAsText(file);
  };

  const handleXmlChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setXmlContent(e.target.value);
    setError('');
    setResult(null);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setResult(null);

    if (!xmlContent.trim()) {
      setError('Il contenuto XML è obbligatorio');
      return;
    }

    // Basic XML validation
    if (!xmlContent.includes('<FatturaElettronica') && !xmlContent.includes('<?xml')) {
      setError('Il contenuto non sembra essere un file FatturaPA XML valido');
      return;
    }

    try {
      const response = await importXMLInvoice({
        xml: xmlContent,
        fileName: fileName || undefined,
        isBase64: false,
        skipDuplicates
      }).unwrap();

      setResult(response);

      if (response.count > 0) {
        onSuccess?.();
      }
    } catch (err: unknown) {
      const apiError = err as { data?: { title?: string; detail?: string; error?: string }; status?: number };
      if (apiError?.data?.detail) {
        setError(apiError.data.detail);
      } else if (apiError?.data?.title) {
        setError(apiError.data.title);
      } else if (apiError?.data?.error) {
        setError(apiError.data.error);
      } else {
        setError("Errore durante l'importazione della fattura");
      }
    }
  };

  const handleClose = () => {
    setXmlContent('');
    setFileName('');
    setError('');
    setResult(null);
    setActiveTab('file');
    if (fileInputRef.current) {
      fileInputRef.current.value = '';
    }
    onHide();
  };

  const handleImportAnother = () => {
    setXmlContent('');
    setFileName('');
    setError('');
    setResult(null);
    if (fileInputRef.current) {
      fileInputRef.current.value = '';
    }
  };

  return (
    <Modal show={show} onHide={handleClose} size="lg" centered>
      <Modal.Header className="bg-body-tertiary d-flex justify-content-between align-items-center">
        <Modal.Title as="h5" id="import-xml-modal-title">
          <FontAwesomeIcon icon="file-import" className="me-2 text-primary" />
          Importa Fattura XML
        </Modal.Title>
        <FalconCloseButton onClick={handleClose} />
      </Modal.Header>

      <Modal.Body className="p-4">
        {error && (
          <Alert variant="danger" dismissible onClose={() => setError('')}>
            <FontAwesomeIcon icon="exclamation-triangle" className="me-2" />
            {error}
          </Alert>
        )}

        {result ? (
          <div className="import-result">
            <Alert variant={result.count > 0 ? 'success' : 'warning'}>
              <FontAwesomeIcon
                icon={result.count > 0 ? 'check-circle' : 'exclamation-circle'}
                className="me-2"
              />
              {result.message}
            </Alert>

            {result.supplier && (
              <div className="mb-3 p-3 bg-body-tertiary rounded">
                <h6 className="mb-2">
                  <FontAwesomeIcon icon="building" className="me-2 text-primary" />
                  Fornitore
                  {result.supplier.isNew && (
                    <span className="badge bg-success ms-2">Nuovo</span>
                  )}
                </h6>
                <div>
                  <strong>{result.supplier.name}</strong>
                  <br />
                  <small className="text-body-secondary">P.IVA: {result.supplier.fiscalId}</small>
                </div>
              </div>
            )}

            {result.invoices && result.invoices.length > 0 && (
              <div className="mb-3">
                <h6 className="mb-2">
                  <FontAwesomeIcon icon="file-invoice" className="me-2 text-success" />
                  Fatture Importate ({result.count})
                </h6>
                <Table size="sm" bordered hover responsive>
                  <thead className="bg-body-tertiary">
                    <tr>
                      <th>Numero</th>
                      <th>Tipo</th>
                      <th>Data</th>
                      <th className="text-end">Importo</th>
                    </tr>
                  </thead>
                  <tbody>
                    {result.invoices.map((invoice: ImportedInvoiceSummary) => (
                      <tr key={invoice.id}>
                        <td>{invoice.number}</td>
                        <td>
                          <small>{DOCUMENT_TYPE_LABELS[invoice.documentType] || invoice.documentType}</small>
                        </td>
                        <td>{formatItalianDate(invoice.date)}</td>
                        <td className="text-end fw-semibold">{formatCurrency(invoice.totalAmount)}</td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              </div>
            )}

            {result.skipped && result.skipped.length > 0 && (
              <div className="mb-3">
                <h6 className="mb-2">
                  <FontAwesomeIcon icon="forward" className="me-2 text-warning" />
                  Fatture Saltate ({result.skipped.length})
                </h6>
                <Table size="sm" bordered responsive className="table-warning">
                  <thead>
                    <tr>
                      <th>Numero</th>
                      <th>Motivo</th>
                    </tr>
                  </thead>
                  <tbody>
                    {result.skipped.map((skipped: SkippedInvoice, idx: number) => (
                      <tr key={idx}>
                        <td>{skipped.number}</td>
                        <td>{skipped.reason}</td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              </div>
            )}

            <div className="d-flex gap-2 justify-content-end mt-4">
              <Button variant="outline-secondary" onClick={handleImportAnother}>
                <FontAwesomeIcon icon="plus" className="me-1" />
                Importa altra fattura
              </Button>
              <Button variant="primary" onClick={handleClose}>
                Chiudi
              </Button>
            </div>
          </div>
        ) : (
          <Form onSubmit={handleSubmit}>
            <Tab.Container activeKey={activeTab} onSelect={(k) => k && setActiveTab(k)}>
              <Nav variant="tabs" className="mb-3">
                <Nav.Item>
                  <Nav.Link eventKey="file">
                    <FontAwesomeIcon icon="upload" className="me-1" />
                    Carica File
                  </Nav.Link>
                </Nav.Item>
                <Nav.Item>
                  <Nav.Link eventKey="paste">
                    <FontAwesomeIcon icon="paste" className="me-1" />
                    Incolla XML
                  </Nav.Link>
                </Nav.Item>
              </Nav>

              <Tab.Content>
                <Tab.Pane eventKey="file">
                  <Form.Group className="mb-3">
                    <Form.Label>Seleziona file FatturaPA (.xml)</Form.Label>
                    <Form.Control
                      ref={fileInputRef}
                      type="file"
                      accept=".xml,application/xml,text/xml"
                      onChange={handleFileChange}
                    />
                    <Form.Text className="text-body-secondary">
                      Supporta file FatturaPA in formato XML (es. IT12345678901_XXXXX.xml)
                    </Form.Text>
                  </Form.Group>
                  {fileName && (
                    <Alert variant="info" className="py-2">
                      <FontAwesomeIcon icon="file-code" className="me-2" />
                      File selezionato: <strong>{fileName}</strong>
                    </Alert>
                  )}
                </Tab.Pane>

                <Tab.Pane eventKey="paste">
                  <Form.Group className="mb-3">
                    <Form.Label>Contenuto XML FatturaPA</Form.Label>
                    <Form.Control
                      as="textarea"
                      rows={10}
                      value={xmlContent}
                      onChange={handleXmlChange}
                      placeholder="Incolla qui il contenuto XML della fattura..."
                      style={{ fontFamily: 'monospace', fontSize: '0.85rem' }}
                    />
                    <Form.Text className="text-body-secondary">
                      Incolla il contenuto completo del file XML FatturaPA
                    </Form.Text>
                  </Form.Group>
                </Tab.Pane>
              </Tab.Content>
            </Tab.Container>

            <hr className="my-3" />

            <Form.Group className="mb-3">
              <Form.Check
                type="checkbox"
                id="skipDuplicates"
                label="Salta fatture duplicate (consigliato)"
                checked={skipDuplicates}
                onChange={(e) => setSkipDuplicates(e.target.checked)}
              />
              <Form.Text className="text-body-secondary">
                Se attivo, le fatture già presenti nel sistema verranno saltate invece di generare un errore
              </Form.Text>
            </Form.Group>

            <div className="d-flex gap-2 justify-content-end">
              <Button variant="outline-secondary" onClick={handleClose} disabled={isLoading}>
                Annulla
              </Button>
              <Button
                variant="primary"
                type="submit"
                disabled={isLoading || !xmlContent.trim()}
              >
                {isLoading ? (
                  <>
                    <Spinner animation="border" size="sm" className="me-1" />
                    Importazione in corso...
                  </>
                ) : (
                  <>
                    <FontAwesomeIcon icon="file-import" className="me-1" />
                    Importa Fattura
                  </>
                )}
              </Button>
            </div>
          </Form>
        )}
      </Modal.Body>
    </Modal>
  );
};

export default ImportXMLModal;

import React, { useState, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Modal,
  Button,
  Form,
  Alert,
  Nav,
  Tab,
  Spinner,
  Table
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useImportXMLInvoiceMutation } from 'store/api/billingApi';
import type {
  ImportXMLInvoiceResponse,
  ImportedInvoiceSummary,
  SkippedInvoice
} from 'types/billing';
import {
  DOCUMENT_TYPE_LABELS,
  formatCurrency,
  formatItalianDate
} from 'types/billing';
import OrkestraCloseButton from 'components/common/OrkestraCloseButton';

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
  const { t } = useTranslation();
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
    reader.onload = event => {
      const content = event.target?.result as string;
      setXmlContent(content);
    };
    reader.onerror = () => {
      setError(t('billing.importXml.errors.readFile'));
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
      setError(t('billing.importXml.errors.xmlRequired'));
      return;
    }

    // Basic XML validation
    if (
      !xmlContent.includes('<FatturaElettronica') &&
      !xmlContent.includes('<?xml')
    ) {
      setError(t('billing.importXml.errors.notValidFatturaPa'));
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
      const apiError = err as {
        data?: { title?: string; detail?: string; error?: string };
        status?: number;
      };
      if (apiError?.data?.detail) {
        setError(apiError.data.detail);
      } else if (apiError?.data?.title) {
        setError(apiError.data.title);
      } else if (apiError?.data?.error) {
        setError(apiError.data.error);
      } else {
        setError(t('billing.importXml.errors.importFailed'));
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
          {t('billing.importXml.title')}
        </Modal.Title>
        <OrkestraCloseButton onClick={handleClose} />
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
                  <FontAwesomeIcon
                    icon="building"
                    className="me-2 text-primary"
                  />
                  {t('billing.importXml.result.supplier')}
                  {result.supplier.isNew && (
                    <span className="badge bg-success ms-2">
                      {t('billing.importXml.result.supplierNewBadge')}
                    </span>
                  )}
                </h6>
                <div>
                  <strong>{result.supplier.name}</strong>
                  <br />
                  <small className="text-body-secondary">
                    {t('billing.importXml.result.supplierVat')}{' '}
                    {result.supplier.fiscalId}
                  </small>
                </div>
              </div>
            )}

            {result.invoices && result.invoices.length > 0 && (
              <div className="mb-3">
                <h6 className="mb-2">
                  <FontAwesomeIcon
                    icon="file-invoice"
                    className="me-2 text-success"
                  />
                  {t('billing.importXml.result.imported', {
                    count: result.count
                  })}
                </h6>
                <Table size="sm" bordered hover responsive>
                  <thead className="bg-body-tertiary">
                    <tr>
                      <th>{t('billing.importXml.result.colNumber')}</th>
                      <th>{t('billing.importXml.result.colType')}</th>
                      <th>{t('billing.importXml.result.colDate')}</th>
                      <th className="text-end">
                        {t('billing.importXml.result.colAmount')}
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {result.invoices.map((invoice: ImportedInvoiceSummary) => (
                      <tr key={invoice.id}>
                        <td>{invoice.number}</td>
                        <td>
                          <small>
                            {DOCUMENT_TYPE_LABELS[invoice.documentType] ||
                              invoice.documentType}
                          </small>
                        </td>
                        <td>{formatItalianDate(invoice.date)}</td>
                        <td className="text-end fw-semibold">
                          {formatCurrency(invoice.totalAmount)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              </div>
            )}

            {result.skipped && result.skipped.length > 0 && (
              <div className="mb-3">
                <h6 className="mb-2">
                  <FontAwesomeIcon
                    icon="forward"
                    className="me-2 text-warning"
                  />
                  {t('billing.importXml.result.skipped', {
                    count: result.skipped.length
                  })}
                </h6>
                <Table size="sm" bordered responsive className="table-warning">
                  <thead>
                    <tr>
                      <th>{t('billing.importXml.result.colNumber')}</th>
                      <th>{t('billing.importXml.result.colReason')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {result.skipped.map(
                      (skipped: SkippedInvoice, idx: number) => (
                        <tr key={idx}>
                          <td>{skipped.number}</td>
                          <td>{skipped.reason}</td>
                        </tr>
                      )
                    )}
                  </tbody>
                </Table>
              </div>
            )}

            <div className="d-flex gap-2 justify-content-end mt-4">
              <Button variant="outline-secondary" onClick={handleImportAnother}>
                <FontAwesomeIcon icon="plus" className="me-1" />
                {t('billing.importXml.actions.importAnother')}
              </Button>
              <Button variant="primary" onClick={handleClose}>
                {t('billing.importXml.actions.close')}
              </Button>
            </div>
          </div>
        ) : (
          <Form onSubmit={handleSubmit}>
            <Tab.Container
              activeKey={activeTab}
              onSelect={k => k && setActiveTab(k)}
            >
              <Nav variant="tabs" className="mb-3">
                <Nav.Item>
                  <Nav.Link eventKey="file">
                    <FontAwesomeIcon icon="upload" className="me-1" />
                    {t('billing.importXml.tabs.file')}
                  </Nav.Link>
                </Nav.Item>
                <Nav.Item>
                  <Nav.Link eventKey="paste">
                    <FontAwesomeIcon icon="paste" className="me-1" />
                    {t('billing.importXml.tabs.paste')}
                  </Nav.Link>
                </Nav.Item>
              </Nav>

              <Tab.Content>
                <Tab.Pane eventKey="file">
                  <Form.Group className="mb-3">
                    <Form.Label>{t('billing.importXml.file.label')}</Form.Label>
                    <Form.Control
                      ref={fileInputRef}
                      type="file"
                      accept=".xml,application/xml,text/xml"
                      onChange={handleFileChange}
                    />
                    <Form.Text className="text-body-secondary">
                      {t('billing.importXml.file.help')}
                    </Form.Text>
                  </Form.Group>
                  {fileName && (
                    <Alert variant="info" className="py-2">
                      <FontAwesomeIcon icon="file-code" className="me-2" />
                      {t('billing.importXml.file.selected')}{' '}
                      <strong>{fileName}</strong>
                    </Alert>
                  )}
                </Tab.Pane>

                <Tab.Pane eventKey="paste">
                  <Form.Group className="mb-3">
                    <Form.Label>
                      {t('billing.importXml.paste.label')}
                    </Form.Label>
                    <Form.Control
                      as="textarea"
                      rows={10}
                      value={xmlContent}
                      onChange={handleXmlChange}
                      placeholder={t('billing.importXml.paste.placeholder')}
                      style={{ fontFamily: 'monospace', fontSize: '0.85rem' }}
                    />
                    <Form.Text className="text-body-secondary">
                      {t('billing.importXml.paste.help')}
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
                label={t('billing.importXml.skipDuplicates.label')}
                checked={skipDuplicates}
                onChange={e => setSkipDuplicates(e.target.checked)}
              />
              <Form.Text className="text-body-secondary">
                {t('billing.importXml.skipDuplicates.help')}
              </Form.Text>
            </Form.Group>

            <div className="d-flex gap-2 justify-content-end">
              <Button
                variant="outline-secondary"
                onClick={handleClose}
                disabled={isLoading}
              >
                {t('billing.importXml.actions.cancel')}
              </Button>
              <Button
                variant="primary"
                type="submit"
                disabled={isLoading || !xmlContent.trim()}
              >
                {isLoading ? (
                  <>
                    <Spinner animation="border" size="sm" className="me-1" />
                    {t('billing.importXml.actions.importing')}
                  </>
                ) : (
                  <>
                    <FontAwesomeIcon icon="file-import" className="me-1" />
                    {t('billing.importXml.actions.import')}
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

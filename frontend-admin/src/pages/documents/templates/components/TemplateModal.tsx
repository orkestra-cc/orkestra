import { useState, useEffect } from 'react';
import {
  Modal,
  Button,
  Form,
  Tab,
  Nav,
  Row,
  Col,
  Alert,
  Spinner,
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faTimes, faCode, faPaintBrush, faCog, faEye } from '@fortawesome/free-solid-svg-icons';
import {
  useCreateTemplateMutation,
  useUpdateTemplateMutation,
  useGetTemplateQuery,
  usePreviewHTMLFromContentMutation,
} from '../../../../store/api/documentsApi';
import {
  TemplateListItem,
  CreateTemplateInput,
  UpdateTemplateInput,
  TemplateType,
  PageMargins,
  TEMPLATE_TYPE_LABELS,
  PAGE_SIZE_LABELS,
  PAGE_ORIENTATION_LABELS,
  DEFAULT_MARGINS,
  DEFAULT_PAGE_SIZE,
  DEFAULT_ORIENTATION,
} from '../../../../types/documents';

interface TemplateModalProps {
  show: boolean;
  onHide: () => void;
  template?: TemplateListItem | null;
  onSuccess?: () => void;
}

const TemplateModal: React.FC<TemplateModalProps> = ({
  show,
  onHide,
  template,
  onSuccess,
}) => {
  const isEditMode = !!template;
  const [activeTab, setActiveTab] = useState('general');
  const [error, setError] = useState<string>('');

  // Form state
  const [formData, setFormData] = useState<CreateTemplateInput>({
    name: '',
    description: '',
    type: 'invoice',
    htmlContent: '',
    cssContent: '',
    pageSize: DEFAULT_PAGE_SIZE,
    orientation: DEFAULT_ORIENTATION,
    margins: DEFAULT_MARGINS,
    headerHtml: '',
    footerHtml: '',
  });

  // Preview state
  const [previewHtml, setPreviewHtml] = useState<string>('');

  // API hooks
  const { data: fullTemplate, isLoading: isLoadingTemplate } = useGetTemplateQuery(
    template?.id || '',
    { skip: !template?.id || !show }
  );
  const [createTemplate, { isLoading: isCreating }] = useCreateTemplateMutation();
  const [updateTemplate, { isLoading: isUpdating }] = useUpdateTemplateMutation();
  const [previewMutation, { isLoading: isPreviewing }] = usePreviewHTMLFromContentMutation();

  const isLoading = isCreating || isUpdating;

  // Populate form when editing
  useEffect(() => {
    if (fullTemplate && show) {
      setFormData({
        name: fullTemplate.name,
        description: fullTemplate.description || '',
        type: fullTemplate.type,
        htmlContent: fullTemplate.htmlContent,
        cssContent: fullTemplate.cssContent || '',
        pageSize: fullTemplate.pageSize,
        orientation: fullTemplate.orientation,
        margins: fullTemplate.margins,
        headerHtml: fullTemplate.headerHtml || '',
        footerHtml: fullTemplate.footerHtml || '',
      });
    }
  }, [fullTemplate, show]);

  // Reset form when modal closes
  useEffect(() => {
    if (!show) {
      setFormData({
        name: '',
        description: '',
        type: 'invoice',
        htmlContent: '',
        cssContent: '',
        pageSize: DEFAULT_PAGE_SIZE,
        orientation: DEFAULT_ORIENTATION,
        margins: DEFAULT_MARGINS,
        headerHtml: '',
        footerHtml: '',
      });
      setError('');
      setActiveTab('general');
      setPreviewHtml('');
    }
  }, [show]);

  const handleChange = (
    e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>
  ) => {
    const { name, value } = e.target;
    setFormData((prev) => ({
      ...prev,
      [name]: value,
    }));
  };

  const handleMarginChange = (field: keyof PageMargins, value: string) => {
    const numValue = parseFloat(value) || 0;
    setFormData((prev) => ({
      ...prev,
      margins: {
        ...prev.margins!,
        [field]: numValue,
      },
    }));
  };

  const handlePreview = async () => {
    if (!formData.htmlContent) {
      setError('Inserisci il contenuto HTML prima di visualizzare l\'anteprima');
      setActiveTab('html');
      return;
    }

    try {
      const html = await previewMutation({
        htmlContent: formData.htmlContent,
        cssContent: formData.cssContent,
        data: getSampleData(formData.type),
      }).unwrap();
      setPreviewHtml(html);
      setActiveTab('preview');
    } catch (err) {
      setError('Errore durante la generazione dell\'anteprima');
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    // Validation
    if (!formData.name.trim()) {
      setError('Il nome è obbligatorio');
      setActiveTab('general');
      return;
    }
    if (!formData.htmlContent.trim()) {
      setError('Il contenuto HTML è obbligatorio');
      setActiveTab('html');
      return;
    }

    try {
      if (isEditMode && template) {
        const updateData: UpdateTemplateInput = {
          name: formData.name,
          description: formData.description,
          htmlContent: formData.htmlContent,
          cssContent: formData.cssContent,
          pageSize: formData.pageSize,
          orientation: formData.orientation,
          margins: formData.margins,
          headerHtml: formData.headerHtml,
          footerHtml: formData.footerHtml,
        };
        await updateTemplate({ id: template.id, data: updateData }).unwrap();
      } else {
        await createTemplate(formData).unwrap();
      }
      onSuccess?.();
      onHide();
    } catch (err: any) {
      setError(err?.data?.error || 'Errore durante il salvataggio del template');
    }
  };

  return (
    <Modal show={show} onHide={onHide} size="xl" centered>
      <Modal.Header>
        <Modal.Title>{isEditMode ? 'Modifica Template' : 'Nuovo Template'}</Modal.Title>
        <Button variant="link" className="p-0 text-decoration-none" onClick={onHide}>
          <FontAwesomeIcon icon={faTimes} />
        </Button>
      </Modal.Header>

      <Modal.Body>
        {error && (
          <Alert variant="danger" dismissible onClose={() => setError('')}>
            {error}
          </Alert>
        )}

        {isLoadingTemplate ? (
          <div className="text-center py-5">
            <Spinner animation="border" variant="primary" />
          </div>
        ) : (
          <Form onSubmit={handleSubmit}>
            <Tab.Container activeKey={activeTab} onSelect={(k) => setActiveTab(k || 'general')}>
              <Nav variant="pills" className="mb-3">
                <Nav.Item>
                  <Nav.Link eventKey="general">
                    <FontAwesomeIcon icon={faCog} className="me-1" />
                    Generale
                  </Nav.Link>
                </Nav.Item>
                <Nav.Item>
                  <Nav.Link eventKey="html">
                    <FontAwesomeIcon icon={faCode} className="me-1" />
                    HTML
                  </Nav.Link>
                </Nav.Item>
                <Nav.Item>
                  <Nav.Link eventKey="css">
                    <FontAwesomeIcon icon={faPaintBrush} className="me-1" />
                    CSS
                  </Nav.Link>
                </Nav.Item>
                <Nav.Item>
                  <Nav.Link eventKey="preview">
                    <FontAwesomeIcon icon={faEye} className="me-1" />
                    Anteprima
                  </Nav.Link>
                </Nav.Item>
              </Nav>

              <Tab.Content>
                {/* General Tab */}
                <Tab.Pane eventKey="general">
                  <Row className="g-3">
                    <Col md={6}>
                      <Form.Group>
                        <Form.Label>Nome *</Form.Label>
                        <Form.Control
                          type="text"
                          name="name"
                          value={formData.name}
                          onChange={handleChange}
                          placeholder="Es: Fattura Standard"
                          required
                        />
                      </Form.Group>
                    </Col>
                    <Col md={6}>
                      <Form.Group>
                        <Form.Label>Tipo *</Form.Label>
                        <Form.Select
                          name="type"
                          value={formData.type}
                          onChange={handleChange}
                          disabled={isEditMode}
                        >
                          {Object.entries(TEMPLATE_TYPE_LABELS).map(([value, label]) => (
                            <option key={value} value={value}>
                              {label}
                            </option>
                          ))}
                        </Form.Select>
                      </Form.Group>
                    </Col>
                    <Col md={12}>
                      <Form.Group>
                        <Form.Label>Descrizione</Form.Label>
                        <Form.Control
                          as="textarea"
                          rows={2}
                          name="description"
                          value={formData.description}
                          onChange={handleChange}
                          placeholder="Breve descrizione del template..."
                        />
                      </Form.Group>
                    </Col>
                    <Col md={4}>
                      <Form.Group>
                        <Form.Label>Formato pagina</Form.Label>
                        <Form.Select
                          name="pageSize"
                          value={formData.pageSize}
                          onChange={handleChange}
                        >
                          {Object.entries(PAGE_SIZE_LABELS).map(([value, label]) => (
                            <option key={value} value={value}>
                              {label}
                            </option>
                          ))}
                        </Form.Select>
                      </Form.Group>
                    </Col>
                    <Col md={4}>
                      <Form.Group>
                        <Form.Label>Orientamento</Form.Label>
                        <Form.Select
                          name="orientation"
                          value={formData.orientation}
                          onChange={handleChange}
                        >
                          {Object.entries(PAGE_ORIENTATION_LABELS).map(([value, label]) => (
                            <option key={value} value={value}>
                              {label}
                            </option>
                          ))}
                        </Form.Select>
                      </Form.Group>
                    </Col>
                  </Row>
                  <Row className="g-3 mt-2">
                    <Col xs={12}>
                      <Form.Label>Margini (mm)</Form.Label>
                    </Col>
                    <Col md={3}>
                      <Form.Group>
                        <Form.Label className="small text-muted">Sopra</Form.Label>
                        <Form.Control
                          type="number"
                          value={formData.margins?.top || 20}
                          onChange={(e) => handleMarginChange('top', e.target.value)}
                          min={0}
                          max={100}
                        />
                      </Form.Group>
                    </Col>
                    <Col md={3}>
                      <Form.Group>
                        <Form.Label className="small text-muted">Sotto</Form.Label>
                        <Form.Control
                          type="number"
                          value={formData.margins?.bottom || 20}
                          onChange={(e) => handleMarginChange('bottom', e.target.value)}
                          min={0}
                          max={100}
                        />
                      </Form.Group>
                    </Col>
                    <Col md={3}>
                      <Form.Group>
                        <Form.Label className="small text-muted">Sinistra</Form.Label>
                        <Form.Control
                          type="number"
                          value={formData.margins?.left || 20}
                          onChange={(e) => handleMarginChange('left', e.target.value)}
                          min={0}
                          max={100}
                        />
                      </Form.Group>
                    </Col>
                    <Col md={3}>
                      <Form.Group>
                        <Form.Label className="small text-muted">Destra</Form.Label>
                        <Form.Control
                          type="number"
                          value={formData.margins?.right || 20}
                          onChange={(e) => handleMarginChange('right', e.target.value)}
                          min={0}
                          max={100}
                        />
                      </Form.Group>
                    </Col>
                  </Row>
                </Tab.Pane>

                {/* HTML Tab */}
                <Tab.Pane eventKey="html">
                  <Form.Group>
                    <Form.Label>Contenuto HTML *</Form.Label>
                    <Form.Control
                      as="textarea"
                      rows={20}
                      name="htmlContent"
                      value={formData.htmlContent}
                      onChange={handleChange}
                      placeholder="<!DOCTYPE html>&#10;<html>&#10;<head>...</head>&#10;<body>...</body>&#10;</html>"
                      style={{ fontFamily: 'monospace', fontSize: '13px' }}
                      required
                    />
                    <Form.Text className="text-muted">
                      Usa le variabili Go template come {'{{.number}}'}, {'{{.seller.name}}'}, {'{{range .lines}}...{{end}}'}
                    </Form.Text>
                  </Form.Group>
                </Tab.Pane>

                {/* CSS Tab */}
                <Tab.Pane eventKey="css">
                  <Form.Group>
                    <Form.Label>Stili CSS</Form.Label>
                    <Form.Control
                      as="textarea"
                      rows={20}
                      name="cssContent"
                      value={formData.cssContent}
                      onChange={handleChange}
                      placeholder="body { font-family: Arial, sans-serif; }&#10;.header { ... }"
                      style={{ fontFamily: 'monospace', fontSize: '13px' }}
                    />
                    <Form.Text className="text-muted">
                      Gli stili CSS verranno inclusi automaticamente nel documento
                    </Form.Text>
                  </Form.Group>
                </Tab.Pane>

                {/* Preview Tab */}
                <Tab.Pane eventKey="preview">
                  <div className="d-flex justify-content-between align-items-center mb-3">
                    <span className="text-muted">Anteprima con dati di esempio</span>
                    <Button
                      variant="outline-primary"
                      size="sm"
                      onClick={handlePreview}
                      disabled={isPreviewing}
                    >
                      {isPreviewing ? (
                        <Spinner animation="border" size="sm" className="me-1" />
                      ) : (
                        <FontAwesomeIcon icon={faEye} className="me-1" />
                      )}
                      Aggiorna anteprima
                    </Button>
                  </div>
                  {previewHtml ? (
                    <div
                      className="border rounded p-3 bg-white"
                      style={{ minHeight: '500px', overflow: 'auto' }}
                    >
                      <iframe
                        srcDoc={previewHtml}
                        style={{ width: '100%', height: '600px', border: 'none' }}
                        title="Template Preview"
                      />
                    </div>
                  ) : (
                    <div className="text-center py-5 text-muted">
                      <FontAwesomeIcon icon={faEye} size="3x" className="mb-3" />
                      <p>Clicca "Aggiorna anteprima" per visualizzare il template</p>
                    </div>
                  )}
                </Tab.Pane>
              </Tab.Content>
            </Tab.Container>
          </Form>
        )}
      </Modal.Body>

      <Modal.Footer>
        <Button variant="secondary" onClick={onHide}>
          Annulla
        </Button>
        <Button variant="primary" onClick={handleSubmit} disabled={isLoading}>
          {isLoading ? (
            <Spinner animation="border" size="sm" className="me-1" />
          ) : null}
          {isEditMode ? 'Salva modifiche' : 'Crea template'}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

// Sample data for preview
function getSampleData(type: TemplateType): Record<string, unknown> {
  const commonData = {
    number: 'INV-2024-001',
    date: new Date(),
  };

  switch (type) {
    case 'invoice':
      return {
        ...commonData,
        seller: {
          name: 'Azienda Demo S.r.l.',
          address: 'Via Roma 123, 00100 Roma RM',
          vatNumber: 'IT12345678901',
          pec: 'azienda@pec.it',
        },
        buyer: {
          name: 'Cliente Esempio S.p.A.',
          address: 'Via Milano 456, 20100 Milano MI',
          vatNumber: 'IT09876543210',
          fiscalCode: 'RSSMRA80A01H501U',
        },
        lines: [
          { Description: 'Servizio consulenza', Quantity: 10, UnitPrice: 50.0, VATRate: 22, TotalPrice: 500.0 },
          { Description: 'Sviluppo software', Quantity: 1, UnitPrice: 1500.0, VATRate: 22, TotalPrice: 1500.0 },
        ],
        totalTaxable: 2000.0,
        totalVAT: 440.0,
        totalAmount: 2440.0,
        paymentTerms: 'Bonifico bancario entro 30 giorni',
        notes: 'Fattura elettronica ai sensi dell\'art. 1, comma 3, D.Lgs. 127/2015',
      };
    case 'offer':
      return {
        ...commonData,
        validUntil: new Date(Date.now() + 30 * 24 * 60 * 60 * 1000),
        subject: 'Proposta per sviluppo applicazione web',
        company: {
          name: 'Azienda Demo S.r.l.',
          address: 'Via Roma 123, 00100 Roma RM',
          vatNumber: 'IT12345678901',
          email: 'info@aziendademo.it',
          phone: '+39 06 1234567',
        },
        customer: {
          name: 'Cliente Esempio S.p.A.',
          address: 'Via Milano 456, 20100 Milano MI',
          email: 'info@cliente.it',
        },
        items: [
          { Description: 'Analisi e progettazione', Quantity: 1, UnitPrice: 2000.0, Total: 2000.0 },
          { Description: 'Sviluppo frontend', Quantity: 1, UnitPrice: 5000.0, Total: 5000.0 },
          { Description: 'Sviluppo backend', Quantity: 1, UnitPrice: 5000.0, Total: 5000.0 },
        ],
        subtotal: 12000.0,
        tax: 2640.0,
        total: 14640.0,
        notes: 'Il preventivo ha validità 30 giorni dalla data di emissione.',
      };
    default:
      return commonData;
  }
}

export default TemplateModal;

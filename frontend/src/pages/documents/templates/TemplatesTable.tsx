import { useState } from 'react';
import {
  Card,
  Button,
  Badge,
  Dropdown,
  Form,
  InputGroup,
  Spinner,
  Alert,
  Row,
  Col,
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faPlus,
  faSearch,
  faEllipsisV,
  faEdit,
  faTrash,
  faCopy,
  faStar,
  faFilter,
  faFileAlt,
} from '@fortawesome/free-solid-svg-icons';
import {
  useGetTemplatesQuery,
  useDeleteTemplateMutation,
  useSetDefaultTemplateMutation,
  useDuplicateTemplateMutation,
} from '../../../store/api/documentsApi';
import {
  TemplateListItem,
  TemplateType,
  TEMPLATE_TYPE_LABELS,
  TEMPLATE_TYPE_COLORS,
  PAGE_SIZE_LABELS,
  PAGE_ORIENTATION_LABELS,
  formatDateTime,
} from '../../../types/documents';
import TemplateModal from './components/TemplateModal';
import DeleteConfirmModal from './components/DeleteConfirmModal';

const TemplatesTable: React.FC = () => {
  // Filters
  const [search, setSearch] = useState('');
  const [typeFilter, setTypeFilter] = useState<TemplateType | ''>('');
  const [page, setPage] = useState(1);
  const pageSize = 10;

  // Modal states
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [editTemplate, setEditTemplate] = useState<TemplateListItem | null>(null);
  const [deleteTemplate, setDeleteTemplate] = useState<TemplateListItem | null>(null);
  const [duplicateName, setDuplicateName] = useState('');
  const [showDuplicateModal, setShowDuplicateModal] = useState(false);
  const [templateToDuplicate, setTemplateToDuplicate] = useState<TemplateListItem | null>(null);

  // API hooks
  const { data, isLoading, error, refetch } = useGetTemplatesQuery({
    page,
    pageSize,
    search: search || undefined,
    type: typeFilter || undefined,
    isActive: true,
  });

  const [deleteTemplateMutation, { isLoading: isDeleting }] = useDeleteTemplateMutation();
  const [setDefaultMutation, { isLoading: isSettingDefault }] = useSetDefaultTemplateMutation();
  const [duplicateMutation, { isLoading: isDuplicating }] = useDuplicateTemplateMutation();

  // Handlers
  const handleDelete = async () => {
    if (!deleteTemplate) return;
    try {
      await deleteTemplateMutation(deleteTemplate.id).unwrap();
      setDeleteTemplate(null);
    } catch (err) {
      console.error('Failed to delete template:', err);
    }
  };

  const handleSetDefault = async (template: TemplateListItem) => {
    try {
      await setDefaultMutation(template.id).unwrap();
    } catch (err) {
      console.error('Failed to set default template:', err);
    }
  };

  const handleDuplicate = async () => {
    if (!templateToDuplicate || !duplicateName) return;
    try {
      await duplicateMutation({
        id: templateToDuplicate.id,
        data: { name: duplicateName },
      }).unwrap();
      setShowDuplicateModal(false);
      setTemplateToDuplicate(null);
      setDuplicateName('');
    } catch (err) {
      console.error('Failed to duplicate template:', err);
    }
  };

  const openDuplicateModal = (template: TemplateListItem) => {
    setTemplateToDuplicate(template);
    setDuplicateName(`${template.name} (Copia)`);
    setShowDuplicateModal(true);
  };

  // Render
  if (error) {
    return (
      <Card>
        <Card.Body>
          <Alert variant="danger">
            Errore nel caricamento dei template. Riprova più tardi.
          </Alert>
        </Card.Body>
      </Card>
    );
  }

  return (
    <>
      <Card>
        <Card.Header className="border-bottom border-200">
          <Row className="align-items-center g-2">
            <Col xs={12} md={6} lg={4}>
              <InputGroup>
                <InputGroup.Text>
                  <FontAwesomeIcon icon={faSearch} />
                </InputGroup.Text>
                <Form.Control
                  type="text"
                  placeholder="Cerca template..."
                  value={search}
                  onChange={(e) => {
                    setSearch(e.target.value);
                    setPage(1);
                  }}
                />
              </InputGroup>
            </Col>
            <Col xs={12} md={4} lg={3}>
              <InputGroup>
                <InputGroup.Text>
                  <FontAwesomeIcon icon={faFilter} />
                </InputGroup.Text>
                <Form.Select
                  value={typeFilter}
                  onChange={(e) => {
                    setTypeFilter(e.target.value as TemplateType | '');
                    setPage(1);
                  }}
                >
                  <option value="">Tutti i tipi</option>
                  {Object.entries(TEMPLATE_TYPE_LABELS).map(([value, label]) => (
                    <option key={value} value={value}>
                      {label}
                    </option>
                  ))}
                </Form.Select>
              </InputGroup>
            </Col>
            <Col xs={12} md={2} lg={5} className="text-end">
              <Button variant="primary" onClick={() => setShowCreateModal(true)}>
                <FontAwesomeIcon icon={faPlus} className="me-1" />
                Nuovo Template
              </Button>
            </Col>
          </Row>
        </Card.Header>

        <Card.Body className="p-0">
          {isLoading ? (
            <div className="text-center py-5">
              <Spinner animation="border" variant="primary" />
              <p className="mt-2 text-muted">Caricamento template...</p>
            </div>
          ) : data?.templates?.length === 0 ? (
            <div className="text-center py-5">
              <FontAwesomeIcon icon={faFileAlt} className="text-muted mb-3" size="3x" />
              <p className="text-muted">Nessun template trovato</p>
              <Button variant="outline-primary" onClick={() => setShowCreateModal(true)}>
                Crea il primo template
              </Button>
            </div>
          ) : (
            <div className="table-responsive">
              <table className="table table-hover mb-0">
                <thead className="bg-body-tertiary">
                  <tr>
                    <th>Nome</th>
                    <th>Tipo</th>
                    <th>Formato</th>
                    <th>Stato</th>
                    <th>Ultima modifica</th>
                    <th className="text-end">Azioni</th>
                  </tr>
                </thead>
                <tbody>
                  {data?.templates?.map((template) => (
                    <tr key={template.id}>
                      <td>
                        <div className="d-flex align-items-center">
                          <div>
                            <span className="fw-semi-bold">
                              {template.name}
                              {template.isDefault && (
                                <FontAwesomeIcon
                                  icon={faStar}
                                  className="ms-1 text-warning"
                                  title="Template predefinito"
                                />
                              )}
                            </span>
                            {template.description && (
                              <p className="mb-0 fs-10 text-muted">{template.description}</p>
                            )}
                          </div>
                        </div>
                      </td>
                      <td>
                        <Badge bg={TEMPLATE_TYPE_COLORS[template.type]}>
                          {TEMPLATE_TYPE_LABELS[template.type]}
                        </Badge>
                      </td>
                      <td>
                        <span className="text-nowrap">
                          {PAGE_SIZE_LABELS[template.pageSize]} •{' '}
                          {PAGE_ORIENTATION_LABELS[template.orientation]}
                        </span>
                      </td>
                      <td>
                        {template.isBuiltIn ? (
                          <Badge bg="info" className="me-1">
                            Built-in
                          </Badge>
                        ) : null}
                        <Badge bg={template.isActive ? 'success' : 'secondary'}>
                          {template.isActive ? 'Attivo' : 'Inattivo'}
                        </Badge>
                      </td>
                      <td className="text-nowrap">{formatDateTime(template.updatedAt)}</td>
                      <td className="text-end">
                        <Dropdown align="end">
                          <Dropdown.Toggle
                            variant="link"
                            className="text-decoration-none p-0"
                            id={`dropdown-${template.id}`}
                          >
                            <FontAwesomeIcon icon={faEllipsisV} />
                          </Dropdown.Toggle>
                          <Dropdown.Menu>
                            <Dropdown.Item onClick={() => setEditTemplate(template)}>
                              <FontAwesomeIcon icon={faEdit} className="me-2" />
                              Modifica
                            </Dropdown.Item>
                            <Dropdown.Item onClick={() => openDuplicateModal(template)}>
                              <FontAwesomeIcon icon={faCopy} className="me-2" />
                              Duplica
                            </Dropdown.Item>
                            {!template.isDefault && (
                              <Dropdown.Item
                                onClick={() => handleSetDefault(template)}
                                disabled={isSettingDefault}
                              >
                                <FontAwesomeIcon icon={faStar} className="me-2" />
                                Imposta predefinito
                              </Dropdown.Item>
                            )}
                            <Dropdown.Divider />
                            <Dropdown.Item
                              className="text-danger"
                              onClick={() => setDeleteTemplate(template)}
                              disabled={template.isBuiltIn}
                            >
                              <FontAwesomeIcon icon={faTrash} className="me-2" />
                              {template.isBuiltIn ? 'Non eliminabile' : 'Elimina'}
                            </Dropdown.Item>
                          </Dropdown.Menu>
                        </Dropdown>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card.Body>

        {data && data.totalPages > 1 && (
          <Card.Footer className="d-flex justify-content-between align-items-center">
            <span className="text-muted fs-10">
              Pagina {data.page} di {data.totalPages} ({data.total} template totali)
            </span>
            <div>
              <Button
                variant="outline-secondary"
                size="sm"
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
                className="me-1"
              >
                Precedente
              </Button>
              <Button
                variant="outline-secondary"
                size="sm"
                disabled={page >= data.totalPages}
                onClick={() => setPage(page + 1)}
              >
                Successivo
              </Button>
            </div>
          </Card.Footer>
        )}
      </Card>

      {/* Create/Edit Modal */}
      <TemplateModal
        show={showCreateModal || !!editTemplate}
        onHide={() => {
          setShowCreateModal(false);
          setEditTemplate(null);
        }}
        template={editTemplate}
        onSuccess={() => {
          setShowCreateModal(false);
          setEditTemplate(null);
          refetch();
        }}
      />

      {/* Delete Confirmation Modal */}
      <DeleteConfirmModal
        show={!!deleteTemplate}
        onHide={() => setDeleteTemplate(null)}
        onConfirm={handleDelete}
        isLoading={isDeleting}
        templateName={deleteTemplate?.name || ''}
      />

      {/* Duplicate Modal */}
      <DeleteConfirmModal
        show={showDuplicateModal}
        onHide={() => {
          setShowDuplicateModal(false);
          setTemplateToDuplicate(null);
          setDuplicateName('');
        }}
        onConfirm={handleDuplicate}
        isLoading={isDuplicating}
        templateName=""
        title="Duplica Template"
        body={
          <Form.Group>
            <Form.Label>Nome del nuovo template</Form.Label>
            <Form.Control
              type="text"
              value={duplicateName}
              onChange={(e) => setDuplicateName(e.target.value)}
              placeholder="Inserisci il nome..."
            />
          </Form.Group>
        }
        confirmText="Duplica"
        confirmVariant="primary"
      />
    </>
  );
};

export default TemplatesTable;

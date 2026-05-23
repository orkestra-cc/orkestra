import { useState, useCallback, useRef, useEffect } from 'react';
import {
  Row,
  Col,
  Card,
  Button,
  Form,
  Table,
  Badge,
  Spinner,
  Alert,
  Modal,
  Dropdown,
  Accordion
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faUpload,
  faEye,
  faPen,
  faTrash,
  faEllipsisV,
  faFileAlt,
  faSearch
} from '@fortawesome/free-solid-svg-icons';
import { Trans, useTranslation } from 'react-i18next';
import {
  useListDocumentsQuery,
  useUploadDocumentMutation,
  useUpdateDocumentMutation,
  useGetDocumentChunksQuery,
  useDeleteDocumentMutation
} from '../../../store/api/ragApi';
import { useListAIModelsQuery } from '../../../store/api/aiModelsApi';
import type { RagDocument } from '../../../types/rag';

const statusColors: Record<string, string> = {
  pending: 'warning',
  processing: 'info',
  completed: 'success',
  failed: 'danger'
};

const formatSize = (bytes: number) => {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
};

// ── Upload Modal ─────────────────────────────────────────────────────────────

interface UploadModalProps {
  show: boolean;
  onHide: () => void;
}

const UploadModal: React.FC<UploadModalProps> = ({ show, onHide }) => {
  const { t } = useTranslation();
  const fileRef = useRef<HTMLInputElement>(null);
  const [title, setTitle] = useState('');
  const [iso, setIso] = useState('');
  const [version, setVersion] = useState('');
  const [category, setCategory] = useState('');
  const [llmModel, setLlmModel] = useState('');
  const [error, setError] = useState('');
  const [uploadDocument, { isLoading }] = useUploadDocumentMutation();
  const { data: modelsData } = useListAIModelsQuery({ type: 'llm' });
  const llmModels = modelsData?.models?.filter(m => m.isActive) ?? [];

  const reset = () => {
    setTitle('');
    setIso('');
    setVersion('');
    setCategory('');
    setLlmModel('');
    setError('');
    if (fileRef.current) fileRef.current.value = '';
  };

  const handleClose = () => {
    reset();
    onHide();
  };

  const handleSubmit = async () => {
    const file = fileRef.current?.files?.[0];
    if (!file) {
      setError(t('graph.documents.uploadModal.errorNoFile'));
      return;
    }

    const formData = new FormData();
    formData.append('file', file);
    formData.append('title', title || file.name);
    if (iso) formData.append('isoStandard', iso);
    if (version) formData.append('version', version);
    if (category) formData.append('documentCategory', category);
    if (llmModel) formData.append('llmModelUuid', llmModel);

    try {
      await uploadDocument(formData).unwrap();
      handleClose();
    } catch (err: unknown) {
      const msg = (err as { data?: { message?: string } })?.data?.message;
      setError(msg || t('graph.documents.uploadModal.errorGeneric'));
    }
  };

  return (
    <Modal show={show} onHide={handleClose} centered backdrop="static">
      <Modal.Header closeButton>
        <Modal.Title className="fs-9">
          {t('graph.documents.uploadModal.title')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {error && (
          <Alert
            variant="danger"
            dismissible
            onClose={() => setError('')}
            className="py-2"
          >
            {error}
          </Alert>
        )}
        <Form.Group className="mb-3">
          <Form.Label className="small">
            {t('graph.documents.uploadModal.fileLabel')}{' '}
            <span className="text-danger">*</span>
          </Form.Label>
          <Form.Control
            type="file"
            size="sm"
            ref={fileRef}
            accept=".pdf,.txt,.md,.text"
          />
        </Form.Group>
        <Form.Group className="mb-3">
          <Form.Label className="small">
            {t('graph.documents.uploadModal.titleLabel')}
          </Form.Label>
          <Form.Control
            size="sm"
            value={title}
            onChange={e => setTitle(e.target.value)}
            placeholder={t('graph.documents.uploadModal.titlePlaceholder')}
          />
        </Form.Group>
        <Row className="g-2 mb-3">
          <Col>
            <Form.Group>
              <Form.Label className="small">
                {t('graph.documents.uploadModal.isoLabel')}
              </Form.Label>
              <Form.Control
                size="sm"
                value={iso}
                onChange={e => setIso(e.target.value)}
                placeholder={t('graph.documents.uploadModal.isoPlaceholder')}
              />
            </Form.Group>
          </Col>
          <Col>
            <Form.Group>
              <Form.Label className="small">
                {t('graph.documents.uploadModal.versionLabel')}
              </Form.Label>
              <Form.Control
                size="sm"
                value={version}
                onChange={e => setVersion(e.target.value)}
                placeholder={t(
                  'graph.documents.uploadModal.versionPlaceholder'
                )}
              />
            </Form.Group>
          </Col>
        </Row>
        <Form.Group className="mb-3">
          <Form.Label className="small">
            {t('graph.documents.uploadModal.categoryLabel')}
          </Form.Label>
          <Form.Select
            size="sm"
            value={category}
            onChange={e => setCategory(e.target.value)}
          >
            <option value="">
              {t('graph.documents.uploadModal.category.auto')}
            </option>
            <option value="iso">
              {t('graph.documents.uploadModal.category.iso')}
            </option>
            <option value="law">
              {t('graph.documents.uploadModal.category.law')}
            </option>
            <option value="regulation">
              {t('graph.documents.uploadModal.category.regulation')}
            </option>
            <option value="generic">
              {t('graph.documents.uploadModal.category.generic')}
            </option>
          </Form.Select>
        </Form.Group>
        <Form.Group>
          <Form.Label className="small">
            {t('graph.documents.uploadModal.llmLabel')}
          </Form.Label>
          <Form.Select
            size="sm"
            value={llmModel}
            onChange={e => setLlmModel(e.target.value)}
          >
            <option value="">
              {t('graph.documents.uploadModal.llmDefaultOption')}
            </option>
            {llmModels.map(m => (
              <option key={m.uuid} value={m.uuid}>
                {t('graph.documents.uploadModal.llmOption', {
                  name: m.name,
                  modelName: m.modelName
                })}
                {m.isDefault
                  ? t('graph.documents.uploadModal.llmDefaultSuffix')
                  : ''}
              </option>
            ))}
          </Form.Select>
          <Form.Text className="text-muted">
            {t('graph.documents.uploadModal.llmHelp')}
          </Form.Text>
        </Form.Group>
      </Modal.Body>
      <Modal.Footer>
        <Button
          variant="secondary"
          size="sm"
          onClick={handleClose}
          disabled={isLoading}
        >
          {t('graph.documents.uploadModal.cancel')}
        </Button>
        <Button
          variant="primary"
          size="sm"
          onClick={handleSubmit}
          disabled={isLoading}
        >
          {isLoading ? (
            <>
              <Spinner size="sm" className="me-1" />{' '}
              {t('graph.documents.uploadModal.submitting')}
            </>
          ) : (
            t('graph.documents.uploadModal.submit')
          )}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

// ── Edit Modal ───────────────────────────────────────────────────────────────

interface EditModalProps {
  show: boolean;
  onHide: () => void;
  document: RagDocument | null;
}

const EditModal: React.FC<EditModalProps> = ({ show, onHide, document }) => {
  const { t } = useTranslation();
  const [title, setTitle] = useState('');
  const [iso, setIso] = useState('');
  const [version, setVersion] = useState('');
  const [error, setError] = useState('');
  const [updateDocument, { isLoading }] = useUpdateDocumentMutation();

  useEffect(() => {
    if (document && show) {
      setTitle(document.title);
      setIso(document.isoStandard || '');
      setVersion(document.version || '');
      setError('');
    }
  }, [document, show]);

  const handleClose = () => {
    setError('');
    onHide();
  };

  const handleSubmit = async () => {
    if (!document) return;
    if (!title.trim()) {
      setError(t('graph.documents.editModal.errorTitleRequired'));
      return;
    }

    try {
      await updateDocument({
        uuid: document.uuid,
        data: {
          title: title !== document.title ? title : undefined,
          isoStandard: iso !== (document.isoStandard || '') ? iso : undefined,
          version: version !== (document.version || '') ? version : undefined
        }
      }).unwrap();
      handleClose();
    } catch (err: unknown) {
      const msg = (err as { data?: { message?: string } })?.data?.message;
      setError(msg || t('graph.documents.editModal.errorGeneric'));
    }
  };

  return (
    <Modal show={show} onHide={handleClose} centered>
      <Modal.Header closeButton>
        <Modal.Title className="fs-9">
          {t('graph.documents.editModal.title')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {error && (
          <Alert
            variant="danger"
            dismissible
            onClose={() => setError('')}
            className="py-2"
          >
            {error}
          </Alert>
        )}
        <Form.Group className="mb-3">
          <Form.Label className="small">
            {t('graph.documents.editModal.titleLabel')}{' '}
            <span className="text-danger">*</span>
          </Form.Label>
          <Form.Control
            size="sm"
            value={title}
            onChange={e => setTitle(e.target.value)}
          />
        </Form.Group>
        <Row className="g-2">
          <Col>
            <Form.Group>
              <Form.Label className="small">
                {t('graph.documents.editModal.isoLabel')}
              </Form.Label>
              <Form.Control
                size="sm"
                value={iso}
                onChange={e => setIso(e.target.value)}
                placeholder={t('graph.documents.editModal.isoPlaceholder')}
              />
            </Form.Group>
          </Col>
          <Col>
            <Form.Group>
              <Form.Label className="small">
                {t('graph.documents.editModal.versionLabel')}
              </Form.Label>
              <Form.Control
                size="sm"
                value={version}
                onChange={e => setVersion(e.target.value)}
                placeholder={t('graph.documents.editModal.versionPlaceholder')}
              />
            </Form.Group>
          </Col>
        </Row>
      </Modal.Body>
      <Modal.Footer>
        <Button
          variant="secondary"
          size="sm"
          onClick={handleClose}
          disabled={isLoading}
        >
          {t('graph.documents.editModal.cancel')}
        </Button>
        <Button
          variant="primary"
          size="sm"
          onClick={handleSubmit}
          disabled={isLoading}
        >
          {isLoading
            ? t('graph.documents.editModal.saving')
            : t('graph.documents.editModal.save')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

// ── Content Viewer Modal ─────────────────────────────────────────────────────

interface ContentViewerProps {
  show: boolean;
  onHide: () => void;
  document: RagDocument | null;
}

const ContentViewer: React.FC<ContentViewerProps> = ({
  show,
  onHide,
  document
}) => {
  const { t } = useTranslation();
  const [search, setSearch] = useState('');
  const { data, isLoading } = useGetDocumentChunksQuery(document?.uuid ?? '', {
    skip: !document || !show
  });

  const chunks = data?.chunks ?? [];
  const filtered = search.trim()
    ? chunks.filter(
        c =>
          c.text.toLowerCase().includes(search.toLowerCase()) ||
          (c.fullPath || '').toLowerCase().includes(search.toLowerCase())
      )
    : chunks;

  const highlightText = (text: string) => {
    if (!search.trim()) return text;
    const regex = new RegExp(
      `(${search.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`,
      'gi'
    );
    const parts = text.split(regex);
    return parts.map((part, i) =>
      regex.test(part) ? <mark key={i}>{part}</mark> : part
    );
  };

  return (
    <Modal show={show} onHide={onHide} size="xl" centered scrollable>
      <Modal.Header closeButton>
        <div>
          <Modal.Title className="fs-9">
            <FontAwesomeIcon icon={faFileAlt} className="me-2 text-primary" />
            {document?.title}
          </Modal.Title>
          <div className="small text-muted mt-1">
            {document?.fileName}
            {document?.isoStandard && (
              <Badge bg="primary" className="ms-2">
                {document.isoStandard}
              </Badge>
            )}
            {document?.version && (
              <Badge bg="secondary" className="ms-1">
                {t('graph.documents.versionPrefix', {
                  version: document.version
                })}
              </Badge>
            )}
            <span className="ms-2">
              {t('graph.documents.viewer.chunksSummary', {
                count: chunks.length
              })}
            </span>
          </div>
        </div>
      </Modal.Header>
      <Modal.Body style={{ maxHeight: '70vh' }}>
        <div className="mb-3">
          <div className="position-relative">
            <Form.Control
              size="sm"
              placeholder={t('graph.documents.viewer.searchPlaceholder')}
              value={search}
              onChange={e => setSearch(e.target.value)}
              className="ps-4"
            />
            <FontAwesomeIcon
              icon={faSearch}
              className="position-absolute text-muted"
              style={{
                left: 10,
                top: '50%',
                transform: 'translateY(-50%)',
                fontSize: '0.75rem'
              }}
            />
          </div>
          {search && (
            <small className="text-muted">
              {t('graph.documents.viewer.matchSummary', {
                matched: filtered.length,
                total: chunks.length
              })}
            </small>
          )}
        </div>

        {isLoading ? (
          <div className="text-center p-4">
            <Spinner size="sm" /> {t('graph.documents.viewer.loading')}
          </div>
        ) : chunks.length === 0 ? (
          <Alert variant="info">
            {t('graph.documents.viewer.emptyChunks')}
          </Alert>
        ) : filtered.length === 0 ? (
          <Alert variant="warning">
            {t('graph.documents.viewer.noMatches')}
          </Alert>
        ) : (
          <Accordion defaultActiveKey={['0']} alwaysOpen>
            {filtered.map((chunk, i) => (
              <Accordion.Item key={chunk.uuid} eventKey={String(i)}>
                <Accordion.Header>
                  <div className="d-flex align-items-center gap-2 w-100 pe-2">
                    <Badge bg="secondary" className="flex-shrink-0">
                      #{chunk.position + 1}
                    </Badge>
                    {chunk.fullPath && (
                      <span className="fw-semibold small">
                        {chunk.fullPath}
                      </span>
                    )}
                    {chunk.requirementLevel && (
                      <Badge bg="warning" text="dark" className="ms-1">
                        {chunk.requirementLevel}
                      </Badge>
                    )}
                    {chunk.nodeType && (
                      <Badge bg="info" className="ms-1">
                        {chunk.nodeType}
                      </Badge>
                    )}
                    <small className="text-muted ms-auto flex-shrink-0">
                      {t('graph.documents.viewer.charsSuffix', {
                        count: chunk.text.length
                      })}
                    </small>
                  </div>
                </Accordion.Header>
                <Accordion.Body>
                  <pre
                    className="mb-0 small"
                    style={{
                      whiteSpace: 'pre-wrap',
                      fontFamily: 'inherit',
                      lineHeight: 1.6
                    }}
                  >
                    {highlightText(chunk.text)}
                  </pre>
                </Accordion.Body>
              </Accordion.Item>
            ))}
          </Accordion>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" size="sm" onClick={onHide}>
          {t('graph.documents.viewer.close')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

// ── Main Page ────────────────────────────────────────────────────────────────

const GraphDocuments: React.FC = () => {
  const { t } = useTranslation();
  const [statusFilter, setStatusFilter] = useState('');
  const [isoFilter, setIsoFilter] = useState('');
  const [showUpload, setShowUpload] = useState(false);
  const [editDoc, setEditDoc] = useState<RagDocument | null>(null);
  const [viewDoc, setViewDoc] = useState<RagDocument | null>(null);

  const { data, isLoading, refetch } = useListDocumentsQuery({
    status: statusFilter || undefined,
    isoStandard: isoFilter || undefined
  } as { status?: string; isoStandard?: string });
  const [deleteDocument] = useDeleteDocumentMutation();

  const documents = data?.documents ?? [];

  // Auto-refresh while any document is processing
  const hasProcessing = documents.some(
    d => d.status === 'pending' || d.status === 'processing'
  );
  useEffect(() => {
    if (!hasProcessing) return;
    const timer = setInterval(() => refetch(), 3000);
    return () => clearInterval(timer);
  }, [hasProcessing, refetch]);

  const handleDelete = useCallback(
    async (doc: RagDocument) => {
      if (!confirm(t('graph.documents.deleteConfirm', { title: doc.title })))
        return;
      try {
        await deleteDocument(doc.uuid).unwrap();
      } catch {
        // Handled by RTK Query
      }
    },
    [deleteDocument, t]
  );

  return (
    <>
      {/* Header */}
      <Row className="g-3 mb-3">
        <Col>
          <div className="d-flex align-items-center justify-content-between">
            <h5 className="mb-0">{t('graph.documents.pageTitle')}</h5>
            <div className="d-flex gap-2">
              <Form.Select
                size="sm"
                value={statusFilter}
                onChange={e => setStatusFilter(e.target.value)}
                style={{ width: 130 }}
              >
                <option value="">
                  {t('graph.documents.statusFilter.all')}
                </option>
                <option value="pending">
                  {t('graph.documents.statusFilter.pending')}
                </option>
                <option value="processing">
                  {t('graph.documents.statusFilter.processing')}
                </option>
                <option value="completed">
                  {t('graph.documents.statusFilter.completed')}
                </option>
                <option value="failed">
                  {t('graph.documents.statusFilter.failed')}
                </option>
              </Form.Select>
              <Form.Control
                size="sm"
                placeholder={t('graph.documents.isoFilterPlaceholder')}
                value={isoFilter}
                onChange={e => setIsoFilter(e.target.value)}
                style={{ width: 130 }}
              />
              <Button
                size="sm"
                variant="primary"
                onClick={() => setShowUpload(true)}
              >
                <FontAwesomeIcon icon={faUpload} className="me-1" />
                {t('graph.documents.uploadButton')}
              </Button>
            </div>
          </div>
        </Col>
      </Row>

      {/* Table */}
      <Row className="g-3">
        <Col>
          <Card>
            <Card.Body className="p-0">
              {isLoading ? (
                <div className="text-center p-3">
                  <Spinner size="sm" />
                </div>
              ) : documents.length === 0 ? (
                <Alert variant="info" className="m-3 mb-0">
                  {t('graph.documents.empty')}
                </Alert>
              ) : (
                <Table size="sm" hover responsive className="mb-0 fs-10">
                  <thead className="bg-body-tertiary">
                    <tr>
                      <th>{t('graph.documents.cols.title')}</th>
                      <th>{t('graph.documents.cols.file')}</th>
                      <th>{t('graph.documents.cols.iso')}</th>
                      <th>{t('graph.documents.cols.llm')}</th>
                      <th>{t('graph.documents.cols.status')}</th>
                      <th className="text-end">
                        {t('graph.documents.cols.chunks')}
                      </th>
                      <th className="text-end">
                        {t('graph.documents.cols.size')}
                      </th>
                      <th>{t('graph.documents.cols.date')}</th>
                      <th className="text-end" style={{ width: 50 }}></th>
                    </tr>
                  </thead>
                  <tbody>
                    {documents.map(d => (
                      <tr key={d.uuid} className="align-middle">
                        <td>
                          <span
                            className="fw-semibold text-primary cursor-pointer"
                            role="button"
                            onClick={() =>
                              d.status === 'completed' && setViewDoc(d)
                            }
                            style={{
                              cursor:
                                d.status === 'completed' ? 'pointer' : 'default'
                            }}
                          >
                            {d.title}
                          </span>
                          {d.version && (
                            <small className="text-muted ms-1">
                              {t('graph.documents.versionPrefix', {
                                version: d.version
                              })}
                            </small>
                          )}
                        </td>
                        <td className="small text-muted">{d.fileName}</td>
                        <td>
                          {d.isoStandard ? (
                            <Badge bg="primary">{d.isoStandard}</Badge>
                          ) : (
                            <span className="text-muted">-</span>
                          )}
                        </td>
                        <td className="small text-muted">
                          {d.llmModelName || '-'}
                        </td>
                        <td>
                          <Badge bg={statusColors[d.status] || 'secondary'}>
                            {d.status === 'processing' && (
                              <Spinner size="sm" className="me-1" />
                            )}
                            {t(`graph.documents.statusLabels.${d.status}`, {
                              defaultValue: d.status
                            })}
                          </Badge>
                          {d.error && (
                            <small
                              className="d-block text-danger mt-1"
                              style={{ maxWidth: 350, wordBreak: 'break-word' }}
                            >
                              {d.error}
                            </small>
                          )}
                        </td>
                        <td className="text-end">{d.chunkCount || '-'}</td>
                        <td className="text-end small text-muted">
                          {formatSize(d.fileSize)}
                        </td>
                        <td className="small text-muted">
                          {new Date(d.createdAt).toLocaleDateString()}
                        </td>
                        <td className="text-end">
                          <Dropdown align="end">
                            <Dropdown.Toggle
                              variant="link"
                              size="sm"
                              className="text-muted p-0 shadow-none"
                            >
                              <FontAwesomeIcon icon={faEllipsisV} />
                            </Dropdown.Toggle>
                            <Dropdown.Menu className="py-1">
                              {d.status === 'completed' && (
                                <Dropdown.Item
                                  onClick={() => setViewDoc(d)}
                                  className="small"
                                >
                                  <FontAwesomeIcon
                                    icon={faEye}
                                    className="me-2"
                                    fixedWidth
                                  />
                                  {t('graph.documents.menu.viewContent')}
                                </Dropdown.Item>
                              )}
                              <Dropdown.Item
                                onClick={() => setEditDoc(d)}
                                className="small"
                              >
                                <FontAwesomeIcon
                                  icon={faPen}
                                  className="me-2"
                                  fixedWidth
                                />
                                {t('graph.documents.menu.edit')}
                              </Dropdown.Item>
                              <Dropdown.Divider className="my-1" />
                              <Dropdown.Item
                                onClick={() => handleDelete(d)}
                                className="small text-danger"
                              >
                                <FontAwesomeIcon
                                  icon={faTrash}
                                  className="me-2"
                                  fixedWidth
                                />
                                {t('graph.documents.menu.delete')}
                              </Dropdown.Item>
                            </Dropdown.Menu>
                          </Dropdown>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}
            </Card.Body>
          </Card>
        </Col>
      </Row>

      {/* Pipeline Info */}
      <Row className="g-3 mt-1">
        <Col>
          <Card className="border-0 bg-body-tertiary">
            <Card.Body className="py-3 px-4">
              <h6 className="mb-3">
                <i className="fas fa-info-circle text-info me-2" />
                {t('graph.documents.pipeline.heading')}
              </h6>
              <p className="small text-muted mb-2">
                {t('graph.documents.pipeline.intro')}
              </p>
              <ol
                className="small text-muted mb-0 ps-3"
                style={{ lineHeight: 1.9 }}
              >
                <li>
                  <Trans
                    i18nKey="graph.documents.pipeline.step1"
                    components={{ strong: <strong />, code: <code /> }}
                  />
                </li>
                <li>
                  <Trans
                    i18nKey="graph.documents.pipeline.step2"
                    components={{ strong: <strong />, code: <code /> }}
                  />
                </li>
                <li>
                  <Trans
                    i18nKey="graph.documents.pipeline.step3"
                    components={{ strong: <strong />, code: <code /> }}
                  />
                </li>
                <li>
                  <Trans
                    i18nKey="graph.documents.pipeline.step4"
                    components={{ strong: <strong />, code: <code /> }}
                  />
                </li>
                <li>
                  <Trans
                    i18nKey="graph.documents.pipeline.step5"
                    components={{ strong: <strong />, code: <code /> }}
                  />
                </li>
                <li>
                  <Trans
                    i18nKey="graph.documents.pipeline.step6"
                    components={{ strong: <strong />, code: <code /> }}
                  />
                </li>
                <li>
                  <Trans
                    i18nKey="graph.documents.pipeline.step7"
                    components={{ strong: <strong />, code: <code /> }}
                  />
                </li>
                <li>
                  <Trans
                    i18nKey="graph.documents.pipeline.step8"
                    components={{ strong: <strong />, code: <code /> }}
                  />
                </li>
                <li>
                  <Trans
                    i18nKey="graph.documents.pipeline.step9"
                    components={{ strong: <strong />, code: <code /> }}
                  />
                </li>
              </ol>
              <p className="small text-muted mt-2 mb-0">
                <Trans
                  i18nKey="graph.documents.pipeline.footer"
                  components={{ code: <code /> }}
                />
              </p>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      {/* Modals */}
      <UploadModal show={showUpload} onHide={() => setShowUpload(false)} />
      <EditModal
        show={!!editDoc}
        onHide={() => setEditDoc(null)}
        document={editDoc}
      />
      <ContentViewer
        show={!!viewDoc}
        onHide={() => setViewDoc(null)}
        document={viewDoc}
      />
    </>
  );
};

export default GraphDocuments;

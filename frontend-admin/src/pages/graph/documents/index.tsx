import { useState, useCallback, useRef, useEffect } from 'react';
import {
  Row, Col, Card, Button, Form, Table, Badge, Spinner, Alert, Modal, Dropdown,
  Accordion,
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faUpload, faEye, faPen, faTrash, faEllipsisV, faFileAlt, faSearch,
} from '@fortawesome/free-solid-svg-icons';
import {
  useListDocumentsQuery,
  useUploadDocumentMutation,
  useUpdateDocumentMutation,
  useGetDocumentChunksQuery,
  useDeleteDocumentMutation,
} from '../../../store/api/ragApi';
import { useListAIModelsQuery } from '../../../store/api/aiModelsApi';
import type { RagDocument } from '../../../types/rag';

const statusColors: Record<string, string> = {
  pending: 'warning',
  processing: 'info',
  completed: 'success',
  failed: 'danger',
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

  const handleClose = () => { reset(); onHide(); };

  const handleSubmit = async () => {
    const file = fileRef.current?.files?.[0];
    if (!file) { setError('Please select a file'); return; }

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
      setError(msg || 'Upload failed');
    }
  };

  return (
    <Modal show={show} onHide={handleClose} centered backdrop="static">
      <Modal.Header closeButton>
        <Modal.Title className="fs-9">Upload Document</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {error && <Alert variant="danger" dismissible onClose={() => setError('')} className="py-2">{error}</Alert>}
        <Form.Group className="mb-3">
          <Form.Label className="small">File (PDF or Text) <span className="text-danger">*</span></Form.Label>
          <Form.Control type="file" size="sm" ref={fileRef} accept=".pdf,.txt,.md,.text" />
        </Form.Group>
        <Form.Group className="mb-3">
          <Form.Label className="small">Title</Form.Label>
          <Form.Control size="sm" value={title} onChange={e => setTitle(e.target.value)} placeholder="Leave empty to use filename" />
        </Form.Group>
        <Row className="g-2 mb-3">
          <Col>
            <Form.Group>
              <Form.Label className="small">ISO Standard</Form.Label>
              <Form.Control size="sm" value={iso} onChange={e => setIso(e.target.value)} placeholder="e.g. ISO 9001" />
            </Form.Group>
          </Col>
          <Col>
            <Form.Group>
              <Form.Label className="small">Version</Form.Label>
              <Form.Control size="sm" value={version} onChange={e => setVersion(e.target.value)} placeholder="e.g. 2015" />
            </Form.Group>
          </Col>
        </Row>
        <Form.Group className="mb-3">
          <Form.Label className="small">Document Category</Form.Label>
          <Form.Select size="sm" value={category} onChange={e => setCategory(e.target.value)}>
            <option value="">Auto-detect</option>
            <option value="iso">ISO Standard</option>
            <option value="law">Law / Legal Act</option>
            <option value="regulation">Regulation</option>
            <option value="generic">Generic Document</option>
          </Form.Select>
        </Form.Group>
        <Form.Group>
          <Form.Label className="small">LLM for Contextual Enrichment</Form.Label>
          <Form.Select size="sm" value={llmModel} onChange={e => setLlmModel(e.target.value)}>
            <option value="">Default model</option>
            {llmModels.map(m => (
              <option key={m.uuid} value={m.uuid}>
                {m.name} ({m.modelName}){m.isDefault ? ' - default' : ''}
              </option>
            ))}
          </Form.Select>
          <Form.Text className="text-muted">
            Used to generate context prefixes for each chunk before embedding
          </Form.Text>
        </Form.Group>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" size="sm" onClick={handleClose} disabled={isLoading}>Cancel</Button>
        <Button variant="primary" size="sm" onClick={handleSubmit} disabled={isLoading}>
          {isLoading ? <><Spinner size="sm" className="me-1" /> Uploading...</> : 'Upload & Ingest'}
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

  const handleClose = () => { setError(''); onHide(); };

  const handleSubmit = async () => {
    if (!document) return;
    if (!title.trim()) { setError('Title is required'); return; }

    try {
      await updateDocument({
        uuid: document.uuid,
        data: {
          title: title !== document.title ? title : undefined,
          isoStandard: iso !== (document.isoStandard || '') ? iso : undefined,
          version: version !== (document.version || '') ? version : undefined,
        },
      }).unwrap();
      handleClose();
    } catch (err: unknown) {
      const msg = (err as { data?: { message?: string } })?.data?.message;
      setError(msg || 'Update failed');
    }
  };

  return (
    <Modal show={show} onHide={handleClose} centered>
      <Modal.Header closeButton>
        <Modal.Title className="fs-9">Edit Document</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {error && <Alert variant="danger" dismissible onClose={() => setError('')} className="py-2">{error}</Alert>}
        <Form.Group className="mb-3">
          <Form.Label className="small">Title <span className="text-danger">*</span></Form.Label>
          <Form.Control size="sm" value={title} onChange={e => setTitle(e.target.value)} />
        </Form.Group>
        <Row className="g-2">
          <Col>
            <Form.Group>
              <Form.Label className="small">ISO Standard</Form.Label>
              <Form.Control size="sm" value={iso} onChange={e => setIso(e.target.value)} placeholder="e.g. ISO 9001" />
            </Form.Group>
          </Col>
          <Col>
            <Form.Group>
              <Form.Label className="small">Version</Form.Label>
              <Form.Control size="sm" value={version} onChange={e => setVersion(e.target.value)} placeholder="e.g. 2015" />
            </Form.Group>
          </Col>
        </Row>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" size="sm" onClick={handleClose} disabled={isLoading}>Cancel</Button>
        <Button variant="primary" size="sm" onClick={handleSubmit} disabled={isLoading}>
          {isLoading ? 'Saving...' : 'Save Changes'}
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

const ContentViewer: React.FC<ContentViewerProps> = ({ show, onHide, document }) => {
  const [search, setSearch] = useState('');
  const { data, isLoading } = useGetDocumentChunksQuery(document?.uuid ?? '', { skip: !document || !show });

  const chunks = data?.chunks ?? [];
  const filtered = search.trim()
    ? chunks.filter(c =>
        c.text.toLowerCase().includes(search.toLowerCase()) ||
        (c.fullPath || '').toLowerCase().includes(search.toLowerCase())
      )
    : chunks;

  const highlightText = (text: string) => {
    if (!search.trim()) return text;
    const regex = new RegExp(`(${search.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi');
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
            {document?.isoStandard && <Badge bg="primary" className="ms-2">{document.isoStandard}</Badge>}
            {document?.version && <Badge bg="secondary" className="ms-1">v{document.version}</Badge>}
            <span className="ms-2">{chunks.length} chunks</span>
          </div>
        </div>
      </Modal.Header>
      <Modal.Body style={{ maxHeight: '70vh' }}>
        <div className="mb-3">
          <div className="position-relative">
            <Form.Control
              size="sm"
              placeholder="Search in document content..."
              value={search}
              onChange={e => setSearch(e.target.value)}
              className="ps-4"
            />
            <FontAwesomeIcon icon={faSearch} className="position-absolute text-muted" style={{ left: 10, top: '50%', transform: 'translateY(-50%)', fontSize: '0.75rem' }} />
          </div>
          {search && <small className="text-muted">{filtered.length} of {chunks.length} chunks match</small>}
        </div>

        {isLoading ? (
          <div className="text-center p-4"><Spinner size="sm" /> Loading content...</div>
        ) : chunks.length === 0 ? (
          <Alert variant="info">No chunks available. The document may still be processing.</Alert>
        ) : filtered.length === 0 ? (
          <Alert variant="warning">No chunks match your search.</Alert>
        ) : (
          <Accordion defaultActiveKey={['0']} alwaysOpen>
            {filtered.map((chunk, i) => (
              <Accordion.Item key={chunk.uuid} eventKey={String(i)}>
                <Accordion.Header>
                  <div className="d-flex align-items-center gap-2 w-100 pe-2">
                    <Badge bg="secondary" className="flex-shrink-0">#{chunk.position + 1}</Badge>
                    {chunk.fullPath && <span className="fw-semibold small">{chunk.fullPath}</span>}
                    {chunk.requirementLevel && <Badge bg="warning" text="dark" className="ms-1">{chunk.requirementLevel}</Badge>}
                    {chunk.nodeType && <Badge bg="info" className="ms-1">{chunk.nodeType}</Badge>}
                    <small className="text-muted ms-auto flex-shrink-0">{chunk.text.length} chars</small>
                  </div>
                </Accordion.Header>
                <Accordion.Body>
                  <pre className="mb-0 small" style={{ whiteSpace: 'pre-wrap', fontFamily: 'inherit', lineHeight: 1.6 }}>
                    {highlightText(chunk.text)}
                  </pre>
                </Accordion.Body>
              </Accordion.Item>
            ))}
          </Accordion>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" size="sm" onClick={onHide}>Close</Button>
      </Modal.Footer>
    </Modal>
  );
};

// ── Main Page ────────────────────────────────────────────────────────────────

const GraphDocuments: React.FC = () => {
  const [statusFilter, setStatusFilter] = useState('');
  const [isoFilter, setIsoFilter] = useState('');
  const [showUpload, setShowUpload] = useState(false);
  const [editDoc, setEditDoc] = useState<RagDocument | null>(null);
  const [viewDoc, setViewDoc] = useState<RagDocument | null>(null);

  const { data, isLoading, refetch } = useListDocumentsQuery(
    { status: statusFilter || undefined, isoStandard: isoFilter || undefined } as { status?: string; isoStandard?: string }
  );
  const [deleteDocument] = useDeleteDocumentMutation();

  const documents = data?.documents ?? [];

  // Auto-refresh while any document is processing
  const hasProcessing = documents.some(d => d.status === 'pending' || d.status === 'processing');
  useEffect(() => {
    if (!hasProcessing) return;
    const timer = setInterval(() => refetch(), 3000);
    return () => clearInterval(timer);
  }, [hasProcessing, refetch]);

  const handleDelete = useCallback(async (doc: RagDocument) => {
    if (!confirm(`Delete "${doc.title}" and all its chunks from the knowledge graph?`)) return;
    try {
      await deleteDocument(doc.uuid).unwrap();
    } catch {
      // Handled by RTK Query
    }
  }, [deleteDocument]);

  return (
    <>
      {/* Header */}
      <Row className="g-3 mb-3">
        <Col>
          <div className="d-flex align-items-center justify-content-between">
            <h5 className="mb-0">Documents</h5>
            <div className="d-flex gap-2">
              <Form.Select size="sm" value={statusFilter} onChange={e => setStatusFilter(e.target.value)} style={{ width: 130 }}>
                <option value="">All status</option>
                <option value="pending">Pending</option>
                <option value="processing">Processing</option>
                <option value="completed">Completed</option>
                <option value="failed">Failed</option>
              </Form.Select>
              <Form.Control
                size="sm"
                placeholder="ISO filter..."
                value={isoFilter}
                onChange={e => setIsoFilter(e.target.value)}
                style={{ width: 130 }}
              />
              <Button size="sm" variant="primary" onClick={() => setShowUpload(true)}>
                <FontAwesomeIcon icon={faUpload} className="me-1" />
                Upload
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
                <div className="text-center p-3"><Spinner size="sm" /></div>
              ) : documents.length === 0 ? (
                <Alert variant="info" className="m-3 mb-0">No documents ingested yet. Upload one to get started.</Alert>
              ) : (
                <Table size="sm" hover responsive className="mb-0 fs-10">
                  <thead className="bg-body-tertiary">
                    <tr>
                      <th>Title</th>
                      <th>File</th>
                      <th>ISO</th>
                      <th>LLM</th>
                      <th>Status</th>
                      <th className="text-end">Chunks</th>
                      <th className="text-end">Size</th>
                      <th>Date</th>
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
                            onClick={() => d.status === 'completed' && setViewDoc(d)}
                            style={{ cursor: d.status === 'completed' ? 'pointer' : 'default' }}
                          >
                            {d.title}
                          </span>
                          {d.version && <small className="text-muted ms-1">v{d.version}</small>}
                        </td>
                        <td className="small text-muted">{d.fileName}</td>
                        <td>{d.isoStandard ? <Badge bg="primary">{d.isoStandard}</Badge> : <span className="text-muted">-</span>}</td>
                        <td className="small text-muted">{d.llmModelName || '-'}</td>
                        <td>
                          <Badge bg={statusColors[d.status] || 'secondary'}>
                            {d.status === 'processing' && <Spinner size="sm" className="me-1" />}
                            {d.status}
                          </Badge>
                          {d.error && (
                            <small className="d-block text-danger mt-1" style={{ maxWidth: 350, wordBreak: 'break-word' }}>
                              {d.error}
                            </small>
                          )}
                        </td>
                        <td className="text-end">{d.chunkCount || '-'}</td>
                        <td className="text-end small text-muted">{formatSize(d.fileSize)}</td>
                        <td className="small text-muted">{new Date(d.createdAt).toLocaleDateString()}</td>
                        <td className="text-end">
                          <Dropdown align="end">
                            <Dropdown.Toggle variant="link" size="sm" className="text-muted p-0 shadow-none">
                              <FontAwesomeIcon icon={faEllipsisV} />
                            </Dropdown.Toggle>
                            <Dropdown.Menu className="py-1">
                              {d.status === 'completed' && (
                                <Dropdown.Item onClick={() => setViewDoc(d)} className="small">
                                  <FontAwesomeIcon icon={faEye} className="me-2" fixedWidth />
                                  View Content
                                </Dropdown.Item>
                              )}
                              <Dropdown.Item onClick={() => setEditDoc(d)} className="small">
                                <FontAwesomeIcon icon={faPen} className="me-2" fixedWidth />
                                Edit
                              </Dropdown.Item>
                              <Dropdown.Divider className="my-1" />
                              <Dropdown.Item onClick={() => handleDelete(d)} className="small text-danger">
                                <FontAwesomeIcon icon={faTrash} className="me-2" fixedWidth />
                                Delete
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
                What happens after upload?
              </h6>
              <p className="small text-muted mb-2">
                When a document is uploaded, the backend runs an asynchronous ingestion pipeline
                that transforms it into a searchable knowledge graph. Here are the steps:
              </p>
              <ol className="small text-muted mb-0 ps-3" style={{ lineHeight: 1.9 }}>
                <li>
                  <strong>Text Extraction</strong> &mdash;
                  <code>text_extractor.go</code> extracts raw text (Gotenberg for PDFs, pass-through for .txt/.md)
                </li>
                <li>
                  <strong>Structural Parsing</strong> &mdash;
                  <code>structural_parser.go</code> (PDF/TXT) or <code>markdown_parser.go</code> (.md)
                  builds a hierarchical tree of sections, clauses, and articles.
                  Cleans OCR boilerplate, promotes numbered sub-clauses (e.g. 4.4.1) to proper nodes,
                  and detects requirement levels (SHALL / SHOULD / MAY)
                </li>
                <li>
                  <strong>Chunking</strong> &mdash;
                  <code>chunker.go</code> splits the tree into chunks respecting structural boundaries.
                  Each chunk inherits metadata: full path, numbering, node type, requirement level.
                  Lists are kept together with their introductory clause when possible
                </li>
                <li>
                  <strong>Contextual Enrichment</strong> &mdash;
                  <code>contextual_enrichment.go</code> calls the LLM to generate a short context prefix
                  for each chunk (Contextual Retrieval). The prefix is prepended before embedding
                  so vectors capture broader document context
                </li>
                <li>
                  <strong>Embedding</strong> &mdash;
                  Each chunk is embedded into a vector via the configured embedding model.
                  The embedding uses the contextualized text (prefix + chunk) for better retrieval
                </li>
                <li>
                  <strong>Graph Node Creation</strong> &mdash;
                  <code>ingestion_service.go</code> creates <code>:RagDocument</code>, <code>:RagSection</code>,
                  and <code>:RagChunk</code> nodes in Memgraph
                </li>
                <li>
                  <strong>Structural Relationships</strong> &mdash;
                  <code>ingestion_service.go</code> creates <code>HAS_SECTION</code>, <code>CONTAINS</code>,
                  <code>NEXT_SECTION</code>, and <code>NEXT</code> edges to encode hierarchy and reading order
                </li>
                <li>
                  <strong>Semantic Relationships</strong> &mdash;
                  <code>relationship_extractor.go</code> extracts definitions (<code>DEFINES</code>),
                  cross-references (<code>REFERENCES</code>), and computes similarity
                  edges (<code>SIMILAR_TO</code>, cosine &ge; 0.85)
                </li>
                <li>
                  <strong>Indexing</strong> &mdash;
                  Vector indexes and property indexes are created for fast retrieval
                </li>
              </ol>
              <p className="small text-muted mt-2 mb-0">
                All files are located in <code>backend/internal/rag/services/</code>.
                The pipeline is orchestrated by <code>ingestion_service.go</code>.
              </p>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      {/* Modals */}
      <UploadModal show={showUpload} onHide={() => setShowUpload(false)} />
      <EditModal show={!!editDoc} onHide={() => setEditDoc(null)} document={editDoc} />
      <ContentViewer show={!!viewDoc} onHide={() => setViewDoc(null)} document={viewDoc} />
    </>
  );
};

export default GraphDocuments;

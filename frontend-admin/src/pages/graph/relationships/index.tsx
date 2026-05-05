import { useState, useCallback, useEffect } from 'react';
import {
  Row, Col, Card, Button, Form, Table, Badge, Spinner, Alert, Modal,
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faPlus, faPen, faTrash, faLock } from '@fortawesome/free-solid-svg-icons';
import {
  useListRelationshipTypesQuery,
  useCreateRelationshipTypeMutation,
  useUpdateRelationshipTypeMutation,
  useDeleteRelationshipTypeMutation,
} from '../../../store/api/ragApi';
import type { RelationshipTypeConfig } from '../../../types/rag';

const CATEGORIES = ['iso', 'law', 'regulation', 'generic'] as const;
const CATEGORY_LABELS: Record<string, string> = {
  iso: 'ISO',
  law: 'Law',
  regulation: 'Regulation',
  generic: 'Generic',
};
const NODE_TYPES = ['RagDocument', 'RagSection', 'RagChunk', 'RagDefinition'];

// ── Add/Edit Modal ───────────────────────────────────────────────────────────

interface RelModalProps {
  show: boolean;
  onHide: () => void;
  existing?: RelationshipTypeConfig | null;
}

const RelModal: React.FC<RelModalProps> = ({ show, onHide, existing }) => {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [fromNode, setFromNode] = useState('RagChunk');
  const [toNode, setToNode] = useState('RagChunk');
  const [properties, setProperties] = useState('');
  const [categories, setCategories] = useState<Record<string, boolean>>({
    iso: true, law: true, regulation: true, generic: true,
  });
  const [error, setError] = useState('');

  const [createRel, { isLoading: isCreating }] = useCreateRelationshipTypeMutation();
  const [updateRel, { isLoading: isUpdating }] = useUpdateRelationshipTypeMutation();
  const isLoading = isCreating || isUpdating;
  const isEdit = !!existing;

  useEffect(() => {
    if (existing && show) {
      setName(existing.name);
      setDescription(existing.description);
      setFromNode(existing.fromNode);
      setToNode(existing.toNode);
      setProperties((existing.properties ?? []).join(', '));
      setCategories({ ...existing.categories });
      setError('');
    } else if (!existing && show) {
      setName('');
      setDescription('');
      setFromNode('RagChunk');
      setToNode('RagChunk');
      setProperties('');
      setCategories({ iso: true, law: true, regulation: true, generic: true });
      setError('');
    }
  }, [existing, show]);

  const handleClose = () => { setError(''); onHide(); };

  const handleToggle = (cat: string) => {
    setCategories(prev => ({ ...prev, [cat]: !prev[cat] }));
  };

  const handleSubmit = async () => {
    if (!isEdit && !name.trim()) { setError('Name is required'); return; }

    const propsArr = properties.split(',').map(s => s.trim()).filter(Boolean);

    try {
      if (isEdit && existing) {
        await updateRel({
          uuid: existing.uuid,
          data: {
            description,
            properties: propsArr,
            categories,
          },
        }).unwrap();
      } else {
        await createRel({
          name: name.toUpperCase().replace(/\s+/g, '_'),
          description,
          fromNode,
          toNode,
          properties: propsArr.length > 0 ? propsArr : undefined,
          categories,
        }).unwrap();
      }
      handleClose();
    } catch (err: unknown) {
      const msg = (err as { data?: { detail?: string } })?.data?.detail;
      setError(msg || 'Operation failed');
    }
  };

  return (
    <Modal show={show} onHide={handleClose} centered>
      <Modal.Header closeButton>
        <Modal.Title className="fs-9">{isEdit ? 'Edit Relationship Type' : 'Add Relationship Type'}</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {error && <Alert variant="danger" dismissible onClose={() => setError('')} className="py-2">{error}</Alert>}

        <Form.Group className="mb-3">
          <Form.Label className="small">Name <span className="text-danger">*</span></Form.Label>
          <Form.Control
            size="sm"
            value={name}
            onChange={e => setName(e.target.value)}
            placeholder="e.g. IMPLEMENTS"
            disabled={isEdit}
            style={{ textTransform: 'uppercase' }}
          />
          {isEdit && <Form.Text className="text-muted">Name cannot be changed</Form.Text>}
        </Form.Group>

        <Form.Group className="mb-3">
          <Form.Label className="small">Description</Form.Label>
          <Form.Control
            size="sm"
            as="textarea"
            rows={2}
            value={description}
            onChange={e => setDescription(e.target.value)}
            placeholder="What this relationship represents"
          />
        </Form.Group>

        <Row className="g-2 mb-3">
          <Col>
            <Form.Group>
              <Form.Label className="small">From Node</Form.Label>
              <Form.Select size="sm" value={fromNode} onChange={e => setFromNode(e.target.value)} disabled={isEdit}>
                {NODE_TYPES.map(t => <option key={`from-${t}`} value={t}>{t}</option>)}
              </Form.Select>
            </Form.Group>
          </Col>
          <Col>
            <Form.Group>
              <Form.Label className="small">To Node</Form.Label>
              <Form.Select size="sm" value={toNode} onChange={e => setToNode(e.target.value)} disabled={isEdit}>
                {NODE_TYPES.map(t => <option key={`to-${t}`} value={t}>{t}</option>)}
              </Form.Select>
            </Form.Group>
          </Col>
        </Row>

        <Form.Group className="mb-3">
          <Form.Label className="small">Properties</Form.Label>
          <Form.Control
            size="sm"
            value={properties}
            onChange={e => setProperties(e.target.value)}
            placeholder="e.g. similarity, referenceText (comma-separated)"
          />
        </Form.Group>

        <Form.Group>
          <Form.Label className="small">Active for Categories</Form.Label>
          <div className="d-flex gap-3">
            {CATEGORIES.map(cat => (
              <Form.Check
                key={cat}
                type="switch"
                id={`cat-${cat}`}
                label={CATEGORY_LABELS[cat]}
                checked={categories[cat] ?? false}
                onChange={() => handleToggle(cat)}
              />
            ))}
          </div>
        </Form.Group>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" size="sm" onClick={handleClose} disabled={isLoading}>Cancel</Button>
        <Button variant="primary" size="sm" onClick={handleSubmit} disabled={isLoading}>
          {isLoading ? <><Spinner size="sm" className="me-1" /> Saving...</> : isEdit ? 'Save Changes' : 'Create'}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

// ── Category Toggle Cell ─────────────────────────────────────────────────────

interface CategoryToggleProps {
  rel: RelationshipTypeConfig;
  category: string;
}

const CategoryToggle: React.FC<CategoryToggleProps> = ({ rel, category }) => {
  const [updateRel] = useUpdateRelationshipTypeMutation();

  const handleToggle = async () => {
    const newCats = { ...rel.categories, [category]: !rel.categories[category] };
    try {
      await updateRel({ uuid: rel.uuid, data: { categories: newCats } }).unwrap();
    } catch {
      // RTK Query handles error
    }
  };

  return (
    <Form.Check
      type="switch"
      id={`toggle-${rel.uuid}-${category}`}
      checked={rel.categories[category] ?? false}
      onChange={handleToggle}
      className="d-inline-block"
    />
  );
};

// ── Main Page ────────────────────────────────────────────────────────────────

const GraphRelationships: React.FC = () => {
  const [showModal, setShowModal] = useState(false);
  const [editRel, setEditRel] = useState<RelationshipTypeConfig | null>(null);

  const { data, isLoading } = useListRelationshipTypesQuery();
  const [deleteRel] = useDeleteRelationshipTypeMutation();

  const rels = data?.relationshipTypes ?? [];

  const handleDelete = useCallback(async (rel: RelationshipTypeConfig) => {
    if (!confirm(`Delete relationship type "${rel.name}"?`)) return;
    try {
      await deleteRel(rel.uuid).unwrap();
    } catch {
      // RTK Query handles error
    }
  }, [deleteRel]);

  const handleEdit = useCallback((rel: RelationshipTypeConfig) => {
    setEditRel(rel);
    setShowModal(true);
  }, []);

  const handleAdd = useCallback(() => {
    setEditRel(null);
    setShowModal(true);
  }, []);

  return (
    <>
      <Row className="g-3 mb-3">
        <Col>
          <div className="d-flex align-items-center justify-content-between">
            <h5 className="mb-0">Relationship Types</h5>
            <Button size="sm" variant="primary" onClick={handleAdd}>
              <FontAwesomeIcon icon={faPlus} className="me-1" />
              Add Relationship
            </Button>
          </div>
        </Col>
      </Row>

      <Row className="g-3">
        <Col>
          <Card>
            <Card.Body className="p-0">
              {isLoading ? (
                <div className="text-center p-3"><Spinner size="sm" /></div>
              ) : rels.length === 0 ? (
                <Alert variant="info" className="m-3 mb-0">No relationship types configured.</Alert>
              ) : (
                <Table size="sm" hover responsive className="mb-0 fs-10">
                  <thead className="bg-body-tertiary">
                    <tr>
                      <th>Name</th>
                      <th>Description</th>
                      <th>From → To</th>
                      <th>Properties</th>
                      {CATEGORIES.map(cat => (
                        <th key={cat} className="text-center" style={{ width: 70 }}>{CATEGORY_LABELS[cat]}</th>
                      ))}
                      <th className="text-end" style={{ width: 80 }}>Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rels.map(rel => (
                      <tr key={rel.uuid} className="align-middle">
                        <td>
                          <span className="fw-semibold font-monospace">{rel.name}</span>
                          {rel.isSystem && (
                            <Badge bg="secondary" className="ms-2">
                              <FontAwesomeIcon icon={faLock} className="me-1" />
                              System
                            </Badge>
                          )}
                        </td>
                        <td className="small text-muted" style={{ maxWidth: 250 }}>{rel.description}</td>
                        <td className="small">
                          <Badge bg="info">{rel.fromNode}</Badge>
                          <span className="mx-1">→</span>
                          <Badge bg="info">{rel.toNode}</Badge>
                        </td>
                        <td className="small font-monospace">
                          {(rel.properties ?? []).join(', ') || <span className="text-muted">-</span>}
                        </td>
                        {CATEGORIES.map(cat => (
                          <td key={cat} className="text-center">
                            <CategoryToggle rel={rel} category={cat} />
                          </td>
                        ))}
                        <td className="text-end">
                          <Button
                            variant="link"
                            size="sm"
                            className="text-muted p-0 me-2"
                            onClick={() => handleEdit(rel)}
                            title="Edit"
                          >
                            <FontAwesomeIcon icon={faPen} />
                          </Button>
                          {!rel.isSystem && (
                            <Button
                              variant="link"
                              size="sm"
                              className="text-danger p-0"
                              onClick={() => handleDelete(rel)}
                              title="Delete"
                            >
                              <FontAwesomeIcon icon={faTrash} />
                            </Button>
                          )}
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

      <RelModal
        show={showModal}
        onHide={() => { setShowModal(false); setEditRel(null); }}
        existing={editRel}
      />
    </>
  );
};

export default GraphRelationships;

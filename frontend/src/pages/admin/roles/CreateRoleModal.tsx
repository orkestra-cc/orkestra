import { useEffect, useMemo, useState } from 'react';
import { Accordion, Badge, Button, Form, Modal, Spinner } from 'react-bootstrap';
import { toast } from 'react-toastify';
import {
  useCreateRoleMutation,
  useListPermissionsQuery,
  type Permission
} from 'store/api/tenantApi';

interface Props {
  orgId: string;
  show: boolean;
  onHide: () => void;
}

/**
 * CreateRoleModal lets an administrator create a custom per-tenant role by
 * picking permissions from the catalog. Permissions are grouped by their
 * module prefix so you can toggle "all billing permissions" at once.
 */
const CreateRoleModal: React.FC<Props> = ({ orgId, show, onHide }) => {
  const { data, isLoading } = useListPermissionsQuery(undefined, { skip: !show });
  const [createRole, { isLoading: isSaving }] = useCreateRoleMutation();

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set());

  useEffect(() => {
    if (!show) {
      setName('');
      setDescription('');
      setSelected(new Set());
    }
  }, [show]);

  const grouped = useMemo(() => {
    const groups: Record<string, Permission[]> = {};
    for (const p of data?.permissions ?? []) {
      if (!groups[p.module]) groups[p.module] = [];
      groups[p.module].push(p);
    }
    for (const mod of Object.keys(groups)) {
      groups[mod].sort((a, b) => a.key.localeCompare(b.key));
    }
    return groups;
  }, [data]);

  const toggle = (key: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const toggleModule = (perms: Permission[]) => {
    setSelected((prev) => {
      const next = new Set(prev);
      const allSelected = perms.every((p) => next.has(p.key));
      if (allSelected) perms.forEach((p) => next.delete(p.key));
      else perms.forEach((p) => next.add(p.key));
      return next;
    });
  };

  const canSave = name.trim().length > 0 && selected.size > 0 && !isSaving;

  const onSave = async () => {
    try {
      await createRole({
        orgId,
        body: {
          name: name.trim(),
          description: description.trim(),
          permissions: Array.from(selected)
        }
      }).unwrap();
      toast.success(`Custom role "${name.trim()}" created`);
      onHide();
    } catch (err: unknown) {
      toast.error('Create failed: ' + extractError(err));
    }
  };

  return (
    <Modal show={show} onHide={onHide} size="lg" backdrop="static" scrollable>
      <Modal.Header closeButton>
        <Modal.Title>Create custom role</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <Form>
          <Form.Group className="mb-3">
            <Form.Label>Name</Form.Label>
            <Form.Control
              type="text"
              placeholder="e.g. finance_viewer"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={80}
              autoFocus
            />
            <Form.Text muted>
              Lowercase with underscores. This name is used when binding users.
            </Form.Text>
          </Form.Group>
          <Form.Group className="mb-4">
            <Form.Label>Description</Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              placeholder="Short summary shown in the role list"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </Form.Group>

          <div className="d-flex justify-content-between align-items-center mb-2">
            <Form.Label className="mb-0">
              Permissions{' '}
              <Badge bg="primary">{selected.size} selected</Badge>
            </Form.Label>
          </div>

          {isLoading ? (
            <div className="text-center py-3">
              <Spinner animation="border" size="sm" /> Loading catalog…
            </div>
          ) : (
            <Accordion alwaysOpen>
              {Object.keys(grouped)
                .sort()
                .map((mod, idx) => {
                  const perms = grouped[mod];
                  const selectedInMod = perms.filter((p) => selected.has(p.key)).length;
                  return (
                    <Accordion.Item eventKey={String(idx)} key={mod}>
                      <Accordion.Header>
                        <div className="d-flex justify-content-between align-items-center w-100 me-3">
                          <span>
                            <strong>{mod}</strong>{' '}
                            <span className="text-muted small">
                              ({perms.length} permission{perms.length === 1 ? '' : 's'})
                            </span>
                          </span>
                          {selectedInMod > 0 && (
                            <Badge bg="primary" className="ms-2">
                              {selectedInMod}/{perms.length}
                            </Badge>
                          )}
                        </div>
                      </Accordion.Header>
                      <Accordion.Body>
                        <Form.Check
                          type="checkbox"
                          id={`mod-all-${mod}`}
                          label={<em>Select all {mod}</em>}
                          className="mb-2"
                          checked={perms.every((p) => selected.has(p.key))}
                          onChange={() => toggleModule(perms)}
                        />
                        <hr className="my-2" />
                        {perms.map((p) => (
                          <Form.Check
                            key={p.key}
                            type="checkbox"
                            id={`perm-${p.key}`}
                            className="mb-1"
                            checked={selected.has(p.key)}
                            onChange={() => toggle(p.key)}
                            label={
                              <span>
                                <code className="me-2">{p.key}</code>
                                {p.system && (
                                  <Badge bg="warning" text="dark" className="me-2">
                                    system
                                  </Badge>
                                )}
                                <span className="text-muted small">{p.description}</span>
                              </span>
                            }
                          />
                        ))}
                      </Accordion.Body>
                    </Accordion.Item>
                  );
                })}
            </Accordion>
          )}
        </Form>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isSaving}>
          Cancel
        </Button>
        <Button variant="primary" onClick={onSave} disabled={!canSave}>
          {isSaving ? <Spinner size="sm" animation="border" className="me-2" /> : null}
          Create role
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

function extractError(err: unknown): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || 'unknown error';
  }
  return String(err);
}

export default CreateRoleModal;

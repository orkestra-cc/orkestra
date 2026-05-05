import { useEffect, useState } from 'react';
import { Button, Form, Modal, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import { useCreateRoleMutation } from 'store/api/tenantApi';
import PermissionPicker from './PermissionPicker';

interface Props {
  tenantId: string;
  show: boolean;
  onHide: () => void;
}

/**
 * CreateRoleModal creates a new custom role for the current org. All
 * permission-picker state lives inside PermissionPicker — this component
 * only owns name, description, and the save mutation.
 */
const CreateRoleModal: React.FC<Props> = ({ tenantId, show, onHide }) => {
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

  const canSave = name.trim().length > 0 && selected.size > 0 && !isSaving;

  const onSave = async () => {
    try {
      await createRole({
        tenantId,
        body: {
          name: name.trim(),
          description: description.trim(),
          permissions: Array.from(selected),
        },
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
        <Modal.Title>
          <FontAwesomeIcon icon="user-plus" className="text-primary me-2" />
          Create custom role
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <Form>
          <div className="row g-3 mb-4">
            <Form.Group className="col-md-5">
              <Form.Label className="fw-semibold">Name</Form.Label>
              <Form.Control
                type="text"
                placeholder="e.g. finance_viewer"
                value={name}
                onChange={(e) => setName(e.target.value)}
                maxLength={80}
                autoFocus
              />
              <Form.Text muted>Used when binding users to the role.</Form.Text>
            </Form.Group>
            <Form.Group className="col-md-7">
              <Form.Label className="fw-semibold">Description</Form.Label>
              <Form.Control
                type="text"
                placeholder="Short summary shown in the role list"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </Form.Group>
          </div>

          <PermissionPicker selected={selected} onChange={setSelected} />
        </Form>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isSaving}>
          Cancel
        </Button>
        <Button variant="primary" onClick={onSave} disabled={!canSave}>
          {isSaving ? (
            <>
              <Spinner size="sm" animation="border" className="me-2" /> Creating…
            </>
          ) : (
            <>Create role</>
          )}
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

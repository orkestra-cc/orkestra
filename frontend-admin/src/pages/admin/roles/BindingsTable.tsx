import { useState } from 'react';
import { Badge, Button, Spinner, Table } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import {
  useListBindingsQuery,
  useDeleteBindingMutation,
  type Binding
} from 'store/api/tenantApi';
import CreateBindingModal from './CreateBindingModal';

interface Props {
  tenantId: string;
}

/**
 * BindingsTable lists active role bindings in the current org and lets
 * administrators grant new bindings or revoke existing ones. Expired
 * bindings are reaped automatically by the backend TTL index.
 */
const BindingsTable: React.FC<Props> = ({ tenantId }) => {
  const { data, isLoading, error } = useListBindingsQuery(tenantId);
  const [deleteBinding, { isLoading: isDeleting }] = useDeleteBindingMutation();
  const [showCreate, setShowCreate] = useState(false);

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" size="sm" /> Loading bindings…
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-danger">
        Failed to load bindings. Check that you have the{' '}
        <code>authz.binding.read</code> permission in this organization.
      </div>
    );
  }

  const bindings: Binding[] = data?.bindings ?? [];

  const onRevoke = async (b: Binding) => {
    if (!window.confirm(`Revoke ${b.roleName} from ${shortUUID(b.userUUID)}?`)) return;
    try {
      await deleteBinding({ tenantId, bindingId: b.id }).unwrap();
      toast.success('Binding revoked');
    } catch (err: unknown) {
      toast.error('Revoke failed: ' + extractError(err));
    }
  };

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3">
        <div>
          <strong>{bindings.length}</strong> active binding{bindings.length === 1 ? '' : 's'}
        </div>
        <Button size="sm" variant="primary" onClick={() => setShowCreate(true)}>
          <FontAwesomeIcon icon="plus" className="me-1" />
          Grant role to user
        </Button>
      </div>

      {bindings.length === 0 ? (
        <div className="text-muted text-center py-4">
          No role bindings yet. Use <strong>Grant role to user</strong> to assign
          a role to a specific user in this organization.
        </div>
      ) : (
        <Table responsive hover className="mb-0">
          <thead className="table-light">
            <tr>
              <th>User</th>
              <th>Role</th>
              <th>Granted</th>
              <th>Expires</th>
              <th style={{ width: '1%' }}></th>
            </tr>
          </thead>
          <tbody>
            {bindings.map((b) => (
              <tr key={b.id}>
                <td>
                  <code>{shortUUID(b.userUUID)}</code>
                </td>
                <td>
                  <Badge bg="info">{b.roleName}</Badge>
                </td>
                <td className="text-muted small">
                  {new Date(b.grantedAt).toLocaleString()}
                  {b.grantedBy && (
                    <div>by {shortUUID(b.grantedBy)}</div>
                  )}
                </td>
                <td>
                  {b.expiresAt ? (
                    <span className="text-warning small">
                      {new Date(b.expiresAt).toLocaleString()}
                    </span>
                  ) : (
                    <span className="text-muted small">never</span>
                  )}
                </td>
                <td className="text-end">
                  <Button
                    variant="outline-danger"
                    size="sm"
                    onClick={() => onRevoke(b)}
                    disabled={isDeleting}
                  >
                    <FontAwesomeIcon icon="times" />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}

      <CreateBindingModal
        tenantId={tenantId}
        show={showCreate}
        onHide={() => setShowCreate(false)}
      />
    </>
  );
};

function shortUUID(uuid: string): string {
  if (!uuid) return '—';
  if (uuid.length <= 12) return uuid;
  return uuid.slice(0, 8) + '…' + uuid.slice(-4);
}

function extractError(err: unknown): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || 'unknown error';
  }
  return String(err);
}

export default BindingsTable;

import { useState } from 'react';
import { Badge, Button, Spinner, Table } from 'react-bootstrap';
import { toast } from 'react-toastify';
import {
  useListRolesQuery,
  useDeleteRoleMutation,
  type Role
} from 'store/api/tenantApi';
import CreateRoleModal from './CreateRoleModal';

interface Props {
  orgId: string;
}

/**
 * RolesTable lists every role visible to the current org: the six seeded
 * system roles (orgId="") plus any custom roles created for this org.
 * System roles are read-only; custom roles support delete.
 */
const RolesTable: React.FC<Props> = ({ orgId }) => {
  const { data, isLoading, error } = useListRolesQuery(orgId);
  const [deleteRole, { isLoading: isDeleting }] = useDeleteRoleMutation();
  const [showCreate, setShowCreate] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" size="sm" /> Loading roles…
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-danger">
        Failed to load roles. Check that you have the <code>authz.role.read</code>{' '}
        permission in this organization.
      </div>
    );
  }

  const roles: Role[] = data?.roles ?? [];
  const systemRoles = roles.filter((r) => r.isSystem);
  const customRoles = roles.filter((r) => !r.isSystem);

  const onDelete = async (role: Role) => {
    if (!window.confirm(`Delete custom role "${role.name}"? This cannot be undone.`)) return;
    try {
      await deleteRole({ orgId, roleId: role.id }).unwrap();
      toast.success(`Role "${role.name}" deleted`);
    } catch (err: unknown) {
      toast.error('Delete failed: ' + extractError(err));
    }
  };

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3">
        <div>
          <strong>{systemRoles.length}</strong> system +{' '}
          <strong>{customRoles.length}</strong> custom
        </div>
        <Button size="sm" variant="primary" onClick={() => setShowCreate(true)}>
          <i className="fas fa-plus me-1" />
          Create custom role
        </Button>
      </div>

      <Table responsive hover className="mb-0">
        <thead className="table-light">
          <tr>
            <th style={{ width: '32px' }}></th>
            <th>Name</th>
            <th>Description</th>
            <th className="text-center">Type</th>
            <th className="text-center">Permissions</th>
            <th style={{ width: '1%' }}></th>
          </tr>
        </thead>
        <tbody>
          {[...systemRoles, ...customRoles].map((role) => {
            const isExpanded = expanded === role.id;
            return (
              <>
                <tr key={role.id}>
                  <td>
                    <Button
                      variant="link"
                      size="sm"
                      className="p-0"
                      onClick={() => setExpanded(isExpanded ? null : role.id)}
                      aria-label={isExpanded ? 'Collapse permissions' : 'Expand permissions'}
                    >
                      <i className={`fas fa-chevron-${isExpanded ? 'down' : 'right'}`} />
                    </Button>
                  </td>
                  <td>
                    <strong>{role.name}</strong>
                  </td>
                  <td>
                    <span className="text-muted">{role.description || '—'}</span>
                  </td>
                  <td className="text-center">
                    {role.isSystem ? (
                      <Badge bg="secondary">system</Badge>
                    ) : (
                      <Badge bg="info">custom</Badge>
                    )}
                  </td>
                  <td className="text-center">
                    <Badge bg="light" text="dark">
                      {role.permissions.length}
                    </Badge>
                  </td>
                  <td className="text-end">
                    {!role.isSystem && (
                      <Button
                        variant="outline-danger"
                        size="sm"
                        onClick={() => onDelete(role)}
                        disabled={isDeleting}
                      >
                        <i className="fas fa-trash" />
                      </Button>
                    )}
                  </td>
                </tr>
                {isExpanded && (
                  <tr key={role.id + '-perms'}>
                    <td></td>
                    <td colSpan={5}>
                      <PermissionList permissions={role.permissions} />
                    </td>
                  </tr>
                )}
              </>
            );
          })}
        </tbody>
      </Table>

      <CreateRoleModal
        orgId={orgId}
        show={showCreate}
        onHide={() => setShowCreate(false)}
      />
    </>
  );
};

/** Renders a role's permissions grouped by module prefix. */
const PermissionList: React.FC<{ permissions: string[] }> = ({ permissions }) => {
  if (permissions.length === 1 && permissions[0] === '*') {
    return (
      <div className="py-2">
        <Badge bg="warning" text="dark">
          * (wildcard — all permissions)
        </Badge>
      </div>
    );
  }
  const groups: Record<string, string[]> = {};
  for (const p of permissions) {
    const dot = p.indexOf('.');
    const mod = dot >= 0 ? p.slice(0, dot) : 'other';
    if (!groups[mod]) groups[mod] = [];
    groups[mod].push(p);
  }
  const sortedGroups = Object.keys(groups).sort();
  return (
    <div className="py-2">
      {sortedGroups.map((mod) => (
        <div key={mod} className="mb-2">
          <div className="text-muted small text-uppercase mb-1">{mod}</div>
          <div className="d-flex flex-wrap gap-1">
            {groups[mod].sort().map((p) => (
              <Badge key={p} bg="light" text="dark" className="fw-normal">
                {p}
              </Badge>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
};

function extractError(err: unknown): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || 'unknown error';
  }
  return String(err);
}

export default RolesTable;

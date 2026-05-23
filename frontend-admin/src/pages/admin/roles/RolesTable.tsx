import { useMemo, useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Card,
  Form,
  InputGroup,
  OverlayTrigger,
  Spinner,
  Tooltip
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import {
  useListRolesQuery,
  useUpdateRoleMutation,
  type Role
} from 'store/api/tenantApi';
import CreateRoleModal from './CreateRoleModal';
import EditRoleModal from './EditRoleModal';
import DeleteRoleModal from './DeleteRoleModal';

interface Props {
  tenantId: string;
}

// System roles are rendered highest-privilege first. Unknown names fall to the
// end so a future role added server-side still renders.
const SYSTEM_ROLE_ORDER = [
  'super_admin',
  'administrator',
  'developer',
  'manager',
  'operator',
  'guest'
] as const;

const systemRoleRank = (name: string): number => {
  const idx = SYSTEM_ROLE_ORDER.indexOf(
    name as (typeof SYSTEM_ROLE_ORDER)[number]
  );
  return idx === -1 ? SYSTEM_ROLE_ORDER.length : idx;
};

/**
 * RolesTable is the main surface of the role-management page. It lists the
 * six seeded system roles plus any custom roles scoped to the current org,
 * and it's the launchpad for every role CRUD operation:
 *
 *   - Create custom role   → CreateRoleModal
 *   - Edit role            → EditRoleModal (system roles get a disable-only view)
 *   - Delete custom role   → DeleteRoleModal (cascade confirm)
 *   - Toggle active state  → inline Active switch, patches via updateRole
 *
 * System and custom roles are rendered as separate stacked sections so the
 * "which of these can I actually change" question is visually obvious
 * before the user clicks anything. Both sections share a single search
 * input that matches against name, description, and permission keys.
 */
const RolesTable: React.FC<Props> = ({ tenantId }) => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useListRolesQuery(tenantId);
  const [updateRole] = useUpdateRoleMutation();
  const [showCreate, setShowCreate] = useState(false);
  const [editing, setEditing] = useState<Role | null>(null);
  const [deleting, setDeleting] = useState<Role | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [query, setQuery] = useState('');

  const roles: Role[] = data?.roles ?? [];

  const q = query.trim().toLowerCase();
  const matches = (r: Role) => {
    if (!q) return true;
    if (r.name.toLowerCase().includes(q)) return true;
    if (r.description.toLowerCase().includes(q)) return true;
    return r.permissions.some(p => p.toLowerCase().includes(q));
  };

  const systemRoles = useMemo(
    () =>
      roles
        .filter(r => r.isSystem && matches(r))
        .sort((a, b) => systemRoleRank(a.name) - systemRoleRank(b.name)),
    [roles, q]
  );
  const customRoles = useMemo(
    () =>
      roles
        .filter(r => !r.isSystem && matches(r))
        .sort((a, b) => a.name.localeCompare(b.name)),
    [roles, q]
  );

  const totalSystem = roles.filter(r => r.isSystem).length;
  const totalCustom = roles.filter(r => !r.isSystem).length;

  const unknownErr = t('adminRoles.rolesTable.errorUnknown');

  const onToggleActive = async (role: Role) => {
    try {
      await updateRole({
        tenantId,
        roleId: role.id,
        body: { isActive: !role.isActive }
      }).unwrap();
      toast.success(
        role.isActive
          ? t('adminRoles.rolesTable.toastDisabled', { name: role.name })
          : t('adminRoles.rolesTable.toastEnabled', { name: role.name })
      );
    } catch (err: unknown) {
      toast.error(
        t('adminRoles.rolesTable.toastToggleFailed', {
          error: extractError(err, unknownErr)
        })
      );
    }
  };

  if (isLoading) {
    return (
      <div className="text-center py-5">
        <Spinner animation="border" size="sm" />{' '}
        {t('adminRoles.rolesTable.loading')}
      </div>
    );
  }

  if (error) {
    return (
      <Alert variant="danger">
        <Alert.Heading className="fs-9">
          {t('adminRoles.rolesTable.errorTitle')}
        </Alert.Heading>
        <p className="mb-0 fs-10">
          <Trans
            i18nKey="adminRoles.rolesTable.errorBody"
            components={{ code: <code /> }}
          />
        </p>
      </Alert>
    );
  }

  return (
    <>
      {/* Toolbar */}
      <div className="d-flex flex-wrap gap-2 align-items-center justify-content-between mb-3">
        <div className="d-flex gap-2 align-items-center">
          <Badge bg="secondary" className="fs-11">
            <FontAwesomeIcon icon="lock" className="me-1" />
            {t('adminRoles.rolesTable.countSystem', { count: totalSystem })}
          </Badge>
          <Badge bg="info" className="fs-11">
            <FontAwesomeIcon icon="users-cog" className="me-1" />
            {t('adminRoles.rolesTable.countCustom', { count: totalCustom })}
          </Badge>
        </div>
        <div className="d-flex gap-2 align-items-center flex-grow-1 justify-content-end">
          <InputGroup size="sm" style={{ maxWidth: 320 }}>
            <InputGroup.Text>
              <FontAwesomeIcon icon="search" className="text-muted" />
            </InputGroup.Text>
            <Form.Control
              type="search"
              placeholder={t('adminRoles.rolesTable.searchPlaceholder')}
              value={query}
              onChange={e => setQuery(e.target.value)}
              aria-label={t('adminRoles.rolesTable.searchAriaLabel')}
            />
            {query && (
              <Button variant="outline-secondary" onClick={() => setQuery('')}>
                <FontAwesomeIcon icon="times" />
              </Button>
            )}
          </InputGroup>
          <Button
            size="sm"
            variant="primary"
            onClick={() => setShowCreate(true)}
          >
            <FontAwesomeIcon icon="plus" className="me-1" />
            {t('adminRoles.rolesTable.newCustomRole')}
          </Button>
        </div>
      </div>

      {/* System roles section */}
      <RoleSection
        title={t('adminRoles.rolesTable.sectionSystemTitle')}
        subtitle={t('adminRoles.rolesTable.sectionSystemSubtitle')}
        icon="shield-alt"
        roles={systemRoles}
        empty={
          q ? (
            <EmptyResult query={q} />
          ) : (
            <div className="text-muted fs-10 py-3">
              {t('adminRoles.rolesTable.systemEmpty')}
            </div>
          )
        }
      >
        {systemRoles.map(role => (
          <RoleRow
            key={role.id}
            role={role}
            expanded={expanded === role.id}
            onToggleExpand={() =>
              setExpanded(expanded === role.id ? null : role.id)
            }
            onEdit={() => setEditing(role)}
            onDelete={null}
            onToggleActive={() => onToggleActive(role)}
          />
        ))}
      </RoleSection>

      {/* Custom roles section */}
      <div className="mt-4">
        <RoleSection
          title={t('adminRoles.rolesTable.sectionCustomTitle')}
          subtitle={t('adminRoles.rolesTable.sectionCustomSubtitle')}
          icon="users-cog"
          roles={customRoles}
          empty={
            q ? (
              <EmptyResult query={q} />
            ) : (
              <NoCustomRoles onCreate={() => setShowCreate(true)} />
            )
          }
        >
          {customRoles.map(role => (
            <RoleRow
              key={role.id}
              role={role}
              expanded={expanded === role.id}
              onToggleExpand={() =>
                setExpanded(expanded === role.id ? null : role.id)
              }
              onEdit={() => setEditing(role)}
              onDelete={() => setDeleting(role)}
              onToggleActive={() => onToggleActive(role)}
            />
          ))}
        </RoleSection>
      </div>

      <CreateRoleModal
        tenantId={tenantId}
        show={showCreate}
        onHide={() => setShowCreate(false)}
      />
      <EditRoleModal
        tenantId={tenantId}
        role={editing}
        show={editing !== null}
        onHide={() => setEditing(null)}
      />
      <DeleteRoleModal
        tenantId={tenantId}
        role={deleting}
        show={deleting !== null}
        onHide={() => setDeleting(null)}
      />
    </>
  );
};

// -------------------------------------------------------------------------
// Section wrapper — renders a titled card with either the rows it was given
// or an "empty" fallback. Keeps the two sections visually consistent.
// -------------------------------------------------------------------------
interface RoleSectionProps {
  title: string;
  subtitle: string;
  icon: 'shield-alt' | 'users-cog';
  roles: Role[];
  empty: React.ReactNode;
  children: React.ReactNode;
}

const RoleSection: React.FC<RoleSectionProps> = ({
  title,
  subtitle,
  icon,
  roles,
  empty,
  children
}) => {
  return (
    <Card className="shadow-none border">
      <Card.Header className="bg-body-tertiary py-2 px-3">
        <div className="d-flex align-items-center">
          <FontAwesomeIcon icon={icon} className="text-primary me-2" />
          <div>
            <div className="fw-semibold">{title}</div>
            <div className="text-muted small">{subtitle}</div>
          </div>
        </div>
      </Card.Header>
      <Card.Body className="p-0">
        {roles.length === 0 ? (
          <div className="px-3">{empty}</div>
        ) : (
          <div className="role-list">{children}</div>
        )}
      </Card.Body>
    </Card>
  );
};

// -------------------------------------------------------------------------
// One role row. Renders as a flex div (not a tr) so we can use the full
// width responsively and keep the inline active switch aligned with the
// action buttons on the right. Clicking the name area expands the row to
// show its full permission list.
// -------------------------------------------------------------------------
interface RoleRowProps {
  role: Role;
  expanded: boolean;
  onToggleExpand: () => void;
  onEdit: () => void;
  onDelete: (() => void) | null;
  onToggleActive: () => void;
}

const RoleRow: React.FC<RoleRowProps> = ({
  role,
  expanded,
  onToggleExpand,
  onEdit,
  onDelete,
  onToggleActive
}) => {
  const { t } = useTranslation();
  const dimmed = !role.isActive ? 'opacity-75' : '';
  return (
    <div className={`border-bottom ${dimmed}`}>
      <div className="d-flex align-items-center px-3 py-2 gap-3">
        {/* Expand chevron */}
        <Button
          variant="link"
          size="sm"
          className="p-0 text-body-tertiary"
          onClick={onToggleExpand}
          aria-label={
            expanded
              ? t('adminRoles.rolesTable.toggleExpandCollapseAria')
              : t('adminRoles.rolesTable.toggleExpandAria')
          }
        >
          <FontAwesomeIcon
            icon={expanded ? 'chevron-down' : 'chevron-right'}
            className="fs-10"
          />
        </Button>

        {/* Name + description (clickable to expand) */}
        <button
          type="button"
          className="btn btn-link p-0 text-start flex-grow-1 text-decoration-none"
          onClick={onToggleExpand}
          style={{ minWidth: 0 }}
        >
          <div className="d-flex align-items-center gap-2">
            <span className="fw-semibold text-body">{role.name}</span>
            {role.isSystem && (
              <Badge bg="secondary" className="fw-normal">
                <FontAwesomeIcon icon="lock" className="me-1" />
                {t('adminRoles.rolesTable.badgeSystem')}
              </Badge>
            )}
            {!role.isActive && (
              <Badge bg="warning" text="dark" className="fw-normal">
                {t('adminRoles.rolesTable.badgeDisabled')}
              </Badge>
            )}
          </div>
          {role.description && (
            <div className="text-muted small text-truncate">
              {role.description}
            </div>
          )}
        </button>

        {/* Permission count chip */}
        <div className="text-muted small d-none d-md-block text-nowrap">
          {role.permissions[0] === '*' ? (
            <Badge bg="warning" text="dark">
              <span className="me-1">∗</span>
              {t('adminRoles.rolesTable.permissionsAll')}
            </Badge>
          ) : (
            <span>
              <Trans
                i18nKey={
                  role.permissions.length === 1
                    ? 'adminRoles.rolesTable.permissionsCountOne'
                    : 'adminRoles.rolesTable.permissionsCountOther'
                }
                values={{ count: role.permissions.length }}
                components={{ strong: <strong /> }}
              />
            </span>
          )}
        </div>

        {/* Inline active toggle */}
        <OverlayTrigger
          placement="top"
          overlay={
            <Tooltip>
              {role.isActive
                ? t('adminRoles.rolesTable.tooltipDisable')
                : t('adminRoles.rolesTable.tooltipEnable')}
            </Tooltip>
          }
        >
          <Form.Check
            type="switch"
            id={`role-active-${role.id}`}
            className="m-0"
            checked={role.isActive}
            onChange={onToggleActive}
            aria-label={
              role.isActive
                ? t('adminRoles.rolesTable.ariaDisableRole')
                : t('adminRoles.rolesTable.ariaEnableRole')
            }
          />
        </OverlayTrigger>

        {/* Edit */}
        <OverlayTrigger
          placement="top"
          overlay={
            <Tooltip>
              {role.isSystem
                ? t('adminRoles.rolesTable.tooltipEditSystem')
                : t('adminRoles.rolesTable.tooltipEditCustom')}
            </Tooltip>
          }
        >
          <Button
            variant="outline-primary"
            size="sm"
            onClick={onEdit}
            aria-label={t('adminRoles.rolesTable.ariaEditRole', {
              name: role.name
            })}
          >
            <FontAwesomeIcon icon="pencil-alt" />
          </Button>
        </OverlayTrigger>

        {/* Delete (custom only) */}
        {onDelete ? (
          <OverlayTrigger
            placement="top"
            overlay={
              <Tooltip>{t('adminRoles.rolesTable.tooltipDeleteRole')}</Tooltip>
            }
          >
            <Button
              variant="outline-danger"
              size="sm"
              onClick={onDelete}
              aria-label={t('adminRoles.rolesTable.ariaDeleteRole', {
                name: role.name
              })}
            >
              <FontAwesomeIcon icon="trash" />
            </Button>
          </OverlayTrigger>
        ) : (
          <div style={{ width: 38 }} aria-hidden />
        )}
      </div>

      {expanded && (
        <div className="px-3 pb-3 pt-1 bg-body-tertiary">
          <PermissionChips permissions={role.permissions} />
        </div>
      )}
    </div>
  );
};

// -------------------------------------------------------------------------
// Expanded view: permission badges grouped by module prefix. Much more
// scannable than a raw flat list.
// -------------------------------------------------------------------------
const PermissionChips: React.FC<{ permissions: string[] }> = ({
  permissions
}) => {
  const { t } = useTranslation();
  if (permissions.length === 1 && permissions[0] === '*') {
    return (
      <div>
        <Badge bg="warning" text="dark">
          <span className="me-1">∗</span>
          {t('adminRoles.rolesTable.wildcardChip')}
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
    <div>
      {sortedGroups.map(mod => (
        <div key={mod} className="mb-2">
          <div className="text-muted small text-uppercase fw-bold mb-1">
            {mod}{' '}
            <span className="fw-normal">
              {t('adminRoles.rolesTable.moduleGroupCount', {
                count: groups[mod].length
              })}
            </span>
          </div>
          <div className="d-flex flex-wrap gap-1">
            {groups[mod].sort().map(p => (
              <Badge
                key={p}
                bg="light"
                text="dark"
                className="fw-normal border"
              >
                {p}
              </Badge>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
};

// -------------------------------------------------------------------------
// Empty states
// -------------------------------------------------------------------------
const NoCustomRoles: React.FC<{ onCreate: () => void }> = ({ onCreate }) => {
  const { t } = useTranslation();
  return (
    <div className="text-center py-5 text-muted">
      <div className="mb-2">
        <FontAwesomeIcon icon="users-cog" className="fs-5 text-body-tertiary" />
      </div>
      <div className="fw-semibold text-body">
        {t('adminRoles.rolesTable.noCustomRolesTitle')}
      </div>
      <div className="fs-10 mb-3">
        {t('adminRoles.rolesTable.noCustomRolesBody')}
      </div>
      <Button size="sm" variant="primary" onClick={onCreate}>
        <FontAwesomeIcon icon="plus" className="me-1" />{' '}
        {t('adminRoles.rolesTable.createFirstCustom')}
      </Button>
    </div>
  );
};

const EmptyResult: React.FC<{ query: string }> = ({ query }) => (
  <div className="text-center py-4 text-muted fs-10">
    <FontAwesomeIcon icon="filter" className="me-1" />
    <Trans
      i18nKey="adminRoles.rolesTable.noMatch"
      values={{ query }}
      components={{ code: <code /> }}
    />
  </div>
);

function extractError(err: unknown, unknownLabel: string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || unknownLabel;
  }
  return String(err);
}

export default RolesTable;

import { useEffect, useState } from 'react';
import {
  Alert,
  Button,
  Form,
  Modal,
  Spinner,
  Tab,
  Table,
  Tabs,
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import {
  useUpdateOrgAdminMutation,
  useUpdateOrgPlanAdminMutation,
  useListOrgMembersAdminQuery,
  useRemoveOrgMemberAdminMutation,
  useListOrgInvitesAdminQuery,
  useCreateOrgInviteAdminMutation,
  useRevokeOrgInviteAdminMutation,
  useGetTenantBillingCustomerAdminQuery,
  usePromoteTenantToBillingCustomerAdminMutation,
  type AdminOrgListItem,
  type Invite,
  type TenantBillingCustomer,
} from 'store/api/tenantApi';

interface Props {
  org: AdminOrgListItem | null;
  show: boolean;
  onHide: () => void;
  onDelete: (org: AdminOrgListItem) => void;
  onPurge: (org: AdminOrgListItem) => void;
}

// Mirrors backend/internal/core/tenant/services/service.go::defaultFeaturesForPlan.
// Keep in sync when backend plan defaults change.
const PLAN_FEATURES: Record<string, string[]> = {
  free: ['billing', 'documents'],
  pro: ['billing', 'documents', 'company', 'sales', 'agents'],
  enterprise: ['*'],
};

const ALL_FEATURES = ['billing', 'documents', 'company', 'sales', 'agents', 'graph', 'rag'];

const planColors: Record<string, BadgeColor> = {
  free: 'secondary',
  pro: 'primary',
  enterprise: 'success',
};

type TenantTabKey = 'overview' | 'plan' | 'members' | 'invites' | 'billing';

const TenantDetailModal: React.FC<Props> = ({ org, show, onHide, onDelete, onPurge }) => {
  const [tab, setTab] = useState<TenantTabKey>('overview');

  useEffect(() => {
    if (show) setTab('overview');
  }, [show, org?.id]);

  if (!org) return null;

  // The Billing tab only makes sense for Tier-2 external tenants —
  // FatturaPA customers are recipients of invoices the operator issues
  // to clients. Internal tenants are the operator side and don't carry
  // their own customer profile. ADR-0001 PR-4.
  const showBillingTab = org.kind === 'external';

  return (
    <Modal show={show} onHide={onHide} size="xl" backdrop="static">
      <Modal.Header closeButton>
        <Modal.Title className="d-flex align-items-center gap-3">
          <FontAwesomeIcon icon="building" className="text-primary" />
          <span>{org.name}</span>
          <SubtleBadge bg={planColors[org.plan] || 'secondary'} pill>
            {org.plan}
          </SubtleBadge>
          {org.status === 'purged' ? (
            <SubtleBadge bg="dark" pill>
              purged
            </SubtleBadge>
          ) : org.deletedAt || org.status === 'archived' ? (
            <SubtleBadge bg="danger" pill>
              deleted
            </SubtleBadge>
          ) : null}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <Tabs
          id="tenant-detail-tabs"
          activeKey={tab}
          onSelect={(k) => setTab((k as TenantTabKey) || 'overview')}
          className="mb-3"
        >
          <Tab eventKey="overview" title="Overview">
            <OverviewTab org={org} />
          </Tab>
          <Tab eventKey="plan" title="Plan">
            <PlanTab org={org} />
          </Tab>
          <Tab eventKey="members" title={`Members (${org.memberCount})`}>
            <MembersTab org={org} />
          </Tab>
          <Tab eventKey="invites" title="Invites">
            <InvitesTab org={org} />
          </Tab>
          {showBillingTab && (
            <Tab eventKey="billing" title="Billing">
              <BillingTab org={org} />
            </Tab>
          )}
        </Tabs>
      </Modal.Body>
      <Modal.Footer className="d-flex justify-content-between flex-wrap gap-2">
        <div className="d-flex gap-2 flex-wrap">
          {org.status !== 'purged' && !org.deletedAt && (
            <Button variant="outline-danger" size="sm" onClick={() => onDelete(org)}>
              <FontAwesomeIcon icon="trash" className="me-1" />
              Delete tenant
            </Button>
          )}
          {org.status !== 'purged' && (
            <Button variant="danger" size="sm" onClick={() => onPurge(org)}>
              <FontAwesomeIcon icon="exclamation-triangle" className="me-1" />
              Purge (crypto-shred)
            </Button>
          )}
          {org.status === 'purged' && org.purgedAt && (
            <span className="text-muted fs-10">
              Purged on {new Date(org.purgedAt).toLocaleString()} — key shredded.
            </span>
          )}
          {org.status !== 'purged' && org.deletedAt && (
            <span className="text-muted fs-10">
              Soft-deleted on {new Date(org.deletedAt).toLocaleString()}
            </span>
          )}
        </div>
        <Button variant="secondary" onClick={onHide}>
          Close
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

// --- Overview tab ---

const OverviewTab: React.FC<{ org: AdminOrgListItem }> = ({ org }) => {
  const [updateOrg, { isLoading }] = useUpdateOrgAdminMutation();
  const [name, setName] = useState(org.name);
  const [slug, setSlug] = useState(org.slug);

  useEffect(() => {
    setName(org.name);
    setSlug(org.slug);
  }, [org.id, org.name, org.slug]);

  const dirty = name !== org.name || slug !== org.slug;

  const onSave = async () => {
    try {
      await updateOrg({
        tenantId: org.id,
        body: {
          name: name !== org.name ? name : undefined,
          slug: slug !== org.slug ? slug : undefined,
        },
      }).unwrap();
      toast.success('Tenant updated');
    } catch (err: unknown) {
      toast.error('Update failed: ' + extractError(err));
    }
  };

  return (
    <Form className="px-1">
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">Tenant ID</Form.Label>
        <Form.Control readOnly value={org.id} className="fs-11 font-monospace" />
      </Form.Group>
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">Name</Form.Label>
        <Form.Control value={name} onChange={(e) => setName(e.target.value)} />
      </Form.Group>
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">Slug</Form.Label>
        <Form.Control value={slug} onChange={(e) => setSlug(e.target.value)} />
      </Form.Group>
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">Owner</Form.Label>
        <Form.Control
          readOnly
          value={org.ownerUserUUID || '—'}
          className="fs-11 font-monospace"
        />
      </Form.Group>
      <div className="d-flex justify-content-end">
        <Button
          variant="primary"
          size="sm"
          disabled={!dirty || isLoading}
          onClick={onSave}
        >
          {isLoading ? 'Saving…' : 'Save changes'}
        </Button>
      </div>
    </Form>
  );
};

// --- Plan tab ---

const PlanTab: React.FC<{ org: AdminOrgListItem }> = ({ org }) => {
  const [updatePlan, { isLoading }] = useUpdateOrgPlanAdminMutation();
  const [plan, setPlan] = useState(org.plan);
  const [features, setFeatures] = useState<string[]>(org.features ?? []);

  useEffect(() => {
    setPlan(org.plan);
    setFeatures(org.features ?? []);
  }, [org.id, org.plan, org.features]);

  const handlePlanChange = (newPlan: string) => {
    setPlan(newPlan);
    setFeatures(PLAN_FEATURES[newPlan] ?? []);
  };

  const toggleFeature = (feature: string) => {
    setFeatures((prev) =>
      prev.includes(feature) ? prev.filter((f) => f !== feature) : [...prev, feature],
    );
  };

  const onSave = async () => {
    try {
      await updatePlan({ tenantId: org.id, body: { plan, features } }).unwrap();
      toast.success('Plan updated');
    } catch (err: unknown) {
      toast.error('Plan update failed: ' + extractError(err));
    }
  };

  const hasWildcard = features.includes('*');

  return (
    <Form className="px-1">
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">Plan</Form.Label>
        <Form.Select value={plan} onChange={(e) => handlePlanChange(e.target.value)}>
          <option value="free">Free</option>
          <option value="pro">Pro</option>
          <option value="enterprise">Enterprise</option>
        </Form.Select>
        <Form.Text muted>
          Changing the plan pre-fills the features checklist with that plan's
          defaults. You can override individual features before saving.
        </Form.Text>
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">Features</Form.Label>
        {hasWildcard && (
          <Alert variant="info" className="fs-10 py-2">
            The <code>*</code> wildcard grants every feature — individual
            checkboxes have no effect while it is present.
          </Alert>
        )}
        <div className="d-flex flex-wrap gap-3">
          {ALL_FEATURES.map((feature) => (
            <Form.Check
              key={feature}
              type="checkbox"
              id={`feature-${feature}`}
              label={feature}
              checked={features.includes(feature)}
              disabled={hasWildcard}
              onChange={() => toggleFeature(feature)}
            />
          ))}
        </div>
      </Form.Group>

      <div className="d-flex justify-content-end">
        <Button variant="primary" size="sm" disabled={isLoading} onClick={onSave}>
          {isLoading ? 'Saving…' : 'Save plan'}
        </Button>
      </div>
    </Form>
  );
};

// --- Members tab ---

const MembersTab: React.FC<{ org: AdminOrgListItem }> = ({ org }) => {
  const { data, isLoading, error } = useListOrgMembersAdminQuery(org.id);
  const [removeMember] = useRemoveOrgMemberAdminMutation();

  const onRemove = async (userUUID: string) => {
    try {
      await removeMember({ tenantId: org.id, userUUID }).unwrap();
      toast.success('Member removed');
    } catch (err: unknown) {
      toast.error('Remove failed: ' + extractError(err));
    }
  };

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner size="sm" animation="border" />
      </div>
    );
  }

  if (error) {
    return (
      <Alert variant="danger" className="fs-10">
        Failed to load members.
      </Alert>
    );
  }

  const members = data?.members ?? [];

  return (
    <>
      <Alert variant="info" className="fs-10 py-2">
        Role assignments for each member are managed on the{' '}
        <a href="/admin/roles">Role Management page</a>. This tab only shows
        current memberships and lets you remove them.
      </Alert>
      <Table size="sm" className="fs-10 mb-0">
        <thead className="bg-body-tertiary">
          <tr>
            <th>User UUID</th>
            <th>Roles</th>
            <th>Joined</th>
            <th>Owner</th>
            <th className="text-end">Actions</th>
          </tr>
        </thead>
        <tbody>
          {members.map((m) => (
            <tr key={m.id} className="align-middle">
              <td className="font-monospace fs-11">{m.userUUID}</td>
              <td>{m.roles.join(', ') || '—'}</td>
              <td className="text-muted">
                {m.joinedAt ? new Date(m.joinedAt).toLocaleDateString() : '—'}
              </td>
              <td>
                {m.isOwner && (
                  <SubtleBadge bg="primary" pill>
                    owner
                  </SubtleBadge>
                )}
              </td>
              <td className="text-end">
                {!m.isOwner && (
                  <Button
                    variant="link"
                    size="sm"
                    className="p-0 text-danger text-decoration-none"
                    onClick={() => onRemove(m.userUUID)}
                  >
                    Remove
                  </Button>
                )}
              </td>
            </tr>
          ))}
          {members.length === 0 && (
            <tr>
              <td colSpan={5} className="text-center text-muted py-3">
                No members yet.
              </td>
            </tr>
          )}
        </tbody>
      </Table>
    </>
  );
};

// --- Invites tab ---

const InvitesTab: React.FC<{ org: AdminOrgListItem }> = ({ org }) => {
  const { data, isLoading, error } = useListOrgInvitesAdminQuery({ tenantId: org.id });
  const [createInvite, { isLoading: isCreating }] = useCreateOrgInviteAdminMutation();
  const [revokeInvite] = useRevokeOrgInviteAdminMutation();

  const [email, setEmail] = useState('');
  const [rolesInput, setRolesInput] = useState('operator');
  const [freshInvite, setFreshInvite] = useState<Invite | null>(null);

  const onCreate = async () => {
    const roles = rolesInput
      .split(',')
      .map((r) => r.trim())
      .filter(Boolean);
    if (!email || roles.length === 0) {
      toast.error('Email and at least one role are required');
      return;
    }
    try {
      const inv = await createInvite({
        tenantId: org.id,
        body: { email, roles },
      }).unwrap();
      setFreshInvite(inv);
      setEmail('');
      toast.success('Invite created');
    } catch (err: unknown) {
      toast.error('Invite failed: ' + extractError(err));
    }
  };

  const onRevoke = async (inviteId: string) => {
    try {
      await revokeInvite({ tenantId: org.id, inviteId }).unwrap();
      toast.success('Invite revoked');
    } catch (err: unknown) {
      toast.error('Revoke failed: ' + extractError(err));
    }
  };

  const copyToken = () => {
    if (!freshInvite?.token) return;
    navigator.clipboard.writeText(freshInvite.token);
    toast.success('Invite token copied');
  };

  const invites = data?.invites ?? [];

  return (
    <>
      {freshInvite?.token && (
        <Alert
          variant="warning"
          className="fs-10"
          dismissible
          onClose={() => setFreshInvite(null)}
        >
          <strong>Copy this token now — it cannot be shown again.</strong>
          <div className="d-flex align-items-center gap-2 mt-2">
            <code className="flex-grow-1 fs-11 text-break">{freshInvite.token}</code>
            <Button size="sm" variant="outline-dark" onClick={copyToken}>
              <FontAwesomeIcon icon="copy" className="me-1" />
              Copy
            </Button>
          </div>
        </Alert>
      )}

      <Form className="mb-4">
        <div className="row g-2 align-items-end">
          <Form.Group className="col-md-5">
            <Form.Label className="fw-semibold fs-10">Email</Form.Label>
            <Form.Control
              type="email"
              size="sm"
              placeholder="user@example.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </Form.Group>
          <Form.Group className="col-md-5">
            <Form.Label className="fw-semibold fs-10">
              Roles (comma-separated)
            </Form.Label>
            <Form.Control
              type="text"
              size="sm"
              value={rolesInput}
              onChange={(e) => setRolesInput(e.target.value)}
            />
          </Form.Group>
          <Form.Group className="col-md-2">
            <Button
              variant="primary"
              size="sm"
              className="w-100"
              disabled={isCreating}
              onClick={onCreate}
            >
              {isCreating ? 'Creating…' : 'Invite'}
            </Button>
          </Form.Group>
        </div>
      </Form>

      {isLoading ? (
        <div className="text-center py-3">
          <Spinner size="sm" animation="border" />
        </div>
      ) : error ? (
        <Alert variant="danger" className="fs-10">
          Failed to load invites.
        </Alert>
      ) : (
        <Table size="sm" className="fs-10 mb-0">
          <thead className="bg-body-tertiary">
            <tr>
              <th>Email</th>
              <th>Roles</th>
              <th>Created</th>
              <th>Expires</th>
              <th className="text-end">Actions</th>
            </tr>
          </thead>
          <tbody>
            {invites.map((inv) => (
              <tr key={inv.id} className="align-middle">
                <td>{inv.email}</td>
                <td>{inv.roles.join(', ')}</td>
                <td className="text-muted">
                  {new Date(inv.createdAt).toLocaleDateString()}
                </td>
                <td className="text-muted">
                  {new Date(inv.expiresAt).toLocaleDateString()}
                </td>
                <td className="text-end">
                  <Button
                    variant="link"
                    size="sm"
                    className="p-0 text-danger text-decoration-none"
                    onClick={() => onRevoke(inv.id)}
                  >
                    Revoke
                  </Button>
                </td>
              </tr>
            ))}
            {invites.length === 0 && (
              <tr>
                <td colSpan={5} className="text-center text-muted py-3">
                  No pending invites.
                </td>
              </tr>
            )}
          </tbody>
        </Table>
      )}
    </>
  );
};

// --- Billing tab (ADR-0001 PR-4) ---
//
// Surfaces the optional billing.Customer linked via Customer.TenantUUID.
// The aggregator endpoint returns 404 when no link exists; the consumer
// (this component) renders that as the empty state with a "Create
// billing profile" call to action that hits the idempotent promote
// mutation. Most external tenants will hit the empty state until an
// operator promotes them.

const BillingTab: React.FC<{ org: AdminOrgListItem }> = ({ org }) => {
  const { data, error, isLoading, isFetching } = useGetTenantBillingCustomerAdminQuery(
    org.id,
  );
  const [promote, { isLoading: isPromoting }] =
    usePromoteTenantToBillingCustomerAdminMutation();

  const onCreate = async () => {
    try {
      await promote(org.id).unwrap();
      toast.success('Billing customer created');
    } catch (err: unknown) {
      toast.error('Create failed: ' + extractError(err));
    }
  };

  if (isLoading || isFetching) {
    return (
      <div className="text-center py-4">
        <Spinner size="sm" animation="border" />
      </div>
    );
  }

  // 404 = "not linked" empty state. Anything else is a real failure.
  const errorStatus =
    error && typeof error === 'object' && 'status' in error
      ? (error as { status?: number | string }).status
      : undefined;
  const isNotLinked = !data && errorStatus === 404;
  const isUnexpectedError = error && !isNotLinked;

  if (isUnexpectedError) {
    return (
      <Alert variant="danger" className="fs-10">
        Failed to load billing customer: {extractError(error)}
      </Alert>
    );
  }

  if (isNotLinked) {
    return (
      <>
        <Alert variant="info" className="fs-10 py-2">
          This tenant has no FatturaPA billing profile yet. Creating one
          pre-fills the new customer from the tenant's legal-name, VAT,
          fiscal code and country. Address fields stay empty — fill them
          in from the customer page before sending invoices.
        </Alert>
        <div className="d-flex justify-content-end">
          <Button
            variant="primary"
            size="sm"
            disabled={isPromoting}
            onClick={onCreate}
          >
            <FontAwesomeIcon icon="plus" className="me-1" />
            {isPromoting ? 'Creating…' : 'Create billing profile'}
          </Button>
        </div>
      </>
    );
  }

  // Linked — render the projection.
  const customer = data as TenantBillingCustomer;
  return (
    <Form className="px-1">
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">Customer ID</Form.Label>
        <Form.Control
          readOnly
          value={customer.uuid}
          className="fs-11 font-monospace"
        />
      </Form.Group>
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">Denomination</Form.Label>
        <Form.Control readOnly value={customer.denomination || '—'} />
      </Form.Group>
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">Fiscal ID code</Form.Label>
        <Form.Control
          readOnly
          value={customer.fiscalIdCode || '—'}
          className="fs-11 font-monospace"
        />
      </Form.Group>
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">Country</Form.Label>
        <Form.Control readOnly value={customer.country || '—'} />
      </Form.Group>
      <div className="mb-3">
        <SubtleBadge bg={customer.isActive ? 'success' : 'secondary'} pill>
          {customer.isActive ? 'active' : 'inactive'}
        </SubtleBadge>
      </div>
      <div className="d-flex justify-content-end">
        <Button
          as="a"
          href={`/billing/customers`}
          variant="outline-primary"
          size="sm"
          target="_blank"
          rel="noreferrer"
        >
          <FontAwesomeIcon icon="external-link-alt" className="me-1" />
          Open in Billing
        </Button>
      </div>
    </Form>
  );
};

function extractError(err: unknown): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || 'unknown error';
  }
  return String(err);
}

export default TenantDetailModal;

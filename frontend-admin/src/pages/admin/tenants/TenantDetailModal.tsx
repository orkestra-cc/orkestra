import { useEffect, useState } from 'react';
import {
  Alert,
  Button,
  Form,
  Modal,
  Spinner,
  Tab,
  Table,
  Tabs
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import { Trans, useTranslation } from 'react-i18next';
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
  type AdminOrgListItem,
  type Invite
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
  enterprise: ['*']
};

const ALL_FEATURES = [
  'billing',
  'documents',
  'company',
  'sales',
  'agents',
  'graph',
  'rag'
];

const planColors: Record<string, BadgeColor> = {
  free: 'secondary',
  pro: 'primary',
  enterprise: 'success'
};

type TenantTabKey = 'overview' | 'plan' | 'members' | 'invites';

const TenantDetailModal: React.FC<Props> = ({
  org,
  show,
  onHide,
  onDelete,
  onPurge
}) => {
  const { t } = useTranslation();
  const [tab, setTab] = useState<TenantTabKey>('overview');

  useEffect(() => {
    if (show) setTab('overview');
  }, [show, org?.id]);

  if (!org) return null;

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
              {t('adminTenants.detailModal.badgePurged')}
            </SubtleBadge>
          ) : org.deletedAt || org.status === 'archived' ? (
            <SubtleBadge bg="danger" pill>
              {t('adminTenants.detailModal.badgeDeleted')}
            </SubtleBadge>
          ) : null}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <Tabs
          id="tenant-detail-tabs"
          activeKey={tab}
          onSelect={k => setTab((k as TenantTabKey) || 'overview')}
          className="mb-3"
        >
          <Tab
            eventKey="overview"
            title={t('adminTenants.detailModal.tabs.overview')}
          >
            <OverviewTab org={org} />
          </Tab>
          <Tab eventKey="plan" title={t('adminTenants.detailModal.tabs.plan')}>
            <PlanTab org={org} />
          </Tab>
          <Tab
            eventKey="members"
            title={t('adminTenants.detailModal.tabs.members', {
              count: org.memberCount
            })}
          >
            <MembersTab org={org} />
          </Tab>
          <Tab
            eventKey="invites"
            title={t('adminTenants.detailModal.tabs.invites')}
          >
            <InvitesTab org={org} />
          </Tab>
        </Tabs>
      </Modal.Body>
      <Modal.Footer className="d-flex justify-content-between flex-wrap gap-2">
        <div className="d-flex gap-2 flex-wrap">
          {org.status !== 'purged' && !org.deletedAt && (
            <Button
              variant="outline-danger"
              size="sm"
              onClick={() => onDelete(org)}
            >
              <FontAwesomeIcon icon="trash" className="me-1" />
              {t('adminTenants.detailModal.deleteButton')}
            </Button>
          )}
          {org.status !== 'purged' && (
            <Button variant="danger" size="sm" onClick={() => onPurge(org)}>
              <FontAwesomeIcon icon="exclamation-triangle" className="me-1" />
              {t('adminTenants.detailModal.purgeButton')}
            </Button>
          )}
          {org.status === 'purged' && org.purgedAt && (
            <span className="text-muted fs-10">
              {t('adminTenants.detailModal.purgedFootnote', {
                date: new Date(org.purgedAt).toLocaleString()
              })}
            </span>
          )}
          {org.status !== 'purged' && org.deletedAt && (
            <span className="text-muted fs-10">
              {t('adminTenants.detailModal.deletedFootnote', {
                date: new Date(org.deletedAt).toLocaleString()
              })}
            </span>
          )}
        </div>
        <Button variant="secondary" onClick={onHide}>
          {t('adminTenants.detailModal.close')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

// --- Overview tab ---

const OverviewTab: React.FC<{ org: AdminOrgListItem }> = ({ org }) => {
  const { t } = useTranslation();
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
          slug: slug !== org.slug ? slug : undefined
        }
      }).unwrap();
      toast.success(t('adminTenants.detailModal.overview.successToast'));
    } catch (err: unknown) {
      toast.error(
        t('adminTenants.detailModal.overview.errorToast', {
          message: extractError(
            err,
            t('adminTenants.detailModal.overview.unknownError')
          )
        })
      );
    }
  };

  return (
    <Form className="px-1">
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">
          {t('adminTenants.detailModal.overview.tenantIdLabel')}
        </Form.Label>
        <Form.Control
          readOnly
          value={org.id}
          className="fs-11 font-monospace"
        />
      </Form.Group>
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">
          {t('adminTenants.detailModal.overview.nameLabel')}
        </Form.Label>
        <Form.Control value={name} onChange={e => setName(e.target.value)} />
      </Form.Group>
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">
          {t('adminTenants.detailModal.overview.slugLabel')}
        </Form.Label>
        <Form.Control value={slug} onChange={e => setSlug(e.target.value)} />
      </Form.Group>
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">
          {t('adminTenants.detailModal.overview.ownerLabel')}
        </Form.Label>
        <Form.Control
          readOnly
          value={
            org.ownerUserUUID ||
            t('adminTenants.detailModal.overview.ownerDash')
          }
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
          {isLoading
            ? t('adminTenants.detailModal.overview.saving')
            : t('adminTenants.detailModal.overview.save')}
        </Button>
      </div>
    </Form>
  );
};

// --- Plan tab ---

const PlanTab: React.FC<{ org: AdminOrgListItem }> = ({ org }) => {
  const { t } = useTranslation();
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
    setFeatures(prev =>
      prev.includes(feature)
        ? prev.filter(f => f !== feature)
        : [...prev, feature]
    );
  };

  const onSave = async () => {
    try {
      await updatePlan({ tenantId: org.id, body: { plan, features } }).unwrap();
      toast.success(t('adminTenants.detailModal.plan.successToast'));
    } catch (err: unknown) {
      toast.error(
        t('adminTenants.detailModal.plan.errorToast', {
          message: extractError(
            err,
            t('adminTenants.detailModal.plan.unknownError')
          )
        })
      );
    }
  };

  const hasWildcard = features.includes('*');

  return (
    <Form className="px-1">
      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">
          {t('adminTenants.detailModal.plan.planLabel')}
        </Form.Label>
        <Form.Select
          value={plan}
          onChange={e => handlePlanChange(e.target.value)}
        >
          <option value="free">
            {t('adminTenants.detailModal.plan.planFree')}
          </option>
          <option value="pro">
            {t('adminTenants.detailModal.plan.planPro')}
          </option>
          <option value="enterprise">
            {t('adminTenants.detailModal.plan.planEnterprise')}
          </option>
        </Form.Select>
        <Form.Text muted>
          {t('adminTenants.detailModal.plan.planHelp')}
        </Form.Text>
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label className="fw-semibold fs-10">
          {t('adminTenants.detailModal.plan.featuresLabel')}
        </Form.Label>
        {hasWildcard && (
          <Alert variant="info" className="fs-10 py-2">
            <Trans
              i18nKey="adminTenants.detailModal.plan.wildcardInfo"
              components={{ code: <code /> }}
            />
          </Alert>
        )}
        <div className="d-flex flex-wrap gap-3">
          {ALL_FEATURES.map(feature => (
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
        <Button
          variant="primary"
          size="sm"
          disabled={isLoading}
          onClick={onSave}
        >
          {isLoading
            ? t('adminTenants.detailModal.plan.saving')
            : t('adminTenants.detailModal.plan.save')}
        </Button>
      </div>
    </Form>
  );
};

// --- Members tab ---

const MembersTab: React.FC<{ org: AdminOrgListItem }> = ({ org }) => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useListOrgMembersAdminQuery(org.id);
  const [removeMember] = useRemoveOrgMemberAdminMutation();

  const onRemove = async (userUUID: string) => {
    try {
      await removeMember({ tenantId: org.id, userUUID }).unwrap();
      toast.success(t('adminTenants.detailModal.members.successToast'));
    } catch (err: unknown) {
      toast.error(
        t('adminTenants.detailModal.members.errorToast', {
          message: extractError(
            err,
            t('adminTenants.detailModal.members.unknownError')
          )
        })
      );
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
        {t('adminTenants.detailModal.members.loadError')}
      </Alert>
    );
  }

  const members = data?.members ?? [];

  return (
    <>
      <Alert variant="info" className="fs-10 py-2">
        <Trans
          i18nKey="adminTenants.detailModal.members.rolesPageHint"
          components={{ a: <a href="/admin/roles" /> }}
        />
      </Alert>
      <Table size="sm" className="fs-10 mb-0">
        <thead className="bg-body-tertiary">
          <tr>
            <th>{t('adminTenants.detailModal.members.colUserUuid')}</th>
            <th>{t('adminTenants.detailModal.members.colRoles')}</th>
            <th>{t('adminTenants.detailModal.members.colJoined')}</th>
            <th>{t('adminTenants.detailModal.members.colOwner')}</th>
            <th className="text-end">
              {t('adminTenants.detailModal.members.colActions')}
            </th>
          </tr>
        </thead>
        <tbody>
          {members.map(m => (
            <tr key={m.id} className="align-middle">
              <td className="font-monospace fs-11">{m.userUUID}</td>
              <td>
                {m.roles.join(', ') ||
                  t('adminTenants.detailModal.members.dash')}
              </td>
              <td className="text-muted">
                {m.joinedAt
                  ? new Date(m.joinedAt).toLocaleDateString()
                  : t('adminTenants.detailModal.members.dash')}
              </td>
              <td>
                {m.isOwner && (
                  <SubtleBadge bg="primary" pill>
                    {t('adminTenants.detailModal.members.ownerBadge')}
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
                    {t('adminTenants.detailModal.members.remove')}
                  </Button>
                )}
              </td>
            </tr>
          ))}
          {members.length === 0 && (
            <tr>
              <td colSpan={5} className="text-center text-muted py-3">
                {t('adminTenants.detailModal.members.empty')}
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
  const { t } = useTranslation();
  const { data, isLoading, error } = useListOrgInvitesAdminQuery({
    tenantId: org.id
  });
  const [createInvite, { isLoading: isCreating }] =
    useCreateOrgInviteAdminMutation();
  const [revokeInvite] = useRevokeOrgInviteAdminMutation();

  const [email, setEmail] = useState('');
  const [rolesInput, setRolesInput] = useState('operator');
  const [freshInvite, setFreshInvite] = useState<Invite | null>(null);

  const onCreate = async () => {
    const roles = rolesInput
      .split(',')
      .map(r => r.trim())
      .filter(Boolean);
    if (!email || roles.length === 0) {
      toast.error(t('adminTenants.detailModal.invites.validationRequired'));
      return;
    }
    try {
      const inv = await createInvite({
        tenantId: org.id,
        body: { email, roles }
      }).unwrap();
      setFreshInvite(inv);
      setEmail('');
      toast.success(t('adminTenants.detailModal.invites.createSuccessToast'));
    } catch (err: unknown) {
      toast.error(
        t('adminTenants.detailModal.invites.createErrorToast', {
          message: extractError(
            err,
            t('adminTenants.detailModal.invites.unknownError')
          )
        })
      );
    }
  };

  const onRevoke = async (inviteId: string) => {
    try {
      await revokeInvite({ tenantId: org.id, inviteId }).unwrap();
      toast.success(t('adminTenants.detailModal.invites.revokeSuccessToast'));
    } catch (err: unknown) {
      toast.error(
        t('adminTenants.detailModal.invites.revokeErrorToast', {
          message: extractError(
            err,
            t('adminTenants.detailModal.invites.unknownError')
          )
        })
      );
    }
  };

  const copyToken = () => {
    if (!freshInvite?.token) return;
    navigator.clipboard.writeText(freshInvite.token);
    toast.success(t('adminTenants.detailModal.invites.copySuccessToast'));
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
          <strong>
            {t('adminTenants.detailModal.invites.revealTokenIntro')}
          </strong>
          <div className="d-flex align-items-center gap-2 mt-2">
            <code className="flex-grow-1 fs-11 text-break">
              {freshInvite.token}
            </code>
            <Button size="sm" variant="outline-dark" onClick={copyToken}>
              <FontAwesomeIcon icon="copy" className="me-1" />
              {t('adminTenants.detailModal.invites.copyButton')}
            </Button>
          </div>
        </Alert>
      )}

      <Form className="mb-4">
        <div className="row g-2 align-items-end">
          <Form.Group className="col-md-5">
            <Form.Label className="fw-semibold fs-10">
              {t('adminTenants.detailModal.invites.emailLabel')}
            </Form.Label>
            <Form.Control
              type="email"
              size="sm"
              placeholder={t(
                'adminTenants.detailModal.invites.emailPlaceholder'
              )}
              value={email}
              onChange={e => setEmail(e.target.value)}
            />
          </Form.Group>
          <Form.Group className="col-md-5">
            <Form.Label className="fw-semibold fs-10">
              {t('adminTenants.detailModal.invites.rolesLabel')}
            </Form.Label>
            <Form.Control
              type="text"
              size="sm"
              value={rolesInput}
              onChange={e => setRolesInput(e.target.value)}
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
              {isCreating
                ? t('adminTenants.detailModal.invites.creating')
                : t('adminTenants.detailModal.invites.createButton')}
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
          {t('adminTenants.detailModal.invites.loadError')}
        </Alert>
      ) : (
        <Table size="sm" className="fs-10 mb-0">
          <thead className="bg-body-tertiary">
            <tr>
              <th>{t('adminTenants.detailModal.invites.colEmail')}</th>
              <th>{t('adminTenants.detailModal.invites.colRoles')}</th>
              <th>{t('adminTenants.detailModal.invites.colCreated')}</th>
              <th>{t('adminTenants.detailModal.invites.colExpires')}</th>
              <th className="text-end">
                {t('adminTenants.detailModal.invites.colActions')}
              </th>
            </tr>
          </thead>
          <tbody>
            {invites.map(inv => (
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
                    {t('adminTenants.detailModal.invites.revoke')}
                  </Button>
                </td>
              </tr>
            ))}
            {invites.length === 0 && (
              <tr>
                <td colSpan={5} className="text-center text-muted py-3">
                  {t('adminTenants.detailModal.invites.empty')}
                </td>
              </tr>
            )}
          </tbody>
        </Table>
      )}
    </>
  );
};

function extractError(err: unknown, fallback: string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || fallback;
  }
  return String(err);
}

export default TenantDetailModal;

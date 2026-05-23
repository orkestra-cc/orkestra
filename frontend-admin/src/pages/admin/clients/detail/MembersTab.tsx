import { useState } from 'react';
import { Alert, Dropdown, Spinner, Table } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import SubtleBadge from 'components/common/SubtleBadge';
import type { Org } from 'store/api/tenantApi';
import {
  useListOrgMembersAdminQuery,
  useRemoveOrgMemberAdminMutation
} from 'store/api/tenantApi';
import {
  useResendVerificationClientUserAdminMutation,
  useSendPasswordResetClientUserAdminMutation
} from 'store/api/userApi';
import { Button } from 'react-bootstrap';
import AttachMemberModal from './AttachMemberModal';
import AdminResetMfaModal from 'pages/admin/users/AdminResetMfaModal';

interface Props {
  org: Org;
}

interface MfaTarget {
  id: string;
  email: string;
  fullName?: string;
}

/**
 * Members tab — mirrors the MembersTab from the legacy TenantDetailModal.
 * Role assignments live on the Role Management page; this tab only shows
 * current memberships and allows removing non-owner rows. The per-row
 * actions menu also exposes the admin trigger surface that previously
 * lived on the retired user-detail page (resend verification, send
 * password reset, reset MFA) so operators can recover a Tier-2 client
 * user without leaving the tenant.
 */
const MembersTab: React.FC<Props> = ({ org }) => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useListOrgMembersAdminQuery(org.id);
  const [removeMember] = useRemoveOrgMemberAdminMutation();
  const [resendVerification] = useResendVerificationClientUserAdminMutation();
  const [sendPasswordReset] = useSendPasswordResetClientUserAdminMutation();
  const [attachOpen, setAttachOpen] = useState(false);
  const [mfaTarget, setMfaTarget] = useState<MfaTarget | null>(null);

  const unknownErr = t('adminClients.members.errorUnknown');

  const onRemove = async (userUUID: string) => {
    try {
      await removeMember({ tenantId: org.id, userUUID }).unwrap();
      toast.success(t('adminClients.members.toastRemoved'));
    } catch (err: unknown) {
      toast.error(
        t('adminClients.members.toastRemoveFailed', {
          error: extractError(err, unknownErr)
        })
      );
    }
  };

  const onResendVerification = async (userUUID: string) => {
    try {
      await resendVerification(userUUID).unwrap();
      toast.success(t('adminClients.members.toastVerificationSent'));
    } catch (err: unknown) {
      toast.error(
        t('adminClients.members.toastResendFailed', {
          error: extractError(err, unknownErr)
        })
      );
    }
  };

  const onSendPasswordReset = async (userUUID: string) => {
    try {
      await sendPasswordReset(userUUID).unwrap();
      toast.success(t('adminClients.members.toastPasswordResetSent'));
    } catch (err: unknown) {
      toast.error(
        t('adminClients.members.toastSendFailed', {
          error: extractError(err, unknownErr)
        })
      );
    }
  };

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" size="sm" />
      </div>
    );
  }
  if (error) {
    return (
      <Alert variant="danger" className="fs-10">
        {t('adminClients.members.loadFailed')}
      </Alert>
    );
  }

  const members = data?.members ?? [];

  return (
    <>
      <div className="d-flex justify-content-between align-items-start mb-2 gap-3">
        <Alert variant="info" className="fs-10 py-2 mb-0 flex-grow-1">
          <Trans
            i18nKey="adminClients.members.intro"
            components={{ link: <a href="/admin/roles" /> }}
          />
        </Alert>
        <Button
          variant="primary"
          size="sm"
          onClick={() => setAttachOpen(true)}
          className="flex-shrink-0"
        >
          {t('adminClients.members.attachButton')}
        </Button>
      </div>
      <Table size="sm" className="fs-10 mb-0">
        <thead className="bg-body-tertiary">
          <tr>
            <th>{t('adminClients.members.colEmail')}</th>
            <th>{t('adminClients.members.colUserUUID')}</th>
            <th>{t('adminClients.members.colRoles')}</th>
            <th>{t('adminClients.members.colJoined')}</th>
            <th>{t('adminClients.members.colOwner')}</th>
            <th className="text-end">{t('adminClients.members.colActions')}</th>
          </tr>
        </thead>
        <tbody>
          {members.map(m => (
            <tr key={m.id} className="align-middle">
              <td>{m.email || <span className="text-muted">—</span>}</td>
              <td className="font-monospace fs-11">{m.userUUID}</td>
              <td>{m.roles.join(', ') || '—'}</td>
              <td className="text-muted">
                {m.joinedAt ? new Date(m.joinedAt).toLocaleDateString() : '—'}
              </td>
              <td>
                {m.isOwner && (
                  <SubtleBadge bg="primary" pill>
                    {t('adminClients.members.ownerBadge')}
                  </SubtleBadge>
                )}
              </td>
              <td className="text-end">
                <Dropdown align="end">
                  <Dropdown.Toggle
                    variant="orkestra-default"
                    size="sm"
                    className="border-0"
                  >
                    <FontAwesomeIcon icon="ellipsis-h" className="fs-11" />
                  </Dropdown.Toggle>
                  <Dropdown.Menu className="border py-0">
                    <div className="py-1">
                      <Dropdown.Item
                        as="button"
                        type="button"
                        onClick={() => onResendVerification(m.userUUID)}
                      >
                        <FontAwesomeIcon icon="envelope" className="me-2" />
                        {t('adminClients.members.actionResendVerification')}
                      </Dropdown.Item>
                      <Dropdown.Item
                        as="button"
                        type="button"
                        onClick={() => onSendPasswordReset(m.userUUID)}
                      >
                        <FontAwesomeIcon icon="key" className="me-2" />
                        {t('adminClients.members.actionSendPasswordReset')}
                      </Dropdown.Item>
                      <Dropdown.Item
                        as="button"
                        type="button"
                        onClick={() =>
                          setMfaTarget({
                            id: m.userUUID,
                            email: m.email || m.userUUID
                          })
                        }
                      >
                        <FontAwesomeIcon icon="shield-alt" className="me-2" />
                        {t('adminClients.members.actionResetMfa')}
                      </Dropdown.Item>
                      {!m.isOwner && (
                        <>
                          <Dropdown.Divider className="my-1" />
                          <Dropdown.Item
                            as="button"
                            type="button"
                            className="text-danger"
                            onClick={() => onRemove(m.userUUID)}
                          >
                            <FontAwesomeIcon
                              icon="trash-alt"
                              className="me-2"
                            />
                            {t('adminClients.members.actionRemove')}
                          </Dropdown.Item>
                        </>
                      )}
                    </div>
                  </Dropdown.Menu>
                </Dropdown>
              </td>
            </tr>
          ))}
          {members.length === 0 && (
            <tr>
              <td colSpan={6} className="text-center text-muted py-3">
                {t('adminClients.members.empty')}
              </td>
            </tr>
          )}
        </tbody>
      </Table>
      <AttachMemberModal
        org={org}
        show={attachOpen}
        onHide={() => setAttachOpen(false)}
      />
      <AdminResetMfaModal
        show={!!mfaTarget}
        user={mfaTarget}
        tier="client"
        onHide={() => setMfaTarget(null)}
      />
    </>
  );
};

function extractError(err: unknown, unknownLabel: string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || unknownLabel;
  }
  return String(err);
}

export default MembersTab;

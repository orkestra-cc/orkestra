import { useState } from 'react';
import { Alert, Button, Form, Modal, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import { type Org, useAttachOrgMemberAdminMutation } from 'store/api/tenantApi';

interface Props {
  org: Org;
  show: boolean;
  onHide: () => void;
}

// Tenant-scoped role names recognized by authz.SeedSystemRoles. Custom
// roles are out of scope for v1 admin-attach — operators wanting a custom
// role still drive that through the Role Management page after the user is
// a member.
const ROLE_VALUES = [
  'org_member',
  'org_viewer',
  'org_billing',
  'org_admin',
  'org_owner'
] as const;

/**
 * AttachMemberModal — operator-side direct grant for tenant memberships.
 * Mirrors the backend POST /v1/admin/tenants/{id}/members handler. Looks
 * up the user by email by default (the common operator workflow); the
 * advanced "by UUID" toggle exists for cases where the email lookup ran
 * against a different audience (operator vs client) and the operator
 * already knows the UUID.
 */
const AttachMemberModal: React.FC<Props> = ({ org, show, onHide }) => {
  const { t } = useTranslation();
  const [byUUID, setByUUID] = useState(false);
  const [userEmail, setUserEmail] = useState('');
  const [userUUID, setUserUUID] = useState('');
  const [role, setRole] = useState<string>('org_member');
  const [isOwner, setIsOwner] = useState(false);
  const [errMsg, setErrMsg] = useState<string | null>(null);

  const [attach, { isLoading }] = useAttachOrgMemberAdminMutation();

  const reset = () => {
    setUserEmail('');
    setUserUUID('');
    setRole('org_member');
    setIsOwner(false);
    setByUUID(false);
    setErrMsg(null);
  };

  const handleHide = () => {
    if (isLoading) return;
    reset();
    onHide();
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrMsg(null);

    const body: {
      userUuid?: string;
      userEmail?: string;
      role: string;
      isOwner: boolean;
    } = { role, isOwner };
    if (byUUID) {
      const v = userUUID.trim();
      if (!v) {
        setErrMsg(t('adminClients.attachMember.errorUserUUIDRequired'));
        return;
      }
      body.userUuid = v;
    } else {
      const v = userEmail.trim();
      if (!v) {
        setErrMsg(t('adminClients.attachMember.errorEmailRequired'));
        return;
      }
      body.userEmail = v;
    }

    try {
      await attach({ tenantId: org.id, body }).unwrap();
      toast.success(t('adminClients.attachMember.toastAttached'));
      reset();
      onHide();
    } catch (err: unknown) {
      setErrMsg(
        extractError(err, {
          notFound: t('adminClients.attachMember.errorNotFound'),
          conflict: t('adminClients.attachMember.errorConflict'),
          generic: t('adminClients.attachMember.errorGeneric')
        })
      );
    }
  };

  return (
    <Modal show={show} onHide={handleHide} centered>
      <Form onSubmit={handleSubmit}>
        <Modal.Header closeButton>
          <Modal.Title className="fs-9">
            {t('adminClients.attachMember.title')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {errMsg && (
            <Alert variant="danger" className="fs-10 py-2">
              {errMsg}
            </Alert>
          )}

          <div className="mb-3 d-flex gap-3 fs-10">
            <Form.Check
              type="radio"
              id="lookup-email"
              label={t('adminClients.attachMember.byEmail')}
              checked={!byUUID}
              onChange={() => setByUUID(false)}
            />
            <Form.Check
              type="radio"
              id="lookup-uuid"
              label={t('adminClients.attachMember.byUUID')}
              checked={byUUID}
              onChange={() => setByUUID(true)}
            />
          </div>

          {byUUID ? (
            <Form.Group className="mb-3">
              <Form.Label className="fs-10">
                {t('adminClients.attachMember.labelUserUUID')}
              </Form.Label>
              <Form.Control
                type="text"
                value={userUUID}
                onChange={e => setUserUUID(e.target.value)}
                placeholder={t('adminClients.attachMember.placeholderUUID')}
                autoFocus
              />
              <Form.Text className="text-muted fs-11">
                {t('adminClients.attachMember.helpUUID')}
              </Form.Text>
            </Form.Group>
          ) : (
            <Form.Group className="mb-3">
              <Form.Label className="fs-10">
                {t('adminClients.attachMember.labelUserEmail')}
              </Form.Label>
              <Form.Control
                type="email"
                value={userEmail}
                onChange={e => setUserEmail(e.target.value)}
                placeholder={t('adminClients.attachMember.placeholderEmail')}
                autoFocus
              />
              <Form.Text className="text-muted fs-11">
                {org.kind === 'external'
                  ? t('adminClients.attachMember.helpEmailClient')
                  : t('adminClients.attachMember.helpEmailOperator')}
              </Form.Text>
            </Form.Group>
          )}

          <Form.Group className="mb-3">
            <Form.Label className="fs-10">
              {t('adminClients.attachMember.labelRole')}
            </Form.Label>
            <Form.Select value={role} onChange={e => setRole(e.target.value)}>
              {ROLE_VALUES.map(v => (
                <option key={v} value={v}>
                  {t('adminClients.attachMember.roleOption', {
                    label: t(`adminClients.attachMember.roles.${v}`),
                    value: v
                  })}
                </option>
              ))}
            </Form.Select>
          </Form.Group>

          <Form.Group className="mb-1">
            <Form.Check
              type="checkbox"
              id="attach-isowner"
              label={t('adminClients.attachMember.labelIsOwner')}
              checked={isOwner}
              onChange={e => setIsOwner(e.target.checked)}
            />
            <Form.Text className="text-muted fs-11">
              {t('adminClients.attachMember.helpIsOwner')}
            </Form.Text>
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="link" onClick={handleHide} disabled={isLoading}>
            {t('adminClients.attachMember.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={isLoading}>
            {isLoading ? (
              <>
                <Spinner animation="border" size="sm" className="me-2" />
                {t('adminClients.attachMember.attaching')}
              </>
            ) : (
              t('adminClients.attachMember.attach')
            )}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

interface ErrorLabels {
  notFound: string;
  conflict: string;
  generic: string;
}

function extractError(err: unknown, labels: ErrorLabels): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    if (data?.detail) return data.detail;
    if (data?.title) return data.title;
  }
  if (err && typeof err === 'object' && 'status' in err) {
    const status = (err as { status?: number | string }).status;
    if (status === 404) return labels.notFound;
    if (status === 409) return labels.conflict;
  }
  return labels.generic;
}

export default AttachMemberModal;

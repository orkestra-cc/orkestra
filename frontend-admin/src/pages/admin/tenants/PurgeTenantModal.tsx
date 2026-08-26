import { useEffect, useState } from 'react';
import { Alert, Button, Form, Modal, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import { Trans, useTranslation } from 'react-i18next';
import {
  usePurgeOrgAdminMutation,
  type AdminOrgListItem
} from 'store/api/tenantApi';

interface Props {
  org: AdminOrgListItem | null;
  show: boolean;
  onHide: () => void;
  /** Called after the purge request succeeds, so the parent can close any
   *  upstream modals or refresh its own state. */
  onPurged?: () => void;
}

// Purge is the terminal, irreversible lifecycle transition. The backend
// flips status to `purged` AND crypto-shreds the tenant's KMS key — every
// ciphertext sealed with that key becomes mathematically unrecoverable.
// The modal implements a double-confirm: the operator must read the
// warning AND type the tenant slug verbatim before the confirm button
// lights up.
type Stage = 'warn' | 'type-slug';

const PurgeTenantModal: React.FC<Props> = ({ org, show, onHide, onPurged }) => {
  const { t } = useTranslation();
  const [purgeOrg, { isLoading }] = usePurgeOrgAdminMutation();
  const [stage, setStage] = useState<Stage>('warn');
  const [typed, setTyped] = useState('');

  useEffect(() => {
    if (show) {
      setStage('warn');
      setTyped('');
    }
  }, [show]);

  if (!org) return null;

  // The platform default guards every lifecycle mutation on the backend
  // (409 tenant.default_reassignment_required) until it's reassigned
  // elsewhere — never let the wizard advance toward a confirm that would
  // just fail. isDefault is derived straight from the org payload, never
  // local state.
  const isDefault = !!org.isDefault;
  const slugMatches = typed.trim() === org.slug;

  const handleClose = () => {
    if (isLoading) return;
    onHide();
  };

  const onConfirm = async () => {
    if (isDefault) return;
    try {
      await purgeOrg(org.id).unwrap();
      toast.success(
        t('adminTenants.purgeModal.successToast', { name: org.name })
      );
      onPurged?.();
      onHide();
    } catch (err: unknown) {
      toast.error(
        t('adminTenants.purgeModal.errorToast', {
          message: extractError(err, t)
        })
      );
    }
  };

  return (
    <Modal
      show={show}
      onHide={handleClose}
      centered
      backdrop={isLoading ? 'static' : true}
    >
      <Modal.Header closeButton={!isLoading} className="bg-danger-subtle">
        <Modal.Title className="fs-8 text-danger">
          <FontAwesomeIcon icon="exclamation-triangle" className="me-2" />
          {t('adminTenants.purgeModal.title')}
        </Modal.Title>
      </Modal.Header>

      {stage === 'warn' && (
        <>
          <Modal.Body className="fs-10">
            {isDefault ? (
              <Alert variant="warning" className="fs-10 mb-0">
                <FontAwesomeIcon icon="info-circle" className="me-2" />
                {t('adminTenants.default.reassignFirst')}
              </Alert>
            ) : (
              <>
                <Alert variant="danger" className="fs-10 mb-3">
                  <strong>{t('adminTenants.purgeModal.alertHeading')}</strong>
                  <br />
                  {t('adminTenants.purgeModal.alertBody')}
                </Alert>
                <p className="mb-2">
                  <Trans
                    i18nKey="adminTenants.purgeModal.aboutToPurge"
                    values={{ name: org.name, slug: org.slug }}
                    components={{
                      code: <code className="fs-9" />,
                      muted: <span className="text-body-tertiary" />
                    }}
                  />
                </p>
                <ul className="mb-0">
                  <li>
                    <Trans
                      i18nKey="adminTenants.purgeModal.consequenceStatus"
                      components={{ code: <code /> }}
                    />
                  </li>
                  <li>{t('adminTenants.purgeModal.consequenceKms')}</li>
                  <li>{t('adminTenants.purgeModal.consequenceNoUndo')}</li>
                  <li>{t('adminTenants.purgeModal.consequenceAudit')}</li>
                </ul>
              </>
            )}
          </Modal.Body>
          <Modal.Footer>
            <Button variant="outline-secondary" onClick={handleClose}>
              {t('adminTenants.purgeModal.cancel')}
            </Button>
            <Button
              variant="danger"
              onClick={() => setStage('type-slug')}
              disabled={isDefault}
            >
              {t('adminTenants.purgeModal.iUnderstand')}
            </Button>
          </Modal.Footer>
        </>
      )}

      {stage === 'type-slug' && (
        <>
          <Modal.Body className="fs-10">
            <p className="mb-2">{t('adminTenants.purgeModal.typePrompt')}</p>
            <p className="fs-11 text-body-tertiary mb-3">
              <code>{org.slug}</code>
            </p>
            <Form.Control
              autoFocus
              type="text"
              value={typed}
              onChange={e => setTyped(e.target.value)}
              placeholder={org.slug}
              isInvalid={typed.length > 0 && !slugMatches}
              isValid={slugMatches}
            />
            <Form.Control.Feedback type="invalid">
              {t('adminTenants.purgeModal.slugMismatch')}
            </Form.Control.Feedback>
          </Modal.Body>
          <Modal.Footer>
            <Button
              variant="outline-secondary"
              onClick={handleClose}
              disabled={isLoading}
            >
              {t('adminTenants.purgeModal.cancel')}
            </Button>
            <Button
              variant="danger"
              onClick={onConfirm}
              disabled={!slugMatches || isLoading}
            >
              {isLoading ? (
                <>
                  <Spinner animation="border" size="sm" className="me-2" />
                  {t('adminTenants.purgeModal.purging')}
                </>
              ) : (
                <>{t('adminTenants.purgeModal.purge')}</>
              )}
            </Button>
          </Modal.Footer>
        </>
      )}
    </Modal>
  );
};

function extractError(err: unknown, t: (key: string) => string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return (
      data?.detail || data?.title || t('adminTenants.purgeModal.unknownError')
    );
  }
  return String(err);
}

export default PurgeTenantModal;

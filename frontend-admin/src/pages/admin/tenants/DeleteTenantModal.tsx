import { useEffect, useState } from 'react';
import { Alert, Button, Form, Modal, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import { Trans, useTranslation } from 'react-i18next';
import {
  useDeleteOrgAdminMutation,
  type AdminOrgListItem
} from 'store/api/tenantApi';

interface Props {
  org: AdminOrgListItem | null;
  show: boolean;
  onHide: () => void;
}

const DeleteTenantModal: React.FC<Props> = ({ org, show, onHide }) => {
  const { t } = useTranslation();
  const [deleteOrg, { isLoading }] = useDeleteOrgAdminMutation();
  const [confirmText, setConfirmText] = useState('');

  useEffect(() => {
    if (!show) setConfirmText('');
  }, [show]);

  const canDelete = !!org && confirmText === org.slug && !isLoading;

  const onConfirm = async () => {
    if (!org) return;
    try {
      await deleteOrg(org.id).unwrap();
      toast.success(
        t('adminTenants.deleteModal.successToast', { name: org.name })
      );
      onHide();
    } catch (err: unknown) {
      toast.error(
        t('adminTenants.deleteModal.errorToast', {
          message: extractError(err, t)
        })
      );
    }
  };

  return (
    <Modal show={show && !!org} onHide={onHide} backdrop="static" centered>
      <Modal.Header closeButton>
        <Modal.Title>
          <FontAwesomeIcon icon="trash" className="text-danger me-2" />
          {t('adminTenants.deleteModal.title')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <p className="mb-3">
          <Trans
            i18nKey="adminTenants.deleteModal.intro"
            values={{ name: org?.name }}
            components={{ code: <code className="fs-9" /> }}
          />
        </p>
        <Alert variant="warning" className="fs-10 mb-3">
          <Trans
            i18nKey="adminTenants.deleteModal.bindingsRemain"
            components={{ strong: <strong />, em: <em /> }}
          />
        </Alert>
        <Form.Group>
          <Form.Label className="fw-semibold fs-10">
            <Trans
              i18nKey="adminTenants.deleteModal.typeToConfirm"
              values={{ slug: org?.slug }}
              components={{ code: <code /> }}
            />
          </Form.Label>
          <Form.Control
            type="text"
            value={confirmText}
            onChange={e => setConfirmText(e.target.value)}
            placeholder={org?.slug}
          />
        </Form.Group>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isLoading}>
          {t('adminTenants.deleteModal.cancel')}
        </Button>
        <Button variant="danger" onClick={onConfirm} disabled={!canDelete}>
          {isLoading ? (
            <>
              <Spinner size="sm" animation="border" className="me-2" />{' '}
              {t('adminTenants.deleteModal.deleting')}
            </>
          ) : (
            <>{t('adminTenants.deleteModal.delete')}</>
          )}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

function extractError(err: unknown, t: (key: string) => string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return (
      data?.detail || data?.title || t('adminTenants.deleteModal.unknownError')
    );
  }
  return String(err);
}

export default DeleteTenantModal;

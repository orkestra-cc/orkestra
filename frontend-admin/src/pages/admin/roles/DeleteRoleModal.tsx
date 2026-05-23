import { Alert, Button, Modal, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import { Trans, useTranslation } from 'react-i18next';
import { useDeleteRoleMutation, type Role } from 'store/api/tenantApi';

interface Props {
  tenantId: string;
  role: Role | null;
  show: boolean;
  onHide: () => void;
}

/**
 * DeleteRoleModal is a typed confirm dialog that replaces the old
 * `window.confirm` flow so the delete cascade warning is visible, the
 * destination role is named inline, and the submit button carries a
 * loading spinner while the PATCH+DELETE round-trip is in flight.
 *
 * System roles never land here — the delete button is hidden for them in
 * RolesTable. We still gate on `role.isSystem` as a defensive noop.
 */
const DeleteRoleModal: React.FC<Props> = ({ tenantId, role, show, onHide }) => {
  const { t } = useTranslation();
  const [deleteRole, { isLoading }] = useDeleteRoleMutation();

  const onConfirm = async () => {
    if (!role || role.isSystem) {
      onHide();
      return;
    }
    try {
      await deleteRole({ tenantId, roleId: role.id }).unwrap();
      toast.success(
        t('adminRoles.deleteModal.successToast', { name: role.name })
      );
      onHide();
    } catch (err: unknown) {
      toast.error(
        t('adminRoles.deleteModal.errorToast', {
          message: extractError(err, t)
        })
      );
    }
  };

  return (
    <Modal show={show && !!role} onHide={onHide} backdrop="static" centered>
      <Modal.Header closeButton>
        <Modal.Title>
          <FontAwesomeIcon icon="trash" className="text-danger me-2" />
          {t('adminRoles.deleteModal.title')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <p className="mb-3">
          <Trans
            i18nKey="adminRoles.deleteModal.intro"
            values={{ name: role?.name }}
            components={{ code: <code className="fs-9" /> }}
          />
        </p>
        <Alert variant="warning" className="fs-10 mb-0">
          <Trans
            i18nKey="adminRoles.deleteModal.warning"
            components={{ strong: <strong /> }}
          />
        </Alert>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isLoading}>
          {t('adminRoles.deleteModal.cancel')}
        </Button>
        <Button
          variant="danger"
          onClick={onConfirm}
          disabled={isLoading || !role}
        >
          {isLoading ? (
            <>
              <Spinner size="sm" animation="border" className="me-2" />{' '}
              {t('adminRoles.deleteModal.deleting')}
            </>
          ) : (
            <>{t('adminRoles.deleteModal.delete')}</>
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
      data?.detail || data?.title || t('adminRoles.deleteModal.unknownError')
    );
  }
  return String(err);
}

export default DeleteRoleModal;

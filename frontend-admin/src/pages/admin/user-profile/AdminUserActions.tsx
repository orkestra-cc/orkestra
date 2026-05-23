import React, { useState } from 'react';
import {
  Card,
  Button,
  Dropdown,
  Form,
  Modal,
  Spinner,
  Alert
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';
import { User, useUpdateUserMutation } from 'store/api/userApi';
import { toast } from 'react-toastify';

const ROLE_VALUES = [
  'guest',
  'operator',
  'manager',
  'developer',
  'administrator',
  'super_admin'
] as const;

interface AdminUserActionsProps {
  user: User;
}

const AdminUserActions: React.FC<AdminUserActionsProps> = ({ user }) => {
  const { t } = useTranslation();
  const [updateUser, { isLoading: isUpdating }] = useUpdateUserMutation();
  const [showRoleModal, setShowRoleModal] = useState(false);
  const [showProfileModal, setShowProfileModal] = useState(false);
  const [showLockModal, setShowLockModal] = useState(false);
  const [selectedRole, setSelectedRole] = useState(user.role);
  const [updateError, setUpdateError] = useState<string | null>(null);
  const [profileFormData, setProfileFormData] = useState({
    email: user.email || '',
    fullName: user.fullName || '',
    phone: user.phone || ''
  });

  const roles = ROLE_VALUES.map(value => ({
    value,
    label: t(`adminUsers.roles.${value}`),
    description: t(`adminUserProfile.userActions.roleDescriptions.${value}`)
  }));

  const handleToggleAccountLock = async () => {
    try {
      setUpdateError(null);
      const newStatus = !user.isActive;
      await updateUser({
        id: user.id,
        data: { isActive: newStatus }
      }).unwrap();
      toast.success(
        newStatus
          ? t('adminUserProfile.userActions.lockModal.toastUnlocked')
          : t('adminUserProfile.userActions.lockModal.toastLocked')
      );
      setShowLockModal(false);
    } catch (error: any) {
      const errorMessage =
        error?.data?.message ||
        t('adminUserProfile.userActions.lockModal.errorGeneric');
      setUpdateError(errorMessage);
      toast.error(t('adminUserProfile.userActions.lockModal.toastFailure'));
    }
  };

  return (
    <>
      <Card className="mb-3">
        <Card.Header className="bg-body-tertiary">
          <h5 className="mb-0">
            <FontAwesomeIcon icon="tools" className="me-2" />
            {t('adminUserProfile.userActions.quickTitle')}
          </h5>
        </Card.Header>
        <Card.Body>
          <div className="d-grid gap-2">
            <Button
              variant="primary"
              size="sm"
              onClick={() => {
                setProfileFormData({
                  email: user.email || '',
                  fullName: user.fullName || '',
                  phone: user.phone || ''
                });
                setUpdateError(null);
                setShowProfileModal(true);
              }}
            >
              <FontAwesomeIcon icon="edit" className="me-2" />
              {t('adminUserProfile.userActions.editProfile')}
            </Button>

            <Button
              variant="info"
              size="sm"
              onClick={() => {
                setSelectedRole(user.role);
                setUpdateError(null);
                setShowRoleModal(true);
              }}
            >
              <FontAwesomeIcon icon="users" className="me-2" />
              {t('adminUserProfile.userActions.changeRole')}
            </Button>

            <Dropdown>
              <Dropdown.Toggle variant="secondary" size="sm" className="w-100">
                <FontAwesomeIcon icon="cog" className="me-2" />
                {t('adminUserProfile.userActions.accountSettings')}
              </Dropdown.Toggle>
              <Dropdown.Menu className="w-100">
                <Dropdown.Item>
                  <FontAwesomeIcon icon="shield-alt" className="me-2" />
                  {t('adminUserProfile.userActions.menuSecuritySettings')}
                </Dropdown.Item>
                <Dropdown.Item>
                  <FontAwesomeIcon icon="bell" className="me-2" />
                  {t('adminUserProfile.userActions.menuNotificationPrefs')}
                </Dropdown.Item>
                <Dropdown.Item>
                  <FontAwesomeIcon icon="clock" className="me-2" />
                  {t('adminUserProfile.userActions.menuLoginHistory')}
                </Dropdown.Item>
                <Dropdown.Divider />
                <Dropdown.Item>
                  <FontAwesomeIcon icon="download" className="me-2" />
                  {t('adminUserProfile.userActions.menuExportData')}
                </Dropdown.Item>
              </Dropdown.Menu>
            </Dropdown>
          </div>
        </Card.Body>
      </Card>

      <Card className="mb-3">
        <Card.Header className="bg-body-tertiary">
          <h5 className="mb-0">
            <FontAwesomeIcon icon="exclamation-triangle" className="me-2" />
            {t('adminUserProfile.userActions.securityTitle')}
          </h5>
        </Card.Header>
        <Card.Body>
          <div className="d-grid gap-2">
            <Button
              variant={user.isActive ? 'outline-warning' : 'outline-success'}
              size="sm"
              onClick={() => {
                setUpdateError(null);
                setShowLockModal(true);
              }}
            >
              <FontAwesomeIcon
                icon={user.isActive ? 'lock' : 'unlock'}
                className="me-2"
              />
              {user.isActive
                ? t('adminUserProfile.userActions.lockAccount')
                : t('adminUserProfile.userActions.unlockAccount')}
            </Button>

            <Button variant="outline-danger" size="sm">
              <FontAwesomeIcon icon="ban" className="me-2" />
              {t('adminUserProfile.userActions.suspendUser')}
            </Button>

            <Button variant="outline-info" size="sm">
              <FontAwesomeIcon icon="sign-out-alt" className="me-2" />
              {t('adminUserProfile.userActions.forceLogout')}
            </Button>

            <Button variant="outline-secondary" size="sm">
              <FontAwesomeIcon icon="mobile" className="me-2" />
              {t('adminUserProfile.userActions.reset2fa')}
            </Button>
          </div>
        </Card.Body>
      </Card>

      <Modal show={showRoleModal} onHide={() => setShowRoleModal(false)}>
        <Modal.Header closeButton>
          <Modal.Title>
            {t('adminUserProfile.userActions.roleModal.title')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {updateError && (
            <Alert
              variant="danger"
              dismissible
              onClose={() => setUpdateError(null)}
            >
              {updateError}
            </Alert>
          )}
          <Form>
            <Form.Group className="mb-3">
              <Form.Label>
                {t('adminUserProfile.userActions.roleModal.selectLabel')}
              </Form.Label>
              {roles.map(role => (
                <Form.Check
                  key={role.value}
                  type="radio"
                  id={`role-${role.value}`}
                  name="role"
                  value={role.value}
                  checked={selectedRole === role.value}
                  onChange={e => setSelectedRole(e.target.value)}
                  label={
                    <div>
                      <strong>{role.label}</strong>
                      <div className="text-600 fs-11">{role.description}</div>
                    </div>
                  }
                  className="mb-2"
                />
              ))}
            </Form.Group>
          </Form>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="secondary"
            onClick={() => {
              setShowRoleModal(false);
              setSelectedRole(user.role);
              setUpdateError(null);
            }}
            disabled={isUpdating}
          >
            {t('adminUserProfile.userActions.roleModal.cancel')}
          </Button>
          <Button
            variant="primary"
            onClick={async () => {
              try {
                setUpdateError(null);
                await updateUser({
                  id: user.id,
                  data: { role: selectedRole }
                }).unwrap();
                toast.success(
                  t('adminUserProfile.userActions.roleModal.toastSuccess', {
                    role: t(`adminUsers.roles.${selectedRole}`, {
                      defaultValue: selectedRole
                    })
                  })
                );
                setShowRoleModal(false);
              } catch (error: any) {
                setUpdateError(
                  error?.data?.message ||
                    t('adminUserProfile.userActions.roleModal.errorUpdate')
                );
                toast.error(
                  t('adminUserProfile.userActions.roleModal.toastFailure')
                );
              }
            }}
            disabled={isUpdating || selectedRole === user.role}
          >
            {isUpdating ? (
              <>
                <Spinner size="sm" animation="border" className="me-2" />
                {t('adminUserProfile.userActions.roleModal.submitting')}
              </>
            ) : (
              t('adminUserProfile.userActions.roleModal.submit')
            )}
          </Button>
        </Modal.Footer>
      </Modal>

      <Modal show={showProfileModal} onHide={() => setShowProfileModal(false)}>
        <Modal.Header closeButton>
          <Modal.Title>
            {t('adminUserProfile.userActions.profileModal.title')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {updateError && (
            <Alert
              variant="danger"
              dismissible
              onClose={() => setUpdateError(null)}
            >
              {updateError}
            </Alert>
          )}
          <Form>
            <Form.Group className="mb-3">
              <Form.Label>
                {t('adminUserProfile.userActions.profileModal.labelEmail')}
              </Form.Label>
              <Form.Control
                type="email"
                value={profileFormData.email}
                onChange={e =>
                  setProfileFormData(prev => ({
                    ...prev,
                    email: e.target.value
                  }))
                }
                placeholder={t(
                  'adminUserProfile.userActions.profileModal.placeholderEmail'
                )}
              />
            </Form.Group>

            <Form.Group className="mb-3">
              <Form.Label>
                {t('adminUserProfile.userActions.profileModal.labelFullName')}
              </Form.Label>
              <Form.Control
                type="text"
                value={profileFormData.fullName}
                onChange={e =>
                  setProfileFormData(prev => ({
                    ...prev,
                    fullName: e.target.value
                  }))
                }
                placeholder={t(
                  'adminUserProfile.userActions.profileModal.placeholderFullName'
                )}
              />
            </Form.Group>

            <Form.Group className="mb-3">
              <Form.Label>
                {t('adminUserProfile.userActions.profileModal.labelPhone')}
              </Form.Label>
              <Form.Control
                type="tel"
                value={profileFormData.phone}
                onChange={e =>
                  setProfileFormData(prev => ({
                    ...prev,
                    phone: e.target.value
                  }))
                }
                placeholder={t(
                  'adminUserProfile.userActions.profileModal.placeholderPhone'
                )}
              />
            </Form.Group>
          </Form>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="secondary"
            onClick={() => {
              setShowProfileModal(false);
              setProfileFormData({
                email: user.email || '',
                fullName: user.fullName || '',
                phone: user.phone || ''
              });
              setUpdateError(null);
            }}
            disabled={isUpdating}
          >
            {t('adminUserProfile.userActions.profileModal.cancel')}
          </Button>
          <Button
            variant="primary"
            onClick={async () => {
              try {
                setUpdateError(null);
                const updatedFields: any = {};

                if (
                  profileFormData.email &&
                  profileFormData.email !== user.email
                ) {
                  updatedFields.email = profileFormData.email.trim();
                }
                if (
                  profileFormData.fullName &&
                  profileFormData.fullName !== user.fullName
                ) {
                  updatedFields.fullName = profileFormData.fullName.trim();
                }
                if (
                  profileFormData.phone &&
                  profileFormData.phone !== (user.phone || '')
                ) {
                  updatedFields.phone = profileFormData.phone.trim();
                }

                if (Object.keys(updatedFields).length === 0) {
                  setShowProfileModal(false);
                  return;
                }

                await updateUser({
                  id: user.id,
                  data: updatedFields
                }).unwrap();
                toast.success(
                  t('adminUserProfile.userActions.profileModal.toastSuccess')
                );
                setShowProfileModal(false);
              } catch (error: any) {
                const errorMessage =
                  error?.data?.detail ||
                  error?.data?.message ||
                  t('adminUserProfile.userActions.profileModal.errorUpdate');
                setUpdateError(errorMessage);

                if (
                  error?.status === 409 ||
                  errorMessage.includes('already in use')
                ) {
                  toast.error(
                    t(
                      'adminUserProfile.userActions.profileModal.toastEmailInUse'
                    )
                  );
                } else {
                  toast.error(
                    t('adminUserProfile.userActions.profileModal.toastFailure')
                  );
                }
              }
            }}
            disabled={isUpdating}
          >
            {isUpdating ? (
              <>
                <Spinner size="sm" animation="border" className="me-2" />
                {t('adminUserProfile.userActions.profileModal.submitting')}
              </>
            ) : (
              t('adminUserProfile.userActions.profileModal.submit')
            )}
          </Button>
        </Modal.Footer>
      </Modal>

      <Modal
        show={showLockModal}
        onHide={() => setShowLockModal(false)}
        centered
      >
        <Modal.Header closeButton>
          <Modal.Title>
            {user.isActive
              ? t('adminUserProfile.userActions.lockModal.titleLock')
              : t('adminUserProfile.userActions.lockModal.titleUnlock')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {updateError && (
            <Alert
              variant="danger"
              dismissible
              onClose={() => setUpdateError(null)}
            >
              {updateError}
            </Alert>
          )}
          <p>
            <Trans
              i18nKey={
                user.isActive
                  ? 'adminUserProfile.userActions.lockModal.confirmLock'
                  : 'adminUserProfile.userActions.lockModal.confirmUnlock'
              }
              values={{ name: user.fullName }}
              components={{ strong: <strong /> }}
            />
          </p>
          {user.isActive ? (
            <p className="text-warning mb-0">
              {t('adminUserProfile.userActions.lockModal.warningLock')}
            </p>
          ) : (
            <p className="text-success mb-0">
              {t('adminUserProfile.userActions.lockModal.warningUnlock')}
            </p>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="secondary"
            onClick={() => {
              setShowLockModal(false);
              setUpdateError(null);
            }}
            disabled={isUpdating}
          >
            {t('adminUserProfile.userActions.lockModal.cancel')}
          </Button>
          <Button
            variant={user.isActive ? 'warning' : 'success'}
            onClick={handleToggleAccountLock}
            disabled={isUpdating}
          >
            {isUpdating ? (
              <>
                <Spinner size="sm" animation="border" className="me-2" />
                {t('adminUserProfile.userActions.lockModal.submitting')}
              </>
            ) : user.isActive ? (
              t('adminUserProfile.userActions.lockModal.submitLock')
            ) : (
              t('adminUserProfile.userActions.lockModal.submitUnlock')
            )}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default AdminUserActions;

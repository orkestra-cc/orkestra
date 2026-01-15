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
import { User, useUpdateUserMutation } from 'store/api/userApi';
import { toast } from 'react-toastify';

interface AdminUserActionsProps {
  user: User;
}

const AdminUserActions: React.FC<AdminUserActionsProps> = ({ user }) => {
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

  const roles = [
    {
      value: 'guest',
      label: 'Guest',
      description: 'Limited access to own profile'
    },
    {
      value: 'operator',
      label: 'Operator',
      description: 'Limited operator access'
    },
    {
      value: 'manager',
      label: 'Manager',
      description: 'Team and task management'
    },
    {
      value: 'administrator',
      label: 'Administrator',
      description: 'System administration'
    },
    { value: 'ceo', label: 'CEO', description: 'Full system access' }
  ];

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
          ? 'Account unlocked successfully'
          : 'Account locked successfully'
      );
      setShowLockModal(false);
    } catch (error: any) {
      const errorMessage =
        error?.data?.message || "Error during operation";
      setUpdateError(errorMessage);
      toast.error("Unable to modify account status");
    }
  };

  return (
    <>
      <Card className="mb-3">
        <Card.Header className="bg-body-tertiary">
          <h5 className="mb-0">
            <FontAwesomeIcon icon="tools" className="me-2" />
            Quick Actions
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
              Edit Profile
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
              Change Role
            </Button>

            {/* <Button variant="warning" size="sm">
              <FontAwesomeIcon icon="key" className="me-2" />
              Reimposta Password
            </Button> */}

            <Dropdown>
              <Dropdown.Toggle variant="secondary" size="sm" className="w-100">
                <FontAwesomeIcon icon="cog" className="me-2" />
                Account Settings
              </Dropdown.Toggle>
              <Dropdown.Menu className="w-100">
                <Dropdown.Item>
                  <FontAwesomeIcon icon="shield-alt" className="me-2" />
                  Security Settings
                </Dropdown.Item>
                <Dropdown.Item>
                  <FontAwesomeIcon icon="bell" className="me-2" />
                  Notification Preferences
                </Dropdown.Item>
                <Dropdown.Item>
                  <FontAwesomeIcon icon="clock" className="me-2" />
                  Login History
                </Dropdown.Item>
                <Dropdown.Divider />
                <Dropdown.Item>
                  <FontAwesomeIcon icon="download" className="me-2" />
                  Export User Data
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
            Security Actions
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
              {user.isActive ? 'Lock Account' : 'Unlock Account'}
            </Button>

            <Button variant="outline-danger" size="sm">
              <FontAwesomeIcon icon="ban" className="me-2" />
              Suspend User
            </Button>

            <Button variant="outline-info" size="sm">
              <FontAwesomeIcon icon="sign-out-alt" className="me-2" />
              Force Logout
            </Button>

            <Button variant="outline-secondary" size="sm">
              <FontAwesomeIcon icon="mobile" className="me-2" />
              Reset 2FA
            </Button>
          </div>
        </Card.Body>
      </Card>

      <Modal show={showRoleModal} onHide={() => setShowRoleModal(false)}>
        <Modal.Header closeButton>
          <Modal.Title>Change User Role</Modal.Title>
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
              <Form.Label>Select New Role</Form.Label>
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
            Cancel
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
                  `Role successfully updated to ${selectedRole}`
                );
                setShowRoleModal(false);
              } catch (error: any) {
                setUpdateError(
                  error?.data?.message ||
                    "Error updating role"
                );
                toast.error('Unable to update role');
              }
            }}
            disabled={isUpdating || selectedRole === user.role}
          >
            {isUpdating ? (
              <>
                <Spinner size="sm" animation="border" className="me-2" />
                Updating...
              </>
            ) : (
              'Update Role'
            )}
          </Button>
        </Modal.Footer>
      </Modal>

      <Modal show={showProfileModal} onHide={() => setShowProfileModal(false)}>
        <Modal.Header closeButton>
          <Modal.Title>Edit User Profile</Modal.Title>
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
              <Form.Label>Email</Form.Label>
              <Form.Control
                type="email"
                value={profileFormData.email}
                onChange={e =>
                  setProfileFormData(prev => ({
                    ...prev,
                    email: e.target.value
                  }))
                }
                placeholder="Enter email"
              />
            </Form.Group>

            <Form.Group className="mb-3">
              <Form.Label>Full Name</Form.Label>
              <Form.Control
                type="text"
                value={profileFormData.fullName}
                onChange={e =>
                  setProfileFormData(prev => ({
                    ...prev,
                    fullName: e.target.value
                  }))
                }
                placeholder="Enter full name"
              />
            </Form.Group>

            <Form.Group className="mb-3">
              <Form.Label>Phone</Form.Label>
              <Form.Control
                type="tel"
                value={profileFormData.phone}
                onChange={e =>
                  setProfileFormData(prev => ({
                    ...prev,
                    phone: e.target.value
                  }))
                }
                placeholder="Enter phone number"
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
            Cancel
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
                toast.success('Profile updated successfully');
                setShowProfileModal(false);
              } catch (error: any) {
                const errorMessage =
                  error?.data?.detail ||
                  error?.data?.message ||
                  "Error updating profile";
                setUpdateError(errorMessage);

                if (
                  error?.status === 409 ||
                  errorMessage.includes('already in use')
                ) {
                  toast.error('Email already in use by another user');
                } else {
                  toast.error('Unable to update profile');
                }
              }
            }}
            disabled={isUpdating}
          >
            {isUpdating ? (
              <>
                <Spinner size="sm" animation="border" className="me-2" />
                Updating...
              </>
            ) : (
              'Update Profile'
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
            {user.isActive ? 'Lock Account' : 'Unlock Account'}
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
            Are you sure you want to {user.isActive ? 'lock' : 'unlock'}{' '}
            the account of <strong>{user.fullName}</strong>?
          </p>
          {user.isActive ? (
            <p className="text-warning mb-0">
              The user will no longer be able to access the system until the account
              is unlocked.
            </p>
          ) : (
            <p className="text-success mb-0">
              The user will be able to access the system again.
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
            Cancel
          </Button>
          <Button
            variant={user.isActive ? 'warning' : 'success'}
            onClick={handleToggleAccountLock}
            disabled={isUpdating}
          >
            {isUpdating ? (
              <>
                <Spinner size="sm" animation="border" className="me-2" />
                Please wait...
              </>
            ) : user.isActive ? (
              'Lock'
            ) : (
              'Unlock'
            )}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default AdminUserActions;

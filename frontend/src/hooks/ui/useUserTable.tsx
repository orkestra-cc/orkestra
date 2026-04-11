import React, { useState } from 'react';
import { Link } from 'react-router';
import paths from 'routes/paths';
import useAdvanceTable from './useAdvanceTable';
import Avatar from 'components/common/Avatar';
import Flex from 'components/common/Flex';
import SubtleBadge from 'components/common/SubtleBadge';
import { Badge, Dropdown, Modal, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faGoogle,
  faApple,
  faGithub,
  faDiscord
} from '@fortawesome/free-brands-svg-icons';
import {
  useGetUsersQuery,
  useUpdateUserMutation,
  User
} from 'store/api/userApi';
import FalconCloseButton from 'components/common/FalconCloseButton';

// Confirmation Modal Component
interface UserActivationModalProps {
  show: boolean;
  onHide: () => void;
  user: User | null;
  onConfirm: () => void;
  isLoading: boolean;
}

const UserActivationModal: React.FC<UserActivationModalProps> = ({
  show,
  onHide,
  user,
  onConfirm,
  isLoading
}) => {
  if (!user) return null;

  const isActivating = !user.isActive;

  return (
    <Modal show={show} onHide={onHide} centered>
      <Modal.Header>
        <Modal.Title>
          {isActivating ? 'Activate User' : 'Deactivate User'}
        </Modal.Title>
        <FalconCloseButton onClick={onHide} />
      </Modal.Header>
      <Modal.Body>
        <p>
          Are you sure you want to {isActivating ? 'activate' : 'deactivate'}{' '}
          the user <strong>{user.fullName}</strong>?
        </p>
        {!isActivating && (
          <p className="text-warning mb-0">
            The user will no longer be able to access the system until they are
            reactivated.
          </p>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isLoading}>
          Cancel
        </Button>
        <Button
          variant={isActivating ? 'success' : 'warning'}
          onClick={onConfirm}
          disabled={isLoading}
        >
          {isLoading ? 'Please wait...' : isActivating ? 'Activate' : 'Deactivate'}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

const useUserTable = (options?: any) => {
  const [showModal, setShowModal] = useState(false);
  const [selectedUser, setSelectedUser] = useState<User | null>(null);
  const [updateUser, { isLoading: isUpdating }] = useUpdateUserMutation();

  // Fetch users from backend API
  const {
    data: usersResponse,
    isLoading,
    error
  } = useGetUsersQuery({
    pageSize: options?.perPage || 10,
    page: 1
  });

  // Transform the data for the table
  const users = usersResponse?.users || [];

  // Handle activation/deactivation
  const handleToggleActivation = (user: User) => {
    setSelectedUser(user);
    setShowModal(true);
  };

  const handleConfirmToggle = async () => {
    if (!selectedUser) return;

    try {
      await updateUser({
        id: selectedUser.id,
        data: { isActive: !selectedUser.isActive }
      }).unwrap();
      setShowModal(false);
      setSelectedUser(null);
    } catch (error) {
      console.error('Failed to update user:', error);
    }
  };

  const handleCloseModal = () => {
    setShowModal(false);
    setSelectedUser(null);
  };

  // OAuth provider icon mapper with colors
  const providerConfig = {
    google: { icon: faGoogle, color: '#EA4335' },
    apple: { icon: faApple, color: '#FFFFFF' },
    github: { icon: faGithub, color: '#181717' },
    discord: { icon: faDiscord, color: '#5865F2' }
  };

  const columns = [
    {
      accessorKey: 'fullName',
      header: 'User',
      meta: {
        headerProps: { className: 'ps-2 text-900', style: { height: '46px' } },
        cellProps: {
          className: 'py-2 white-space-nowrap pe-3 pe-xxl-4 ps-2'
        }
      },
      cell: ({ row: { original } }: { row: { original: User } }) => {
        const { fullName, email, avatar } = original;
        return (
          <Flex alignItems="center" className="position-relative py-1">
            {avatar ? (
              <Avatar src={avatar} size="xl" className="me-2" />
            ) : (
              <Avatar size="xl" name={fullName} className="me-2" />
            )}
            <div>
              <h6 className="mb-0">
                <Link
                  to={paths.adminUserProfile.replace(':userId', original.id)}
                  className="stretched-link text-900"
                >
                  {fullName}
                </Link>
              </h6>
              <small className="text-muted">{email}</small>
            </div>
          </Flex>
        );
      }
    },
    {
      accessorKey: 'role',
      header: 'Role',
      meta: {
        headerProps: {
          className: 'text-900'
        },
        cellProps: {
          className: 'py-2 pe-4'
        }
      },
      cell: ({ row: { original } }: { row: { original: User } }) => {
        const { role } = original as { role: RoleType };
        type RoleType =
          | 'super_admin'
          | 'administrator'
          | 'developer'
          | 'manager'
          | 'operator'
          | 'guest';
        const roleColors: Record<RoleType, string> = {
          super_admin: 'danger',
          administrator: 'warning',
          developer: 'primary',
          manager: 'info',
          operator: 'success',
          guest: 'secondary'
        };
        const roleLabels: Record<RoleType, string> = {
          super_admin: 'Super Admin',
          administrator: 'Administrator',
          developer: 'Developer',
          manager: 'Manager',
          operator: 'Operator',
          guest: 'Guest'
        };
        return (
          <Badge bg={roleColors[role] || 'secondary'}>
            {roleLabels[role] || role}
          </Badge>
        );
      }
    },
    {
      accessorKey: 'providers',
      header: 'Login With',
      meta: {
        headerProps: {
          className: 'text-900'
        },
        cellProps: {
          className: 'py-2 pe-4'
        }
      },
      cell: ({ row: { original } }: { row: { original: User } }) => {
        const { providers } = original;
        if (!providers || providers.length === 0) {
          return <span className="text-muted">-</span>;
        }
        return (
          <div className="d-flex gap-2 align-items-center">
            {providers.map((provider, index) => {
              const config =
                providerConfig[
                  provider.provider as keyof typeof providerConfig
                ];
              return config ? (
                <FontAwesomeIcon
                  key={index}
                  icon={config.icon}
                  style={{ color: config.color, fontSize: '1.25rem' }}
                  title={`${provider.provider.charAt(0).toUpperCase() + provider.provider.slice(1)} (${provider.email})`}
                />
              ) : null;
            })}
          </div>
        );
      }
    },
    {
      accessorKey: 'isActive',
      header: 'Status',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: {
          className: 'fs-9 pe-4'
        }
      },
      cell: ({ row: { original } }: { row: { original: User } }) => {
        const { isActive } = original;
        return (
          <SubtleBadge bg={isActive ? 'success' : 'secondary'} className="me-2">
            {isActive ? 'Active' : 'Inactive'}
          </SubtleBadge>
        );
      }
    },
    {
      accessorKey: 'lastLogin',
      header: 'Last Login',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: {
          className: 'pe-4'
        }
      },
      cell: ({ row: { original } }: { row: { original: User } }) => {
        const { lastLogin } = original;

        if (!lastLogin) {
          return <div className="text-muted">Never</div>;
        }

        const date = new Date(lastLogin);

        if (isNaN(date.getTime())) {
          return <div className="text-muted">Never</div>;
        }

        const formattedDate = date.toLocaleDateString('en-GB', {
          year: 'numeric',
          month: 'short',
          day: 'numeric'
        });
        const formattedTime = date.toLocaleTimeString('en-GB', {
          hour: '2-digit',
          minute: '2-digit'
        });
        return (
          <div>
            <div className="text-900">{formattedDate}</div>
            <small className="text-muted">{formattedTime}</small>
          </div>
        );
      }
    },
    {
      accessorKey: 'createdAt',
      header: 'Created On',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: {
          className: 'pe-4'
        }
      },
      cell: ({ row: { original } }: { row: { original: User } }) => {
        const { createdAt } = original;
        const date = new Date(createdAt);
        return date.toLocaleDateString('en-GB', {
          year: 'numeric',
          month: 'short',
          day: 'numeric'
        });
      }
    },
    {
      accessorKey: 'actions',
      header: 'Actions',
      meta: {
        headerProps: { className: 'text-end text-900' }
      },
      cell: ({ row: { original } }: { row: { original: User } }) => {
        return (
          <Dropdown align="end" className="btn-reveal-trigger">
            <Dropdown.Toggle
              variant="link"
              size="sm"
              className="text-600 btn-reveal"
            >
              <FontAwesomeIcon icon="ellipsis-h" className="fs-11" />
            </Dropdown.Toggle>

            <Dropdown.Menu className="border py-0">
              <div className="py-2">
                <Dropdown.Item
                  as={Link}
                  to={paths.adminUserProfile.replace(':userId', original.id)}
                >
                  View Details
                </Dropdown.Item>
                {/* <Dropdown.Item>Edit User</Dropdown.Item> */}
                <Dropdown.Divider />
                <Dropdown.Item
                  className="text-warning"
                  onClick={() => handleToggleActivation(original)}
                >
                  {original.isActive ? 'Deactivate' : 'Activate'}
                </Dropdown.Item>
                <Dropdown.Item className="text-danger">
                  Delete User
                </Dropdown.Item>
              </div>
            </Dropdown.Menu>
          </Dropdown>
        );
      }
    }
  ];

  const table = useAdvanceTable({
    columns,
    data: users,
    isLoading,
    error,
    ...options
  });

  return {
    ...table,
    ActivationModal: () => (
      <UserActivationModal
        show={showModal}
        onHide={handleCloseModal}
        user={selectedUser}
        onConfirm={handleConfirmToggle}
        isLoading={isUpdating}
      />
    )
  };
};

export default useUserTable;

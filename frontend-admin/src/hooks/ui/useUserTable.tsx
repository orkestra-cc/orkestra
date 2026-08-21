import React, { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { toast } from 'react-toastify';
import { useTranslation } from 'react-i18next';
import type { OnChangeFn, PaginationState } from '@tanstack/react-table';
import paths from 'routes/paths';
import useAdvanceTable from './useAdvanceTable';
import useDebounce from 'hooks/useDebounce';
import Avatar from 'components/common/Avatar';
import Flex from 'components/common/Flex';
import SubtleBadge, { BadgeColor } from 'components/common/SubtleBadge';
import AdminResetMfaModal from 'pages/admin/users/AdminResetMfaModal';
import DeleteUserModal from 'pages/admin/users/DeleteUserModal';
import {
  Dropdown,
  Modal,
  Button,
  OverlayTrigger,
  Tooltip
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faGoogle,
  faApple,
  faGithub,
  faDiscord
} from '@fortawesome/free-brands-svg-icons';
import {
  useGetUsersQuery,
  useResendVerificationUserAdminMutation,
  useSendPasswordResetUserAdminMutation,
  useUpdateUserMutation,
  User
} from 'store/api/userApi';
import OrkestraCloseButton from 'components/common/OrkestraCloseButton';
import { formatDate, formatTime } from 'helpers/dateFormat';
import { useAppSelector } from 'store/hooks';
import { selectUser } from 'store/slices/authSlice';

// extractToastError prefers the typed `code` returned by errcode-bearing
// handlers (translated via the `errors.<code>` namespace), falling back
// to the human-readable `detail` and finally a generic label.
function extractToastError(
  err: unknown,
  t: (key: string) => string,
  fallback: string
): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { code?: string; detail?: string } }).data;
    if (data?.code) {
      const translated = t(`errors.${data.code}`);
      if (translated && translated !== `errors.${data.code}`) {
        return translated;
      }
    }
    if (data?.detail) {
      return data.detail;
    }
  }
  return fallback;
}

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
        <OrkestraCloseButton onClick={onHide} />
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
          {isLoading
            ? 'Please wait...'
            : isActivating
              ? 'Activate'
              : 'Deactivate'}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

const useUserTable = (options?: any) => {
  const { t } = useTranslation();
  const currentUser = useAppSelector(selectUser);

  const [showModal, setShowModal] = useState(false);
  const [selectedUser, setSelectedUser] = useState<User | null>(null);
  const [mfaResetUser, setMfaResetUser] = useState<User | null>(null);
  const [deleteUser, setDeleteUser] = useState<User | null>(null);

  const [updateUser, { isLoading: isUpdating }] = useUpdateUserMutation();
  const [resendVerification] = useResendVerificationUserAdminMutation();
  const [sendPasswordReset] = useSendPasswordResetUserAdminMutation();

  // Server-side pagination + search state. The backend owns paging and
  // search (page/pageSize/search → total/totalPages), so the table holds
  // only the current page and reports the grand total via rowCount.
  const [pageIndex, setPageIndex] = useState(0);
  const [pageSize, setPageSize] = useState<number>(options?.perPage || 10);
  const [globalFilter, setGlobalFilter] = useState('');
  const debouncedSearch = useDebounce(globalFilter.trim(), 300);

  // A new search term always returns to the first page.
  useEffect(() => {
    setPageIndex(0);
  }, [debouncedSearch]);

  // Fetch the current page from the backend API
  const {
    data: usersResponse,
    isLoading,
    error
  } = useGetUsersQuery({
    page: pageIndex + 1,
    pageSize,
    search: debouncedSearch || undefined
  });

  // Transform the data for the table
  const users = usersResponse?.users || [];
  const total = usersResponse?.total ?? 0;

  const pagination: PaginationState = { pageIndex, pageSize };
  const onPaginationChange: OnChangeFn<PaginationState> = updater => {
    const next = typeof updater === 'function' ? updater(pagination) : updater;
    if (next.pageSize !== pageSize) {
      setPageSize(next.pageSize);
      setPageIndex(0);
    } else {
      setPageIndex(next.pageIndex);
    }
  };

  const displayName = (user: User) => user.fullName || user.email;
  const genericFailure = t('adminUsers.mfaReset.errors.generic');

  // Handle activation/deactivation
  const handleToggleActivation = (user: User) => {
    setSelectedUser(user);
    setShowModal(true);
  };

  const handleConfirmToggle = async () => {
    if (!selectedUser) return;
    const wasActive = selectedUser.isActive;
    try {
      await updateUser({
        id: selectedUser.id,
        data: { isActive: !wasActive }
      }).unwrap();
      toast.success(
        t(
          wasActive
            ? 'adminUsers.rowActions.toastDeactivated'
            : 'adminUsers.rowActions.toastActivated',
          { user: displayName(selectedUser) }
        )
      );
      setShowModal(false);
      setSelectedUser(null);
    } catch (err) {
      toast.error(
        t('adminUsers.rowActions.toastActivationFailed', {
          user: displayName(selectedUser),
          error: extractToastError(err, t, genericFailure)
        })
      );
    }
  };

  const handleCloseModal = () => {
    setShowModal(false);
    setSelectedUser(null);
  };

  const handleResendVerification = async (user: User) => {
    try {
      await resendVerification(user.id).unwrap();
      toast.success(
        t('adminUsers.rowActions.toastVerificationSent', {
          user: displayName(user)
        })
      );
    } catch (err) {
      toast.error(
        t('adminUsers.rowActions.toastVerificationFailed', {
          error: extractToastError(err, t, genericFailure)
        })
      );
    }
  };

  const handleSendPasswordReset = async (user: User) => {
    try {
      await sendPasswordReset(user.id).unwrap();
      toast.success(
        t('adminUsers.rowActions.toastPasswordResetSent', {
          user: displayName(user)
        })
      );
    } catch (err) {
      toast.error(
        t('adminUsers.rowActions.toastPasswordResetFailed', {
          error: extractToastError(err, t, genericFailure)
        })
      );
    }
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
      header: t('adminUsers.columns.user'),
      meta: {
        headerProps: { className: 'ps-2 text-900', style: { height: '46px' } },
        cellProps: {
          className: 'py-2 white-space-nowrap pe-3 pe-xxl-4 ps-2'
        }
      },
      cell: ({ row: { original } }: { row: { original: User } }) => {
        const { fullName, email, avatar, kind } = original;
        return (
          <Flex alignItems="center" className="position-relative py-1">
            {avatar ? (
              <Avatar src={avatar} size="xl" className="me-2" />
            ) : (
              <Avatar size="xl" name={fullName} className="me-2" />
            )}
            <div>
              <div className="d-flex align-items-center gap-2 mb-1">
                <h6 className="mb-0">
                  <Link
                    to={paths.adminUserProfile.replace(':userId', original.id)}
                    className="stretched-link text-900"
                  >
                    {fullName}
                  </Link>
                </h6>
                {kind === 'service' && (
                  <SubtleBadge bg="secondary">
                    {t('adminUsers.serviceBadge')}
                  </SubtleBadge>
                )}
              </div>
              <small className="text-muted">{email}</small>
            </div>
          </Flex>
        );
      }
    },
    {
      accessorKey: 'role',
      header: t('adminUsers.columns.role'),
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
        const roleColors: Record<RoleType, BadgeColor> = {
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
          <SubtleBadge bg={roleColors[role] || 'secondary'}>
            {roleLabels[role] || role}
          </SubtleBadge>
        );
      }
    },
    {
      accessorKey: 'providers',
      header: t('adminUsers.columns.loginWith'),
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
                  title={`${
                    provider.provider.charAt(0).toUpperCase() +
                    provider.provider.slice(1)
                  } (${provider.email})`}
                />
              ) : null;
            })}
          </div>
        );
      }
    },
    {
      accessorKey: 'isActive',
      header: t('adminUsers.columns.status'),
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
            {isActive
              ? t('adminUsers.status.active')
              : t('adminUsers.status.inactive')}
          </SubtleBadge>
        );
      }
    },
    {
      accessorKey: 'lastLogin',
      header: t('adminUsers.columns.lastLogin'),
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: {
          className: 'pe-4'
        }
      },
      cell: ({ row: { original } }: { row: { original: User } }) => {
        const { lastLogin } = original;
        const formattedDate = formatDate(lastLogin);

        if (formattedDate === '—') {
          return (
            <div className="text-muted">{t('adminUsers.columns.never')}</div>
          );
        }

        return (
          <div>
            <div className="text-900">{formattedDate}</div>
            <small className="text-muted">{formatTime(lastLogin)}</small>
          </div>
        );
      }
    },
    {
      accessorKey: 'createdAt',
      header: t('adminUsers.columns.createdOn'),
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: {
          className: 'pe-4'
        }
      },
      cell: ({ row: { original } }: { row: { original: User } }) =>
        formatDate(original.createdAt)
    },
    {
      accessorKey: 'actions',
      header: t('adminUsers.columns.actions'),
      meta: {
        headerProps: { className: 'text-end text-900' }
      },
      cell: ({ row: { original } }: { row: { original: User } }) => {
        // Self-row gate: deactivate + delete are refused server-side and
        // we hint that here so the admin doesn't get a surprise toast.
        // Role demotion is not exposed from this row dropdown today
        // (it lives on the user profile page), so it doesn't need a
        // sibling guard here.
        const isSelf = currentUser?.id === original.id;
        const selfTooltip = (
          <Tooltip>{t('adminUsers.rowActions.selfActionTooltip')}</Tooltip>
        );

        const renderSelfGuarded = (
          item: React.ReactElement,
          key: string
        ): React.ReactElement => {
          if (!isSelf) return item;
          // OverlayTrigger needs a non-disabled child to register the
          // hover, so wrap the disabled item in a span. The Dropdown.Item
          // already has its onClick removed via the disabled prop.
          return (
            <OverlayTrigger key={key} placement="left" overlay={selfTooltip}>
              <span className="d-block">{item}</span>
            </OverlayTrigger>
          );
        };

        return (
          <Dropdown align="end" className="btn-reveal-trigger">
            <Dropdown.Toggle
              variant="link"
              size="sm"
              className="text-600 btn-reveal"
              aria-label={t('adminUsers.rowActions.menuLabel', {
                name: original.fullName
              })}
            >
              <FontAwesomeIcon icon="ellipsis-h" className="fs-11" />
            </Dropdown.Toggle>

            <Dropdown.Menu className="border py-0">
              <div className="py-2">
                <Dropdown.Item
                  as={Link}
                  to={paths.adminUserProfile.replace(':userId', original.id)}
                >
                  {t('adminUsers.rowActions.viewDetails')}
                </Dropdown.Item>
                <Dropdown.Divider />
                {renderSelfGuarded(
                  <Dropdown.Item
                    className="text-warning"
                    onClick={() =>
                      isSelf ? undefined : handleToggleActivation(original)
                    }
                    disabled={isSelf}
                  >
                    {original.isActive
                      ? t('adminUsers.rowActions.deactivate')
                      : t('adminUsers.rowActions.activate')}
                  </Dropdown.Item>,
                  'toggle'
                )}
                <Dropdown.Item onClick={() => setMfaResetUser(original)}>
                  {t('adminUsers.rowActions.resetMfa')}
                </Dropdown.Item>
                {!original.emailVerified && (
                  <Dropdown.Item
                    onClick={() => handleResendVerification(original)}
                  >
                    {t('adminUsers.rowActions.resendVerification')}
                  </Dropdown.Item>
                )}
                <Dropdown.Item
                  onClick={() => handleSendPasswordReset(original)}
                >
                  {t('adminUsers.rowActions.sendPasswordReset')}
                </Dropdown.Item>
                <Dropdown.Divider />
                {renderSelfGuarded(
                  <Dropdown.Item
                    className="text-danger"
                    onClick={() =>
                      isSelf ? undefined : setDeleteUser(original)
                    }
                    disabled={isSelf}
                  >
                    {t('adminUsers.rowActions.deleteUser')}
                  </Dropdown.Item>,
                  'delete'
                )}
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
    ...options,
    // Server-side paging + search. rowCount is the backend grand total so
    // the footer's page count and "x of N" are correct; manualFiltering
    // stops the client re-filtering the already-searched page, and the
    // search box's input is captured via onGlobalFilterChange.
    manualPagination: true,
    rowCount: total,
    manualFiltering: true,
    state: { pagination, globalFilter },
    onPaginationChange,
    onGlobalFilterChange: setGlobalFilter
  });

  return {
    ...table,
    ActivationModal: () => (
      <>
        <UserActivationModal
          show={showModal}
          onHide={handleCloseModal}
          user={selectedUser}
          onConfirm={handleConfirmToggle}
          isLoading={isUpdating}
        />
        <AdminResetMfaModal
          show={mfaResetUser !== null}
          user={mfaResetUser}
          onHide={() => setMfaResetUser(null)}
        />
        <DeleteUserModal
          show={deleteUser !== null}
          user={deleteUser}
          onHide={() => setDeleteUser(null)}
        />
      </>
    )
  };
};

export default useUserTable;

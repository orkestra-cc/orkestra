import { useMemo, useState } from 'react';
import {
  Alert,
  Breadcrumb,
  Button,
  Card,
  Form,
  Modal,
  OverlayTrigger,
  Spinner,
  Tooltip
} from 'react-bootstrap';
import { Link, Navigate, useParams } from 'react-router';
import paths from 'routes/paths';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { ColumnDef } from '@tanstack/react-table';
import classNames from 'classnames';
import SubtleBadge from 'components/common/SubtleBadge';
import IconButton from 'components/common/IconButton';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import { formatDate, formatDateTime } from 'helpers/dateFormat';
import {
  useGetServiceAccountQuery,
  useUpdateServiceAccountMutation,
  useRevokeCredentialMutation
} from 'store/api/serviceAccountApi';
import type { ServiceAccountCredential } from 'types/serviceAccounts';
import IssueCredentialModal from './IssueCredentialModal';

const renameSchema = yup.object({
  name: yup.string().trim().required().max(100)
});
type RenameFormData = yup.InferType<typeof renameSchema>;

// Mirrors CreateServiceAccountModal's extractError (Task 4): the mutations
// on this page don't carry a specific 409/422 meaning of their own, so this
// stays generic — code translation first, then the raw detail, then a
// catch-all. IssueCredentialModal keeps its own copy with the 409→cap
// mapping, since that one status code IS meaningful there.
function extractError(err: unknown, t: (key: string) => string): string {
  const anyErr = err as {
    status?: number;
    data?: { code?: string; detail?: string };
  };
  if (anyErr?.data?.code) {
    const translated = t(`errors.${anyErr.data.code}`);
    if (translated && translated !== `errors.${anyErr.data.code}`) {
      return translated;
    }
  }
  if (anyErr?.data?.detail) {
    return anyErr.data.detail;
  }
  return t('adminServiceAccounts.errors.generic');
}

// ServiceAccountDetailPage — Task 5. Account summary (status, active
// switch, rename) + a credentials table (issue, revoke). No step-up code:
// the baseApi interceptor + global StepUpModal already handle any mutation
// that needs it.
const ServiceAccountDetailPage: React.FC = () => {
  const { t } = useTranslation();
  const { id } = useParams<{ id: string }>();
  const { data, isLoading, error } = useGetServiceAccountQuery(id ?? '', {
    skip: !id
  });
  // Shared by both the active switch and the rename form: either mutation
  // in flight disables the switch, closing the double-fire window where two
  // rapid clicks before invalidation resolves would otherwise send two
  // identical PATCHes (+ two toasts) — same gap the RolesTable precedent
  // has, fixed here per review.
  const [updateServiceAccount, { isLoading: isUpdating }] =
    useUpdateServiceAccountMutation();
  const [revokeCredential, { isLoading: revoking }] =
    useRevokeCredentialMutation();

  const [renaming, setRenaming] = useState(false);
  const [showIssue, setShowIssue] = useState(false);
  const [revokeTarget, setRevokeTarget] =
    useState<ServiceAccountCredential | null>(null);

  const {
    register: registerRename,
    handleSubmit: handleRenameSubmit,
    reset: resetRename,
    formState: { errors: renameErrors }
  } = useForm<RenameFormData>({ resolver: yupResolver(renameSchema) });

  const startRename = () => {
    if (!data) return;
    resetRename({ name: data.name });
    setRenaming(true);
  };

  const onRenameSubmit = handleRenameSubmit(async formData => {
    if (!data) return;
    try {
      const result = await updateServiceAccount({
        id: data.id,
        name: formData.name
      }).unwrap();
      toast.success(
        t('adminServiceAccounts.detail.renameSuccessToast', {
          name: result.name
        })
      );
      setRenaming(false);
    } catch (err) {
      toast.error(
        t('adminServiceAccounts.detail.renameErrorToast', {
          error: extractError(err, t)
        })
      );
    }
  });

  const handleToggleActive = async () => {
    if (!data) return;
    const nextActive = !data.isActive;
    try {
      await updateServiceAccount({ id: data.id, active: nextActive }).unwrap();
      toast.success(
        nextActive
          ? t('adminServiceAccounts.detail.toggleEnabledToast', {
              name: data.name
            })
          : t('adminServiceAccounts.detail.toggleDisabledToast', {
              name: data.name
            })
      );
    } catch (err) {
      toast.error(
        t('adminServiceAccounts.detail.toggleErrorToast', {
          error: extractError(err, t)
        })
      );
    }
  };

  const onConfirmRevoke = async () => {
    if (!data || !revokeTarget) return;
    try {
      await revokeCredential({
        id: data.id,
        credentialId: revokeTarget.id
      }).unwrap();
      toast.success(t('adminServiceAccounts.detail.revokeSuccessToast'));
      setRevokeTarget(null);
    } catch (err) {
      toast.error(
        t('adminServiceAccounts.detail.revokeErrorToast', {
          error: extractError(err, t)
        })
      );
    }
  };

  const columns = useMemo<ColumnDef<ServiceAccountCredential>[]>(
    () => [
      {
        accessorKey: 'clientId',
        header: t('adminServiceAccounts.detail.columns.clientId'),
        cell: ({ row: { original } }) => (
          <span
            className={classNames('font-monospace', {
              'text-decoration-line-through text-muted': !!original.revokedAt
            })}
          >
            {original.clientId}
          </span>
        )
      },
      {
        accessorKey: 'label',
        header: t('adminServiceAccounts.detail.columns.label'),
        cell: ({ row: { original } }) => (
          <span
            className={
              original.revokedAt
                ? 'text-decoration-line-through text-muted'
                : ''
            }
          >
            {original.label || '—'}
          </span>
        )
      },
      {
        accessorKey: 'createdAt',
        header: t('adminServiceAccounts.detail.columns.createdAt'),
        cell: ({ row: { original } }) => formatDate(original.createdAt)
      },
      {
        accessorKey: 'lastUsedAt',
        header: t('adminServiceAccounts.detail.columns.lastUsedAt'),
        cell: ({ row: { original } }) => formatDateTime(original.lastUsedAt)
      },
      {
        id: 'status',
        header: t('adminServiceAccounts.detail.columns.status'),
        cell: ({ row: { original } }) => (
          <SubtleBadge bg={original.revokedAt ? 'secondary' : 'success'}>
            {original.revokedAt
              ? t('adminServiceAccounts.detail.credentialStatus.revoked')
              : t('adminServiceAccounts.detail.credentialStatus.active')}
          </SubtleBadge>
        )
      },
      {
        id: 'actions',
        header: '',
        enableSorting: false,
        meta: { headerProps: { style: { width: 60 } } },
        cell: ({ row: { original } }) => (
          <OverlayTrigger
            placement="top"
            overlay={
              <Tooltip>
                {t('adminServiceAccounts.detail.tooltipRevoke')}
              </Tooltip>
            }
          >
            <span className="d-inline-block">
              <Button
                variant="outline-danger"
                size="sm"
                disabled={!!original.revokedAt}
                onClick={() => setRevokeTarget(original)}
                aria-label={t('adminServiceAccounts.detail.ariaRevoke')}
              >
                <FontAwesomeIcon icon="ban" />
              </Button>
            </span>
          </OverlayTrigger>
        )
      }
    ],
    [t]
  );

  const table = useAdvanceTable({
    data: data?.credentials ?? [],
    columns,
    sortable: false,
    pagination: false
  });

  if (!id) {
    return <Navigate to="/admin/service-accounts" replace />;
  }

  if (isLoading) {
    return (
      <div className="text-center py-5">
        <Spinner animation="border" size="sm" />
      </div>
    );
  }

  if (error || !data) {
    return (
      <Alert variant="danger">
        {t('adminServiceAccounts.detail.notFound')}{' '}
        <Link to="/admin/service-accounts">
          {t('adminServiceAccounts.detail.backToList')}
        </Link>
      </Alert>
    );
  }

  return (
    <>
      <Breadcrumb className="mb-3 fs-10">
        <Breadcrumb.Item
          linkAs={Link}
          linkProps={{ to: '/admin/service-accounts' }}
        >
          {t('adminServiceAccounts.detail.breadcrumbList')}
        </Breadcrumb.Item>
        <Breadcrumb.Item active>{data.name}</Breadcrumb.Item>
      </Breadcrumb>

      <Card className="mb-3 shadow-none border">
        <Card.Body className="d-flex justify-content-between align-items-start flex-wrap gap-3">
          <div>
            <h3 className="fw-normal mb-1">{data.name}</h3>
            <div className="d-flex align-items-center gap-2 flex-wrap fs-10 text-muted">
              <span>{data.email}</span>
              <SubtleBadge bg={data.isActive ? 'success' : 'secondary'} pill>
                {data.isActive
                  ? t('adminServiceAccounts.status.active')
                  : t('adminServiceAccounts.status.inactive')}
              </SubtleBadge>
              {/* AccountView.ID is the same UUID as the underlying user
                  record (verified server-side) — this links straight to
                  where the account's tenant membership and role bindings
                  are actually managed, per spec §6. */}
              <Link
                to={paths.adminUserProfile.replace(':userId', data.id)}
                className="text-decoration-none"
              >
                <FontAwesomeIcon icon="user-shield" className="me-1" />
                {t('adminServiceAccounts.detail.manageAccessLink')}
              </Link>
            </div>
          </div>
          <div className="d-flex align-items-center gap-3">
            <OverlayTrigger
              placement="top"
              overlay={
                <Tooltip>
                  {data.isActive
                    ? t('adminServiceAccounts.detail.tooltipDisable')
                    : t('adminServiceAccounts.detail.tooltipEnable')}
                </Tooltip>
              }
            >
              <Form.Check
                type="switch"
                id="sa-active-switch"
                className="m-0"
                checked={data.isActive}
                disabled={isUpdating}
                onChange={handleToggleActive}
                aria-label={
                  data.isActive
                    ? t('adminServiceAccounts.detail.ariaDisable')
                    : t('adminServiceAccounts.detail.ariaEnable')
                }
              />
            </OverlayTrigger>
            {!renaming && (
              <IconButton
                variant="outline-secondary"
                size="sm"
                icon="pencil-alt"
                onClick={startRename}
              >
                {t('adminServiceAccounts.detail.renameButton')}
              </IconButton>
            )}
          </div>
        </Card.Body>

        {renaming && (
          <Card.Body className="border-top pt-3">
            <Form
              onSubmit={onRenameSubmit}
              noValidate
              className="d-flex align-items-start gap-2 flex-wrap"
            >
              <Form.Group controlId="sa-rename-name" style={{ minWidth: 240 }}>
                <Form.Label className="fs-10 text-muted mb-1">
                  {t('adminServiceAccounts.createModal.nameLabel')}
                </Form.Label>
                <Form.Control
                  size="sm"
                  autoFocus
                  isInvalid={!!renameErrors.name}
                  {...registerRename('name')}
                />
                <Form.Control.Feedback type="invalid">
                  {renameErrors.name?.type === 'max'
                    ? t('adminServiceAccounts.createModal.nameTooLong')
                    : t('adminServiceAccounts.createModal.nameRequired')}
                </Form.Control.Feedback>
              </Form.Group>
              <div className="d-flex gap-2 mt-4">
                <Button
                  size="sm"
                  variant="primary"
                  type="submit"
                  disabled={isUpdating}
                >
                  {isUpdating ? (
                    <>
                      <Spinner size="sm" animation="border" className="me-2" />
                      {t('adminServiceAccounts.detail.renameSaving')}
                    </>
                  ) : (
                    t('adminServiceAccounts.detail.renameSave')
                  )}
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => setRenaming(false)}
                  disabled={isUpdating}
                >
                  {t('adminServiceAccounts.detail.renameCancel')}
                </Button>
              </div>
            </Form>
          </Card.Body>
        )}
      </Card>

      <Card className="shadow-none border">
        <Card.Header className="bg-body-tertiary py-2 px-3 d-flex justify-content-between align-items-center flex-wrap gap-2">
          <div>
            <div className="fw-semibold">
              {t('adminServiceAccounts.detail.credentialsTitle')}
            </div>
            <div className="text-muted small">
              {t('adminServiceAccounts.detail.credentialsDescription')}
            </div>
          </div>
          <IconButton
            variant="orkestra-default"
            size="sm"
            icon="plus"
            transform="shrink-3"
            iconAlign="middle"
            onClick={() => setShowIssue(true)}
          >
            {t('adminServiceAccounts.detail.issueButton')}
          </IconButton>
        </Card.Header>
        {data.credentials.length === 0 ? (
          <Card.Body className="text-muted text-center py-4">
            {t('adminServiceAccounts.detail.emptyCredentials')}
          </Card.Body>
        ) : (
          <AdvanceTableProvider {...table}>
            <Card.Body className="p-0">
              <AdvanceTable
                headerClassName="text-nowrap align-middle"
                rowClassName="align-middle"
                tableProps={{
                  size: 'sm',
                  className: 'fs-10 mb-0 overflow-hidden'
                }}
              />
            </Card.Body>
          </AdvanceTableProvider>
        )}
      </Card>

      <IssueCredentialModal
        show={showIssue}
        onHide={() => setShowIssue(false)}
        accountId={data.id}
      />

      <Modal
        show={!!revokeTarget}
        onHide={() => setRevokeTarget(null)}
        backdrop="static"
        centered
      >
        <Modal.Header closeButton>
          <Modal.Title>
            {t('adminServiceAccounts.detail.revokeModal.title')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <Alert variant="warning" className="fs-10 mb-0">
            {t('adminServiceAccounts.detail.revokeModal.body')}
          </Alert>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="secondary"
            onClick={() => setRevokeTarget(null)}
            disabled={revoking}
          >
            {t('adminServiceAccounts.detail.revokeModal.cancel')}
          </Button>
          <Button
            variant="danger"
            onClick={onConfirmRevoke}
            disabled={revoking}
          >
            {revoking ? (
              <>
                <Spinner size="sm" animation="border" className="me-2" />
                {t('adminServiceAccounts.detail.revokeModal.confirming')}
              </>
            ) : (
              t('adminServiceAccounts.detail.revokeModal.confirm')
            )}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default ServiceAccountDetailPage;

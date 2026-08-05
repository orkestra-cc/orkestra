import { useCallback, useMemo, useState } from 'react';
import { Button, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import { ColumnDef } from '@tanstack/react-table';
import SubtleBadge from 'components/common/SubtleBadge';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import {
  useListBindingsQuery,
  useDeleteBindingMutation,
  type Binding
} from 'store/api/tenantApi';
import CreateBindingModal from './CreateBindingModal';

interface Props {
  tenantId: string;
}

/**
 * BindingsTable lists active role bindings in the current org and lets
 * administrators grant new bindings or revoke existing ones. Expired
 * bindings are reaped automatically by the backend TTL index.
 */
const BindingsTable: React.FC<Props> = ({ tenantId }) => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useListBindingsQuery(tenantId);
  const [deleteBinding, { isLoading: isDeleting }] = useDeleteBindingMutation();
  const [showCreate, setShowCreate] = useState(false);

  const dash = t('adminRoles.bindingsTable.dash');
  const unknownErr = t('adminRoles.bindingsTable.errorUnknown');

  const bindings: Binding[] = useMemo(() => data?.bindings ?? [], [data]);

  const onRevoke = useCallback(
    async (b: Binding) => {
      if (
        !window.confirm(
          t('adminRoles.bindingsTable.revokeConfirm', {
            role: b.roleName,
            user: shortUUID(b.userUUID, dash)
          })
        )
      )
        return;
      try {
        await deleteBinding({ tenantId, bindingId: b.id }).unwrap();
        toast.success(t('adminRoles.bindingsTable.toastRevoked'));
      } catch (err: unknown) {
        toast.error(
          t('adminRoles.bindingsTable.toastRevokeFailed', {
            error: extractError(err, unknownErr)
          })
        );
      }
    },
    [t, dash, deleteBinding, tenantId, unknownErr]
  );

  const columns = useMemo<ColumnDef<Binding>[]>(
    () => [
      {
        accessorKey: 'userUUID',
        header: t('adminRoles.bindingsTable.colUser'),
        cell: ({ row: { original } }) => (
          <code>{shortUUID(original.userUUID, dash)}</code>
        )
      },
      {
        accessorKey: 'roleName',
        header: t('adminRoles.bindingsTable.colRole'),
        cell: ({ row: { original } }) => (
          <SubtleBadge bg="info">{original.roleName}</SubtleBadge>
        )
      },
      {
        accessorKey: 'grantedAt',
        header: t('adminRoles.bindingsTable.colGranted'),
        meta: { cellProps: { className: 'text-muted small' } },
        cell: ({ row: { original } }) => (
          <>
            {new Date(original.grantedAt).toLocaleString()}
            {original.grantedBy && (
              <div>
                {t('adminRoles.bindingsTable.grantedByLine', {
                  actor: shortUUID(original.grantedBy, dash)
                })}
              </div>
            )}
          </>
        )
      },
      {
        accessorKey: 'expiresAt',
        header: t('adminRoles.bindingsTable.colExpires'),
        cell: ({ row: { original } }) =>
          original.expiresAt ? (
            <span className="text-warning small">
              {new Date(original.expiresAt).toLocaleString()}
            </span>
          ) : (
            <span className="text-muted small">
              {t('adminRoles.bindingsTable.expiresNever')}
            </span>
          )
      },
      {
        id: 'actions',
        // The column renders icon-only buttons, so the header carries the
        // only text a screen reader can use to announce it.
        header: () => (
          <span className="visually-hidden">
            {t('adminRoles.bindingsTable.colActions')}
          </span>
        ),
        enableSorting: false,
        meta: {
          headerProps: { style: { width: '1%' } },
          cellProps: { className: 'text-end' }
        },
        cell: ({ row: { original } }) => (
          <Button
            variant="outline-danger"
            size="sm"
            onClick={() => onRevoke(original)}
            disabled={isDeleting}
            aria-label={t('adminRoles.bindingsTable.revokeAria', {
              role: original.roleName,
              user: shortUUID(original.userUUID, dash)
            })}
          >
            <FontAwesomeIcon icon="times" />
          </Button>
        )
      }
    ],
    [t, dash, isDeleting, onRevoke]
  );

  const table = useAdvanceTable({
    data: bindings,
    columns,
    sortable: true,
    pagination: true,
    perPage: 10
  });

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" size="sm" />{' '}
        {t('adminRoles.bindingsTable.loading')}
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-danger">
        <Trans
          i18nKey="adminRoles.bindingsTable.errorIntro"
          components={{ code: <code /> }}
        />
      </div>
    );
  }

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3">
        <div>
          <Trans
            i18nKey={
              bindings.length === 1
                ? 'adminRoles.bindingsTable.countOne'
                : 'adminRoles.bindingsTable.countOther'
            }
            values={{ count: bindings.length }}
            components={{ strong: <strong /> }}
          />
        </div>
        <Button size="sm" variant="primary" onClick={() => setShowCreate(true)}>
          <FontAwesomeIcon icon="plus" className="me-1" />
          {t('adminRoles.bindingsTable.grantButton')}
        </Button>
      </div>

      {bindings.length === 0 ? (
        <div className="text-muted text-center py-4">
          <Trans
            i18nKey="adminRoles.bindingsTable.empty"
            components={{ strong: <strong /> }}
          />
        </div>
      ) : (
        <AdvanceTableProvider {...table}>
          <AdvanceTable
            headerClassName="bg-200 text-nowrap align-middle"
            rowClassName="align-middle"
            tableProps={{
              size: 'sm',
              striped: true,
              className: 'fs-10 mb-0 overflow-hidden'
            }}
          />
          <div className="mt-3">
            <AdvanceTableFooter rowsPerPageSelection rowInfo navButtons />
          </div>
        </AdvanceTableProvider>
      )}

      <CreateBindingModal
        tenantId={tenantId}
        show={showCreate}
        onHide={() => setShowCreate(false)}
      />
    </>
  );
};

function shortUUID(uuid: string, dash: string): string {
  if (!uuid) return dash;
  if (uuid.length <= 12) return uuid;
  return uuid.slice(0, 8) + '…' + uuid.slice(-4);
}

function extractError(err: unknown, unknownLabel: string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || unknownLabel;
  }
  return String(err);
}

export default BindingsTable;

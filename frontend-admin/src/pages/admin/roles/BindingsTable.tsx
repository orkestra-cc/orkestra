import { useState } from 'react';
import { Badge, Button, Spinner, Table } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
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

  const bindings: Binding[] = data?.bindings ?? [];

  const onRevoke = async (b: Binding) => {
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
  };

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
        <Table responsive hover className="mb-0">
          <thead className="table-light">
            <tr>
              <th>{t('adminRoles.bindingsTable.colUser')}</th>
              <th>{t('adminRoles.bindingsTable.colRole')}</th>
              <th>{t('adminRoles.bindingsTable.colGranted')}</th>
              <th>{t('adminRoles.bindingsTable.colExpires')}</th>
              <th style={{ width: '1%' }}></th>
            </tr>
          </thead>
          <tbody>
            {bindings.map(b => (
              <tr key={b.id}>
                <td>
                  <code>{shortUUID(b.userUUID, dash)}</code>
                </td>
                <td>
                  <Badge bg="info">{b.roleName}</Badge>
                </td>
                <td className="text-muted small">
                  {new Date(b.grantedAt).toLocaleString()}
                  {b.grantedBy && (
                    <div>
                      {t('adminRoles.bindingsTable.grantedByLine', {
                        actor: shortUUID(b.grantedBy, dash)
                      })}
                    </div>
                  )}
                </td>
                <td>
                  {b.expiresAt ? (
                    <span className="text-warning small">
                      {new Date(b.expiresAt).toLocaleString()}
                    </span>
                  ) : (
                    <span className="text-muted small">
                      {t('adminRoles.bindingsTable.expiresNever')}
                    </span>
                  )}
                </td>
                <td className="text-end">
                  <Button
                    variant="outline-danger"
                    size="sm"
                    onClick={() => onRevoke(b)}
                    disabled={isDeleting}
                  >
                    <FontAwesomeIcon icon="times" />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
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

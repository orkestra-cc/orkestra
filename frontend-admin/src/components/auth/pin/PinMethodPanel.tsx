import { useState } from 'react';
import { Alert, Button, Modal, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faBan,
  faKey,
  faTabletScreenButton
} from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import IconButton from 'components/common/IconButton';
import SubtleBadge from 'components/common/SubtleBadge';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import { formatDate, formatDateTime } from 'helpers/dateFormat';
import {
  pinErrorKey,
  type PinDevice,
  type PinEnrollmentCode,
  type PinMethodView
} from 'store/api/pinApi';
import SetPinModal from './SetPinModal';
import EnrollmentCodeModal from './EnrollmentCodeModal';

export interface PinMethodPanelProps {
  // admin: an administrator manages another user's PIN; self: the caller's own.
  mode: 'admin' | 'self';
  view: PinMethodView;
  busy: boolean;
  onSet: (pin: string) => Promise<void>;
  onRemove: () => Promise<void>;
  onIssueCode: () => Promise<PinEnrollmentCode>;
  onRevokeDevice: (deviceId: string) => Promise<void>;
}

/**
 * PinMethodPanel is the PIN sign-in surface shared by
 * the admin user profile, the client members tab and /user/security: state,
 * set / replace / remove, one-time device enrollment codes and the enrolled
 * devices. It never sees a PIN after submit, a hash or a device secret.
 */
const PinMethodPanel = ({
  mode,
  view,
  busy,
  onSet,
  onRemove,
  onIssueCode,
  onRevokeDevice
}: PinMethodPanelProps) => {
  const { t } = useTranslation();
  const [showSet, setShowSet] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [code, setCode] = useState<PinEnrollmentCode | null>(null);

  const fail = (err: unknown) => {
    const key = pinErrorKey(err);
    const detail = (err as { data?: { detail?: string } })?.data?.detail;
    toast.error(key ? t(key) : detail || t('pinMethod.errors.unknown'));
  };

  const submitPin = async (pin: string): Promise<boolean> => {
    try {
      await onSet(pin);
      toast.success(
        t(view.set ? 'pinMethod.toastReplaced' : 'pinMethod.toastSet')
      );
      setShowSet(false);
      return true;
    } catch (err) {
      fail(err);
      return false;
    }
  };

  const remove = async () => {
    try {
      await onRemove();
      toast.success(t('pinMethod.toastRemoved'));
    } catch (err) {
      fail(err);
    } finally {
      setConfirmRemove(false);
    }
  };

  const issueCode = async () => {
    try {
      setCode(await onIssueCode());
    } catch (err) {
      fail(err);
    }
  };

  const revoke = async (deviceId: string) => {
    try {
      await onRevokeDevice(deviceId);
      toast.success(t('pinMethod.toastDeviceRevoked'));
    } catch (err) {
      fail(err);
    }
  };

  const locked =
    view.lockedUntil && new Date(view.lockedUntil).getTime() > Date.now();

  const columns: ColumnDef<PinDevice>[] = [
    {
      id: 'device',
      accessorFn: d => d.deviceName || d.deviceId,
      header: t('pinMethod.devices.colDevice'),
      cell: ({ row: { original } }: CellContext<PinDevice, unknown>) => (
        <span className="fw-semibold text-900">
          {original.deviceName || original.deviceId}
        </span>
      )
    },
    {
      accessorKey: 'platform',
      header: t('pinMethod.devices.colPlatform'),
      cell: ({ row: { original } }: CellContext<PinDevice, unknown>) => (
        <span className="text-700">{original.platform || '—'}</span>
      )
    },
    {
      accessorKey: 'via',
      header: t('pinMethod.devices.colVia'),
      cell: ({ row: { original } }: CellContext<PinDevice, unknown>) => (
        <span className="text-700">
          {t(
            original.via === 'admin'
              ? 'pinMethod.devices.viaAdmin'
              : 'pinMethod.devices.viaSelf'
          )}
        </span>
      )
    },
    {
      id: 'createdAt',
      accessorFn: d => d.createdAt,
      header: t('pinMethod.devices.colEnrolled'),
      cell: ({ row: { original } }: CellContext<PinDevice, unknown>) => (
        <span className="text-700">{formatDate(original.createdAt)}</span>
      )
    },
    {
      id: 'lastUsedAt',
      accessorFn: d => d.lastUsedAt ?? '',
      header: t('pinMethod.devices.colLastUsed'),
      cell: ({ row: { original } }: CellContext<PinDevice, unknown>) => (
        <span className="text-700">{formatDateTime(original.lastUsedAt)}</span>
      )
    },
    {
      id: 'actions',
      header: '',
      enableSorting: false,
      meta: {
        headerProps: { className: 'text-end' },
        cellProps: { className: 'text-end' }
      },
      cell: ({ row: { original } }: CellContext<PinDevice, unknown>) => (
        <IconButton
          size="sm"
          variant="outline-secondary"
          icon={faBan}
          onClick={() => revoke(original.deviceId)}
          disabled={busy}
        >
          {t('pinMethod.devices.revoke')}
        </IconButton>
      )
    }
  ];

  const table = useAdvanceTable({
    data: view.devices,
    columns,
    sortable: true,
    pagination: false
  });

  return (
    <>
      {!view.available && (
        <Alert variant="info" className="fs-10">
          {t('pinMethod.unavailable')}
        </Alert>
      )}

      <div className="d-flex align-items-start justify-content-between flex-wrap gap-2 mb-3">
        <div>
          <div className="d-flex align-items-center mb-1">
            <FontAwesomeIcon icon={faKey} className="me-2 text-700" />
            <strong>{t('pinMethod.title')}</strong>
            <span className="ms-2">
              {!view.set ? (
                <SubtleBadge bg="secondary">
                  {t('pinMethod.badgeNotSet')}
                </SubtleBadge>
              ) : locked ? (
                <SubtleBadge bg="warning">
                  {t('pinMethod.badgeLocked')}
                </SubtleBadge>
              ) : (
                <SubtleBadge bg="success">
                  {t('pinMethod.badgeSet')}
                </SubtleBadge>
              )}
            </span>
          </div>
          <p className="mb-0 fs-10 text-700 ms-4">
            {view.set && view.updatedAt
              ? t('pinMethod.updatedAt', { date: formatDate(view.updatedAt) })
              : t(
                  mode === 'admin'
                    ? 'pinMethod.notSetAdmin'
                    : 'pinMethod.notSetSelf'
                )}
            {locked &&
              ' ' +
                t('pinMethod.lockedUntil', {
                  when: formatDateTime(view.lockedUntil)
                })}
          </p>
          <p className="mb-0 fs-11 text-600 ms-4 mt-1">
            {t('pinMethod.weakNote')}
          </p>
        </div>
        <div className="d-flex gap-2 flex-wrap">
          <Button
            variant="orkestra-default"
            size="sm"
            disabled={busy}
            onClick={() => setShowSet(true)}
          >
            {t(view.set ? 'pinMethod.replaceButton' : 'pinMethod.setButton')}
          </Button>
          {view.set && (
            <Button
              variant="orkestra-default"
              size="sm"
              disabled={busy}
              onClick={issueCode}
            >
              {busy ? (
                <Spinner size="sm" animation="border" />
              ) : (
                t('pinMethod.codeButton')
              )}
            </Button>
          )}
          {view.set && (
            <Button
              variant="outline-danger"
              size="sm"
              disabled={busy}
              onClick={() => setConfirmRemove(true)}
            >
              {t('pinMethod.removeButton')}
            </Button>
          )}
        </div>
      </div>

      <div className="d-flex align-items-center mb-2">
        <FontAwesomeIcon
          icon={faTabletScreenButton}
          className="me-2 text-700"
        />
        <strong className="fs-10">{t('pinMethod.devices.heading')}</strong>
      </div>
      <p className="fs-11 text-600 mb-2">
        {t(
          view.requiresTrustedDevice
            ? 'pinMethod.devices.introRequired'
            : 'pinMethod.devices.introOptional'
        )}
      </p>
      {view.devices.length === 0 ? (
        <p className="fs-10 text-600 mb-0">{t('pinMethod.devices.empty')}</p>
      ) : (
        <AdvanceTableProvider {...table}>
          <AdvanceTable
            headerClassName="bg-200 text-nowrap align-middle"
            rowClassName="align-middle white-space-nowrap"
            tableProps={{
              size: 'sm',
              striped: true,
              className: 'fs-10 mb-0 overflow-hidden'
            }}
          />
        </AdvanceTableProvider>
      )}

      <SetPinModal
        show={showSet}
        length={view.length}
        replacing={view.set}
        busy={busy}
        onHide={() => setShowSet(false)}
        onSubmit={submitPin}
      />
      <EnrollmentCodeModal code={code} onHide={() => setCode(null)} />
      <Modal
        show={confirmRemove}
        onHide={() => setConfirmRemove(false)}
        centered
      >
        <Modal.Header closeButton>
          <Modal.Title>{t('pinMethod.removeModal.title')}</Modal.Title>
        </Modal.Header>
        <Modal.Body className="fs-10">
          {t('pinMethod.removeModal.body')}
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="orkestra-default"
            onClick={() => setConfirmRemove(false)}
            disabled={busy}
          >
            {t('pinMethod.cancel')}
          </Button>
          <Button variant="danger" onClick={remove} disabled={busy}>
            {t('pinMethod.removeModal.submit')}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default PinMethodPanel;

import { useState } from 'react';
import { Alert, Button, Modal, Spinner } from 'react-bootstrap';
import { faBan, faLaptop } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import IconButton from 'components/common/IconButton';
import { formatDate } from 'helpers/dateFormat';
import { useTranslation } from 'react-i18next';
import {
  useListTrustedDevicesQuery,
  useRevokeAllTrustedDevicesMutation,
  useRevokeTrustedDeviceMutation,
  type TrustedDevice
} from 'store/api/deviceTrustApi';
import SecurityEmptyState from './SecurityEmptyState';
import SecurityTable from './SecurityTable';

// TrustedDevicesTab shows the "remember this device 30d" grants the
// user holds. Each grant lets the user skip the MFA prompt on the
// listed device for 30 days; revoking forces MFA on the next login.
//
// Same shell as the sessions pane: AdvanceTable, no card of its own, dates
// through the console's single formatting layer.
const TrustedDevicesTab = () => {
  const { t } = useTranslation();
  const { data, isLoading, isFetching } = useListTrustedDevicesQuery();
  const [revokeOne, { isLoading: revokingOne }] =
    useRevokeTrustedDeviceMutation();
  const [revokeAll, { isLoading: revokingAll }] =
    useRevokeAllTrustedDevicesMutation();
  const [showRevokeAll, setShowRevokeAll] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dash = t('userSecurity.trustedDevicesTab.dash');

  const devices = data?.devices ?? [];

  const onRevoke = async (deviceId: string) => {
    setError(null);
    try {
      await revokeOne({ deviceId }).unwrap();
    } catch (err: unknown) {
      const e = err as { data?: { detail?: string; title?: string } };
      setError(
        e?.data?.detail ||
          e?.data?.title ||
          t('userSecurity.trustedDevicesTab.errorOne')
      );
    }
  };

  const onConfirmRevokeAll = async () => {
    setError(null);
    try {
      await revokeAll().unwrap();
      setShowRevokeAll(false);
    } catch (err: unknown) {
      const e = err as { data?: { detail?: string; title?: string } };
      setError(
        e?.data?.detail ||
          e?.data?.title ||
          t('userSecurity.trustedDevicesTab.errorAll')
      );
    }
  };

  const columns: ColumnDef<TrustedDevice>[] = [
    {
      id: 'device',
      accessorFn: d => d.deviceName || d.deviceId,
      header: t('userSecurity.trustedDevicesTab.colDevice'),
      cell: ({ row: { original } }: CellContext<TrustedDevice, unknown>) => (
        <span className="fw-semibold text-900">
          {original.deviceName || original.deviceId}
        </span>
      )
    },
    {
      accessorKey: 'platform',
      header: t('userSecurity.trustedDevicesTab.colPlatform'),
      cell: ({ row: { original } }: CellContext<TrustedDevice, unknown>) => (
        <span className="text-700">{original.platform || dash}</span>
      )
    },
    {
      accessorKey: 'trustedAt',
      header: t('userSecurity.trustedDevicesTab.colTrustedSince'),
      cell: ({ row: { original } }: CellContext<TrustedDevice, unknown>) => (
        <span className="text-700">{formatDate(original.trustedAt)}</span>
      )
    },
    {
      accessorKey: 'trustedUntil',
      header: t('userSecurity.trustedDevicesTab.colExpires'),
      cell: ({ row: { original } }: CellContext<TrustedDevice, unknown>) => (
        <span className="text-700">{formatDate(original.trustedUntil)}</span>
      )
    },
    {
      id: 'actions',
      header: t('userSecurity.trustedDevicesTab.colActions'),
      enableSorting: false,
      meta: {
        headerProps: { className: 'text-end' },
        cellProps: { className: 'text-end' }
      },
      cell: ({ row: { original } }: CellContext<TrustedDevice, unknown>) => (
        <IconButton
          size="sm"
          variant="outline-secondary"
          icon={faBan}
          onClick={() => onRevoke(original.deviceId)}
          disabled={revokingOne || isFetching}
        >
          {t('userSecurity.trustedDevicesTab.rowRevoke')}
        </IconButton>
      )
    }
  ];

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" size="sm" />
      </div>
    );
  }

  return (
    <>
      <div className="d-flex justify-content-between align-items-start flex-wrap gap-2 mb-3">
        <p className="fs-10 text-muted mb-0">
          {t('userSecurity.trustedDevicesTab.intro')}
        </p>
        <IconButton
          size="sm"
          variant="outline-danger"
          icon={faBan}
          className="text-nowrap"
          onClick={() => setShowRevokeAll(true)}
          disabled={devices.length === 0 || revokingAll}
        >
          {t('userSecurity.trustedDevicesTab.revokeAllButton')}
        </IconButton>
      </div>

      {error && (
        <Alert variant="danger" className="fs-10">
          {error}
        </Alert>
      )}

      {devices.length === 0 ? (
        <SecurityEmptyState
          icon={faLaptop}
          message={t('userSecurity.trustedDevicesTab.empty')}
          hint={t('userSecurity.trustedDevicesTab.emptyHint')}
        />
      ) : (
        <SecurityTable
          data={devices}
          columns={columns}
          searchPlaceholder={t(
            'userSecurity.trustedDevicesTab.searchPlaceholder'
          )}
        />
      )}

      <Modal
        show={showRevokeAll}
        onHide={() => setShowRevokeAll(false)}
        centered
      >
        <Modal.Header closeButton>
          <Modal.Title>
            {t('userSecurity.trustedDevicesTab.modalTitle')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>{t('userSecurity.trustedDevicesTab.modalBody')}</Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowRevokeAll(false)}>
            {t('userSecurity.trustedDevicesTab.modalCancel')}
          </Button>
          <Button
            variant="danger"
            onClick={onConfirmRevokeAll}
            disabled={revokingAll}
          >
            {revokingAll
              ? t('userSecurity.trustedDevicesTab.modalSubmitting')
              : t('userSecurity.trustedDevicesTab.modalSubmit')}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default TrustedDevicesTab;

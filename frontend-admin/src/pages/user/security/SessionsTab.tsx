import { useState } from 'react';
import { Alert, Button, Modal, Spinner } from 'react-bootstrap';
import {
  faDesktop,
  faRightFromBracket
} from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import IconButton from 'components/common/IconButton';
import { byTimestamp } from 'components/common/advance-table/sorting';
import SubtleBadge from 'components/common/SubtleBadge';
import { formatDate, formatDateTime } from 'helpers/dateFormat';
import { useTranslation } from 'react-i18next';
import {
  useGetMySessionsQuery,
  useRevokeAllSessionsMutation,
  useRevokeSessionMutation,
  type SelfSessionInfo
} from 'store/api/authApi';
import SecurityEmptyState from './SecurityEmptyState';
import SecurityTable from './SecurityTable';

// Format a session row's friendly device label. The backend stores
// device name + platform separately so we can present whichever is
// available. Falling back to deviceId means tests against minimal
// fixtures still render rather than show empty cells.
function deviceLabel(s: SelfSessionInfo): string {
  if (s.deviceName) return s.deviceName;
  if (s.platform) return s.platform;
  return s.deviceId || s.sessionId.slice(0, 8);
}

// SessionsTab shows the user's active sessions and lets them revoke
// either one or all-except-current. Revoking the current session is
// disabled at the row level — the backend would 409 anyway, but
// graying the button is the better UX. Revoke-all only fires after a
// confirmation modal because the action terminates work in other
// browsers / tabs.
//
// The list goes through the console's AdvanceTable shell (search, sort,
// pagination) rather than raw <table> markup, and the pane carries no card of
// its own — the tab strip above already names it.
const SessionsTab = () => {
  const { t } = useTranslation();
  const { data, isLoading, isFetching } = useGetMySessionsQuery();
  const [revokeOne, { isLoading: revokingOne }] = useRevokeSessionMutation();
  const [revokeAll, { isLoading: revokingAll }] =
    useRevokeAllSessionsMutation();
  const [showRevokeAll, setShowRevokeAll] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const sessions = data?.sessions ?? [];
  const otherCount = sessions.filter(s => !s.isCurrent).length;

  const onRevokeOne = async (s: SelfSessionInfo) => {
    setError(null);
    try {
      await revokeOne({ sessionId: s.sessionId }).unwrap();
    } catch (err: unknown) {
      const e = err as {
        data?: { detail?: string; title?: string; code?: string };
      };
      if (e?.data?.code === 'step_up_required') return; // StepUpModal handles
      if (e?.data?.code === 'password_confirm_required') return; // PasswordConfirmModal handles
      if (e?.data?.code === 'mfa_enrollment_required') {
        setError(t('userSecurity.sessionsTab.errorMfaRequired'));
        return;
      }
      setError(
        e?.data?.detail ||
          e?.data?.title ||
          t('userSecurity.sessionsTab.errorOne')
      );
    }
  };

  const onConfirmRevokeAll = async () => {
    setError(null);
    try {
      await revokeAll().unwrap();
      setShowRevokeAll(false);
    } catch (err: unknown) {
      const e = err as {
        data?: { detail?: string; title?: string; code?: string };
      };
      if (e?.data?.code === 'step_up_required') {
        setShowRevokeAll(false);
        return;
      }
      if (e?.data?.code === 'password_confirm_required') {
        setShowRevokeAll(false);
        return;
      }
      if (e?.data?.code === 'mfa_enrollment_required') {
        setShowRevokeAll(false);
        setError(t('userSecurity.sessionsTab.errorMfaRequired'));
        return;
      }
      setError(
        e?.data?.detail ||
          e?.data?.title ||
          t('userSecurity.sessionsTab.errorAll')
      );
    }
  };

  const columns: ColumnDef<SelfSessionInfo>[] = [
    {
      id: 'device',
      accessorFn: deviceLabel,
      header: t('userSecurity.sessionsTab.colDevice'),
      cell: ({ row: { original } }: CellContext<SelfSessionInfo, unknown>) => (
        <>
          <span className="fw-semibold text-900">{deviceLabel(original)}</span>
          {original.isCurrent && (
            <SubtleBadge bg="success" pill className="ms-2 fs-11 fw-normal">
              {t('userSecurity.sessionsTab.currentBadge')}
            </SubtleBadge>
          )}
        </>
      )
    },
    {
      accessorKey: 'ipAddress',
      header: t('userSecurity.sessionsTab.colIp'),
      cell: ({ row: { original } }: CellContext<SelfSessionInfo, unknown>) => (
        <span className="text-700">
          {original.ipAddress || t('userSecurity.sessionsTab.dash')}
        </span>
      )
    },
    {
      id: 'lastActivity',
      // Formatted accessor + timestamp comparator — see byTimestamp.
      accessorFn: s => formatDateTime(s.lastActivity),
      sortingFn: byTimestamp<SelfSessionInfo>(s => s.lastActivity),
      header: t('userSecurity.sessionsTab.colLastActive'),
      cell: ({ row: { original } }: CellContext<SelfSessionInfo, unknown>) => (
        <span className="text-700">
          {formatDateTime(original.lastActivity)}
        </span>
      )
    },
    {
      id: 'createdAt',
      accessorFn: s => formatDate(s.createdAt),
      sortingFn: byTimestamp<SelfSessionInfo>(s => s.createdAt),
      header: t('userSecurity.sessionsTab.colStarted'),
      cell: ({ row: { original } }: CellContext<SelfSessionInfo, unknown>) => (
        <span className="text-700">{formatDate(original.createdAt)}</span>
      )
    },
    {
      id: 'actions',
      header: t('userSecurity.sessionsTab.colActions'),
      enableSorting: false,
      meta: {
        headerProps: { className: 'text-end' },
        cellProps: { className: 'text-end' }
      },
      cell: ({ row: { original } }: CellContext<SelfSessionInfo, unknown>) => (
        <IconButton
          size="sm"
          variant="outline-secondary"
          icon={faRightFromBracket}
          disabled={original.isCurrent || revokingOne || isFetching}
          onClick={() => onRevokeOne(original)}
        >
          {t('userSecurity.sessionsTab.rowRevoke')}
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
          {t('userSecurity.sessionsTab.intro')}
        </p>
        {/* Bulk sign-out is the destructive action of this pane, so it — and
            not the per-row button — is the one that carries the danger hue. */}
        <IconButton
          size="sm"
          variant="outline-danger"
          icon={faRightFromBracket}
          className="text-nowrap"
          onClick={() => setShowRevokeAll(true)}
          disabled={otherCount === 0 || revokingAll}
        >
          {t('userSecurity.sessionsTab.revokeAllButton')}
        </IconButton>
      </div>

      {error && (
        <Alert variant="danger" className="fs-10">
          {error}
        </Alert>
      )}

      {sessions.length === 0 ? (
        <SecurityEmptyState
          icon={faDesktop}
          message={t('userSecurity.sessionsTab.empty')}
          hint={t('userSecurity.sessionsTab.emptyHint')}
        />
      ) : (
        <SecurityTable
          data={sessions}
          columns={columns}
          searchPlaceholder={t('userSecurity.sessionsTab.searchPlaceholder')}
        />
      )}

      <Modal
        show={showRevokeAll}
        onHide={() => setShowRevokeAll(false)}
        centered
      >
        <Modal.Header closeButton>
          <Modal.Title>{t('userSecurity.sessionsTab.modalTitle')}</Modal.Title>
        </Modal.Header>
        <Modal.Body>{t('userSecurity.sessionsTab.modalBody')}</Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowRevokeAll(false)}>
            {t('userSecurity.sessionsTab.modalCancel')}
          </Button>
          <Button
            variant="danger"
            onClick={onConfirmRevokeAll}
            disabled={revokingAll}
          >
            {revokingAll
              ? t('userSecurity.sessionsTab.modalSubmitting')
              : t(
                  otherCount === 1
                    ? 'userSecurity.sessionsTab.modalSubmitOne'
                    : 'userSecurity.sessionsTab.modalSubmitOther',
                  { count: otherCount }
                )}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default SessionsTab;

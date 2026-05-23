import { useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Card,
  Modal,
  Spinner,
  Table
} from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useGetMySessionsQuery,
  useRevokeAllSessionsMutation,
  useRevokeSessionMutation,
  type SelfSessionInfo
} from 'store/api/authApi';

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
const SessionsTab = () => {
  const { t } = useTranslation();
  const { data, isLoading, isFetching } = useGetMySessionsQuery();
  const [revokeOne, { isLoading: revokingOne }] = useRevokeSessionMutation();
  const [revokeAll, { isLoading: revokingAll }] =
    useRevokeAllSessionsMutation();
  const [showRevokeAll, setShowRevokeAll] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dash = t('userSecurity.sessionsTab.dash');

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" size="sm" />
      </div>
    );
  }

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

  return (
    <>
      <Card className="shadow-none border">
        <Card.Header className="d-flex justify-content-between align-items-center flex-wrap gap-2">
          <Card.Title as="h5" className="mb-0">
            {t('userSecurity.sessionsTab.title')}
          </Card.Title>
          <Button
            variant="outline-danger"
            size="sm"
            onClick={() => setShowRevokeAll(true)}
            disabled={otherCount === 0 || revokingAll}
          >
            {t('userSecurity.sessionsTab.revokeAllButton')}
          </Button>
        </Card.Header>
        <Card.Body>
          {error && (
            <Alert variant="danger" className="fs-10">
              {error}
            </Alert>
          )}
          {sessions.length === 0 ? (
            <p className="fs-10 text-muted mb-0">
              {t('userSecurity.sessionsTab.empty')}
            </p>
          ) : (
            <Table responsive size="sm" className="mb-0 align-middle">
              <thead>
                <tr>
                  <th>{t('userSecurity.sessionsTab.colDevice')}</th>
                  <th>{t('userSecurity.sessionsTab.colIp')}</th>
                  <th>{t('userSecurity.sessionsTab.colLastActive')}</th>
                  <th>{t('userSecurity.sessionsTab.colStarted')}</th>
                  <th className="text-end">
                    {t('userSecurity.sessionsTab.colActions')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {sessions.map(s => (
                  <tr key={s.sessionId}>
                    <td>
                      {deviceLabel(s)}
                      {s.isCurrent && (
                        <Badge bg="success" className="ms-2">
                          {t('userSecurity.sessionsTab.currentBadge')}
                        </Badge>
                      )}
                    </td>
                    <td className="fs-10 text-muted">{s.ipAddress || dash}</td>
                    <td className="fs-10 text-muted">
                      {new Date(s.lastActivity).toLocaleString()}
                    </td>
                    <td className="fs-10 text-muted">
                      {new Date(s.createdAt).toLocaleDateString()}
                    </td>
                    <td className="text-end">
                      <Button
                        variant="outline-danger"
                        size="sm"
                        disabled={s.isCurrent || revokingOne || isFetching}
                        onClick={() => onRevokeOne(s)}
                      >
                        {t('userSecurity.sessionsTab.rowRevoke')}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card.Body>
      </Card>

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

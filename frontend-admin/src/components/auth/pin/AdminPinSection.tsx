import { Alert, Button, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useGetUserPinAdminQuery,
  useIssueUserPinEnrollmentCodeAdminMutation,
  useRemoveUserPinAdminMutation,
  useRevokeUserPinDeviceAdminMutation,
  useSetUserPinAdminMutation,
  type PinTier
} from 'store/api/pinApi';
import PinMethodPanel from './PinMethodPanel';

interface AdminPinSectionProps {
  userId: string;
  tier: PinTier;
}

/**
 * The administrator's view of another user's PIN. Reading needs user.read,
 * every change user.update plus a fresh step-up (the global StepUpModal
 * catches a stale token and replays). A 403 on the read hides the section:
 * a caller without user.read is not entitled to it; a caller with read but
 * not update sees the panel without its controls.
 */
const AdminPinSection = ({ userId, tier }: AdminPinSectionProps) => {
  const { t } = useTranslation();
  const target = { id: userId, tier };
  const { data, isLoading, error, refetch } = useGetUserPinAdminQuery(target);
  const [setPin, setState] = useSetUserPinAdminMutation();
  const [removePin, removeState] = useRemoveUserPinAdminMutation();
  const [issueCode, codeState] = useIssueUserPinEnrollmentCodeAdminMutation();
  const [revokeDevice, revokeState] = useRevokeUserPinDeviceAdminMutation();
  const busy =
    setState.isLoading ||
    removeState.isLoading ||
    codeState.isLoading ||
    revokeState.isLoading;

  if (isLoading) {
    return (
      <div className="text-center py-3">
        <Spinner animation="border" size="sm" />
      </div>
    );
  }
  if (
    error &&
    typeof error === 'object' &&
    'status' in error &&
    error.status === 403
  ) {
    return null;
  }
  if (error || !data) {
    return (
      <Alert variant="warning" className="mb-0 fs-10">
        {t('pinMethod.loadError')}{' '}
        <Button variant="link" className="p-0 fs-10" onClick={() => refetch()}>
          {t('pinMethod.retry')}
        </Button>
      </Alert>
    );
  }
  return (
    <PinMethodPanel
      mode="admin"
      view={data}
      busy={busy}
      // The server says whether the caller holds user.update.
      readOnly={!data.canManage}
      onSet={pin => setPin({ ...target, pin }).unwrap()}
      onRemove={() => removePin(target).unwrap()}
      onIssueCode={() => issueCode(target).unwrap()}
      onRevokeDevice={deviceId =>
        revokeDevice({ ...target, deviceId }).unwrap()
      }
    />
  );
};

export default AdminPinSection;

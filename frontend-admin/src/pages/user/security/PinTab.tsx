import { Alert, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import PinMethodPanel from 'components/auth/pin/PinMethodPanel';
import {
  useGetSelfPinQuery,
  useIssueSelfPinEnrollmentCodeMutation,
  useRemoveSelfPinMutation,
  useRevokeSelfPinDeviceMutation,
  useSetSelfPinMutation
} from 'store/api/pinApi';

// The caller's own PIN. Changes need a fresh step-up and are refused from a
// PIN session (auth.pin_session_forbidden): the panel's error toast says to
// sign in with a stronger method.
const PinTab = () => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useGetSelfPinQuery();
  const [setPin, setState] = useSetSelfPinMutation();
  const [removePin, removeState] = useRemoveSelfPinMutation();
  const [issueCode, codeState] = useIssueSelfPinEnrollmentCodeMutation();
  const [revokeDevice, revokeState] = useRevokeSelfPinDeviceMutation();
  const busy =
    setState.isLoading ||
    removeState.isLoading ||
    codeState.isLoading ||
    revokeState.isLoading;

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" size="sm" />
      </div>
    );
  }
  if (error || !data) {
    return (
      <Alert variant="warning" className="fs-10 mb-0">
        {t('pinMethod.loadError')}
      </Alert>
    );
  }
  return (
    <>
      <p className="fs-10 text-muted mb-3">{t('userSecurity.pinTab.intro')}</p>
      <PinMethodPanel
        mode="self"
        view={data}
        busy={busy}
        onSet={pin => setPin({ pin }).unwrap()}
        onRemove={() => removePin().unwrap()}
        onIssueCode={() => issueCode().unwrap()}
        onRevokeDevice={deviceId => revokeDevice({ deviceId }).unwrap()}
      />
    </>
  );
};

export default PinTab;

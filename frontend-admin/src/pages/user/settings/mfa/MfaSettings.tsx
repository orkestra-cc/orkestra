import { useState } from 'react';
import { Badge, Button, Card, ListGroup, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faShieldHalved, faKey } from '@fortawesome/free-solid-svg-icons';
import {
  useGetMfaStatusQuery,
  useWebAuthnListQuery,
  useWebAuthnRemoveMutation,
} from 'store/api/mfaApi';
import { browserSupportsWebAuthn } from 'store/api/webauthnCodec';
import MfaEnrollWizard from './MfaEnrollWizard';
import MfaRemoveModal from './MfaRemoveModal';
import WebAuthnEnrollDialog from './WebAuthnEnrollDialog';

/**
 * Security-settings panel for the current user's second factors. Renders
 * two cards side-by-side conceptually:
 *   - Authenticator app (TOTP) — single factor with backup codes;
 *   - Passkeys — zero-or-many WebAuthn credentials, each individually removable.
 *
 * The backend reports overall enrolled state on the same status endpoint
 * (`/v1/auth/operator/me/mfa`) so the cards stay in sync without separate fetches.
 */
const MfaSettings = () => {
  const { data, isLoading, refetch } = useGetMfaStatusQuery();
  const [showEnroll, setShowEnroll] = useState(false);
  const [showRemove, setShowRemove] = useState(false);
  const [showPasskey, setShowPasskey] = useState(false);

  const totpStatus = data?.type === 'totp' && data.status === 'enrolled';
  const totpPending = false;
  const webauthnCount = data?.webauthnCredentials ?? 0;
  const supports = browserSupportsWebAuthn();

  return (
    <>
      <Card className="mb-3">
        <Card.Header className="bg-body-tertiary">
          <div className="d-flex align-items-center">
            <FontAwesomeIcon icon={faShieldHalved} className="me-2 text-primary" />
            <Card.Title as="h5" className="mb-0">Authenticator app</Card.Title>
          </div>
        </Card.Header>
        <Card.Body>
          {isLoading ? (
            <div className="text-center py-3"><Spinner size="sm" /></div>
          ) : totpStatus ? (
            <div>
              <div className="d-flex align-items-center mb-2">
                <Badge bg="success" className="me-2">Enabled</Badge>
                <span className="text-muted fs-10">
                  TOTP · {data?.backupCodesRemaining ?? 0} backup codes remaining
                </span>
              </div>
              <p className="fs-10 text-muted mb-3">
                A one-time code from your authenticator is required each time you sign in.
              </p>
              <Button variant="outline-danger" size="sm" onClick={() => setShowRemove(true)}>
                Remove factor
              </Button>
            </div>
          ) : totpPending ? (
            <div>
              <Badge bg="warning" className="mb-2">Enrollment in progress</Badge>
              <p className="fs-10 text-muted mb-3">
                Your authenticator is registered but never confirmed. Complete or restart enrollment below.
              </p>
              <Button variant="primary" size="sm" onClick={() => setShowEnroll(true)}>
                Resume enrollment
              </Button>
            </div>
          ) : (
            <div>
              <p className="fs-10 text-muted mb-3">
                Add a code from an authenticator app such as Google Authenticator, Authy, or 1Password.
              </p>
              <Button variant="primary" size="sm" onClick={() => setShowEnroll(true)}>
                Set up
              </Button>
            </div>
          )}
        </Card.Body>
      </Card>

      <PasskeysCard
        count={webauthnCount}
        supports={supports}
        onEnroll={() => setShowPasskey(true)}
        onRemoved={refetch}
      />

      <MfaEnrollWizard
        show={showEnroll}
        onHide={() => { setShowEnroll(false); refetch(); }}
      />
      <MfaRemoveModal
        show={showRemove}
        onHide={() => { setShowRemove(false); refetch(); }}
      />
      <WebAuthnEnrollDialog
        show={showPasskey}
        onHide={() => { setShowPasskey(false); refetch(); }}
      />
    </>
  );
};

interface PasskeysCardProps {
  count: number;
  supports: boolean;
  onEnroll: () => void;
  onRemoved: () => void;
}

// Passkeys card. Lists per-credential metadata + a delete button. The
// list query only fires when the status reports at least one credential
// to keep the wire chatter minimal on accounts that only use TOTP.
const PasskeysCard = ({ count, supports, onEnroll, onRemoved }: PasskeysCardProps) => {
  const { data: list, refetch } = useWebAuthnListQuery(undefined, { skip: count === 0 });
  const [remove, { isLoading: removing }] = useWebAuthnRemoveMutation();

  const handleRemove = async (credentialId: string) => {
    if (!confirm('Remove this passkey? You will need to register it again to use it.')) return;
    try {
      await remove({ credentialId }).unwrap();
      refetch();
      onRemoved();
    } catch {
      // 401 step_up_required is intercepted by the global StepUpModal;
      // other errors leave the row in place — the user can retry.
    }
  };

  return (
    <Card className="mb-3">
      <Card.Header className="bg-body-tertiary">
        <div className="d-flex align-items-center">
          <FontAwesomeIcon icon={faKey} className="me-2 text-primary" />
          <Card.Title as="h5" className="mb-0">Passkeys</Card.Title>
        </div>
      </Card.Header>
      <Card.Body>
        {!supports && (
          <p className="fs-10 text-muted mb-3">
            This browser does not support passkeys. Try Chrome, Safari, or Firefox over HTTPS to use this feature.
          </p>
        )}
        {count === 0 ? (
          <p className="fs-10 text-muted mb-3">
            Passkeys let you sign in with a fingerprint, Face ID, or a hardware key — no codes to type.
            They can be used as a second factor alongside the authenticator app, or on their own.
          </p>
        ) : (
          <ListGroup variant="flush" className="mb-3">
            {(list?.credentials ?? []).map((c) => (
              <ListGroup.Item key={c.credentialId} className="px-0 d-flex justify-content-between align-items-center">
                <div>
                  <div className="fw-semibold">{c.name}</div>
                  <div className="text-muted fs-10">
                    Added {new Date(c.createdAt).toLocaleDateString()}
                    {c.lastUsedAt && ` · Last used ${new Date(c.lastUsedAt).toLocaleDateString()}`}
                    {c.cloneWarning && ' · ⚠ clone warning'}
                  </div>
                </div>
                <Button
                  variant="outline-danger"
                  size="sm"
                  disabled={removing}
                  onClick={() => handleRemove(c.credentialId)}
                >
                  Remove
                </Button>
              </ListGroup.Item>
            ))}
          </ListGroup>
        )}
        <Button variant={count === 0 ? 'primary' : 'outline-primary'} size="sm" disabled={!supports} onClick={onEnroll}>
          Add passkey
        </Button>
      </Card.Body>
    </Card>
  );
};

export default MfaSettings;

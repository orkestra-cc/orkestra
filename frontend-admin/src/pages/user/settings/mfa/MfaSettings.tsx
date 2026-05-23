import { useState } from 'react';
import { Badge, Button, Card, ListGroup, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faShieldHalved, faKey } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import {
  useGetMfaStatusQuery,
  useWebAuthnListQuery,
  useWebAuthnRemoveMutation
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
  const { t } = useTranslation();
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
            <FontAwesomeIcon
              icon={faShieldHalved}
              className="me-2 text-primary"
            />
            <Card.Title as="h5" className="mb-0">
              {t('userMfa.settings.totp.cardTitle')}
            </Card.Title>
          </div>
        </Card.Header>
        <Card.Body>
          {isLoading ? (
            <div className="text-center py-3">
              <Spinner size="sm" />
            </div>
          ) : totpStatus ? (
            <div>
              <div className="d-flex align-items-center mb-2">
                <Badge bg="success" className="me-2">
                  {t('userMfa.settings.totp.enabledBadge')}
                </Badge>
                <span className="text-muted fs-10">
                  {t('userMfa.settings.totp.enabledStatus', {
                    count: data?.backupCodesRemaining ?? 0
                  })}
                </span>
              </div>
              <p className="fs-10 text-muted mb-3">
                {t('userMfa.settings.totp.enabledDescription')}
              </p>
              <Button
                variant="outline-danger"
                size="sm"
                onClick={() => setShowRemove(true)}
              >
                {t('userMfa.settings.removeFactor')}
              </Button>
            </div>
          ) : totpPending ? (
            <div>
              <Badge bg="warning" className="mb-2">
                {t('userMfa.settings.totp.pendingBadge')}
              </Badge>
              <p className="fs-10 text-muted mb-3">
                {t('userMfa.settings.totp.pendingDescription')}
              </p>
              <Button
                variant="primary"
                size="sm"
                onClick={() => setShowEnroll(true)}
              >
                {t('userMfa.settings.totp.resumeButton')}
              </Button>
            </div>
          ) : (
            <div>
              <p className="fs-10 text-muted mb-3">
                {t('userMfa.settings.totp.notEnrolledDescription')}
              </p>
              <Button
                variant="primary"
                size="sm"
                onClick={() => setShowEnroll(true)}
              >
                {t('userMfa.settings.totp.setupButton')}
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
        onHide={() => {
          setShowEnroll(false);
          refetch();
        }}
      />
      <MfaRemoveModal
        show={showRemove}
        onHide={() => {
          setShowRemove(false);
          refetch();
        }}
      />
      <WebAuthnEnrollDialog
        show={showPasskey}
        onHide={() => {
          setShowPasskey(false);
          refetch();
        }}
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
const PasskeysCard = ({
  count,
  supports,
  onEnroll,
  onRemoved
}: PasskeysCardProps) => {
  const { t } = useTranslation();
  const { data: list, refetch } = useWebAuthnListQuery(undefined, {
    skip: count === 0
  });
  const [remove, { isLoading: removing }] = useWebAuthnRemoveMutation();

  const handleRemove = async (credentialId: string) => {
    if (!confirm(t('userMfa.settings.passkeys.removeConfirm'))) return;
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
          <Card.Title as="h5" className="mb-0">
            {t('userMfa.settings.passkeys.cardTitle')}
          </Card.Title>
        </div>
      </Card.Header>
      <Card.Body>
        {!supports && (
          <p className="fs-10 text-muted mb-3">
            {t('userMfa.settings.passkeys.unsupported')}
          </p>
        )}
        {count === 0 ? (
          <p className="fs-10 text-muted mb-3">
            {t('userMfa.settings.passkeys.introEmpty')}
          </p>
        ) : (
          <ListGroup variant="flush" className="mb-3">
            {(list?.credentials ?? []).map(c => (
              <ListGroup.Item
                key={c.credentialId}
                className="px-0 d-flex justify-content-between align-items-center"
              >
                <div>
                  <div className="fw-semibold">{c.name}</div>
                  <div className="text-muted fs-10">
                    {t('userMfa.settings.passkeys.addedAt', {
                      date: new Date(c.createdAt).toLocaleDateString()
                    })}
                    {c.lastUsedAt &&
                      t('userMfa.settings.passkeys.lastUsedSuffix', {
                        date: new Date(c.lastUsedAt).toLocaleDateString()
                      })}
                    {c.cloneWarning &&
                      t('userMfa.settings.passkeys.cloneWarningSuffix')}
                  </div>
                </div>
                <Button
                  variant="outline-danger"
                  size="sm"
                  disabled={removing}
                  onClick={() => handleRemove(c.credentialId)}
                >
                  {t('userMfa.settings.passkeys.removeButton')}
                </Button>
              </ListGroup.Item>
            ))}
          </ListGroup>
        )}
        <Button
          variant={count === 0 ? 'primary' : 'outline-primary'}
          size="sm"
          disabled={!supports}
          onClick={onEnroll}
        >
          {t('userMfa.settings.passkeys.addButton')}
        </Button>
      </Card.Body>
    </Card>
  );
};

export default MfaSettings;

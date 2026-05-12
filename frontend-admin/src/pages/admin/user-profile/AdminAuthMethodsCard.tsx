import React, { useState } from 'react';
import {
  Card,
  Button,
  Dropdown,
  Spinner,
  Alert,
  Badge,
  OverlayTrigger,
  Tooltip
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import { useAppSelector } from 'store/hooks';
import { selectUser } from 'store/slices/authSlice';
import {
  AdminAuthMethods,
  AdminAuthMfaFactor,
  AdminAuthOAuthProvider,
  OAuthProviderName,
  User,
  useGetUserAuthMethodsAdminQuery,
  useResendVerificationUserAdminMutation,
  useSendPasswordResetUserAdminMutation,
  useUnlinkOAuthUserAdminMutation
} from 'store/api/userApi';
import AdminResetMfaModal from '../users/AdminResetMfaModal';

interface AdminAuthMethodsCardProps {
  user: User;
}

/**
 * Authentication Methods card on /admin/user/profile/:userId.
 *
 * Surfaces the operator user's full auth state — password, MFA factors,
 * OAuth identities, email verification — and exposes per-method admin
 * actions. Reuses AdminResetMfaModal for the MFA reset step-up flow;
 * everything else flows through dedicated mutations on userApi.
 *
 * Self-action and last-credential safeguards live both here (UX hint —
 * hide / disable + tooltip) and in the backend service (authoritative
 * 409 with a typed body code).
 */
const AdminAuthMethodsCard: React.FC<AdminAuthMethodsCardProps> = ({
  user
}) => {
  const currentAdmin = useAppSelector(selectUser);
  const isSelf = currentAdmin?.id === user.id;

  const { data, isLoading, error, refetch } = useGetUserAuthMethodsAdminQuery(
    user.id
  );

  const [sendPasswordReset, { isLoading: pwBusy }] =
    useSendPasswordResetUserAdminMutation();
  const [resendVerification, { isLoading: verifyBusy }] =
    useResendVerificationUserAdminMutation();
  const [unlinkOAuth, { isLoading: unlinkBusy }] =
    useUnlinkOAuthUserAdminMutation();

  const [showMfaModal, setShowMfaModal] = useState(false);
  const [confirmUnlink, setConfirmUnlink] =
    useState<AdminAuthOAuthProvider | null>(null);

  const handleSendReset = async () => {
    try {
      await sendPasswordReset(user.id).unwrap();
      toast.success('Password-reset email sent');
    } catch (err) {
      toast.error('Could not send password reset: ' + extractError(err));
    }
  };

  const handleResendVerification = async () => {
    try {
      await resendVerification(user.id).unwrap();
      toast.success('Verification email re-sent');
    } catch (err) {
      toast.error('Could not resend verification: ' + extractError(err));
    }
  };

  const handleUnlink = async (provider: OAuthProviderName) => {
    try {
      await unlinkOAuth({ id: user.id, provider }).unwrap();
      toast.success(`Unlinked ${provider}`);
      setConfirmUnlink(null);
    } catch (err) {
      // Surface the typed last-credential / self-action codes if the
      // server pushes back; otherwise fall through to the generic msg.
      const code = (err as { data?: { type?: string; title?: string } })?.data
        ?.title;
      if (code === 'last_credential') {
        toast.error(
          'User has no other login method — send a password-reset first.'
        );
      } else if (code === 'self_action') {
        toast.error(
          'You cannot unlink your own OAuth identity from the admin surface.'
        );
      } else {
        toast.error('Unlink failed: ' + extractError(err));
      }
      setConfirmUnlink(null);
    }
  };

  if (isLoading) {
    return (
      <Card className="mb-3">
        <Card.Header className="bg-body-tertiary">
          <h5 className="mb-0">
            <FontAwesomeIcon icon="shield-alt" className="me-2" />
            Authentication Methods
          </h5>
        </Card.Header>
        <Card.Body className="text-center py-4">
          <Spinner animation="border" size="sm" />
        </Card.Body>
      </Card>
    );
  }

  if (error || !data) {
    return (
      <Card className="mb-3">
        <Card.Header className="bg-body-tertiary">
          <h5 className="mb-0">
            <FontAwesomeIcon icon="shield-alt" className="me-2" />
            Authentication Methods
          </h5>
        </Card.Header>
        <Card.Body>
          <Alert variant="warning" className="mb-0">
            Could not load auth methods.{' '}
            <Button variant="link" className="p-0" onClick={() => refetch()}>
              Retry
            </Button>
          </Alert>
        </Card.Body>
      </Card>
    );
  }

  return (
    <>
      <Card className="mb-3">
        <Card.Header className="bg-body-tertiary">
          <h5 className="mb-0">
            <FontAwesomeIcon icon="shield-alt" className="me-2" />
            Authentication Methods
          </h5>
        </Card.Header>
        <Card.Body>
          {/* Password */}
          <Section
            icon="key"
            title="Password"
            badge={
              data.hasUsablePassword ? (
                <Badge bg="success">Set</Badge>
              ) : (
                <Badge bg="secondary">Not set</Badge>
              )
            }
            sub={
              data.hasUsablePassword && data.passwordUpdatedAt
                ? `Last changed ${formatDate(data.passwordUpdatedAt)}`
                : 'No password set — user signs in via OAuth only.'
            }
            action={
              <Button
                variant="orkestra-default"
                size="sm"
                disabled={pwBusy}
                onClick={handleSendReset}
              >
                {pwBusy ? (
                  <Spinner size="sm" animation="border" />
                ) : (
                  'Send password-reset email'
                )}
              </Button>
            }
          />

          {/* Email verification */}
          <Section
            icon="envelope"
            title="Email verification"
            badge={
              data.emailVerified ? (
                <Badge bg="success">Verified</Badge>
              ) : (
                <Badge bg="warning" text="dark">
                  Unverified
                </Badge>
              )
            }
            sub={
              data.emailVerified
                ? 'The user has confirmed their email address.'
                : 'The user has not yet confirmed their email address.'
            }
            action={
              !data.emailVerified ? (
                <Button
                  variant="orkestra-default"
                  size="sm"
                  disabled={verifyBusy}
                  onClick={handleResendVerification}
                >
                  {verifyBusy ? (
                    <Spinner size="sm" animation="border" />
                  ) : (
                    'Resend verification'
                  )}
                </Button>
              ) : null
            }
          />

          {/* MFA */}
          <Section
            icon="lock"
            title="Multi-factor authentication"
            badge={
              data.mfaRequired ? (
                <Badge bg="primary">Required</Badge>
              ) : (
                <Badge bg="secondary">Optional</Badge>
              )
            }
            sub={describeMfa(data)}
            action={
              data.mfaFactors.length > 0 && !isSelf ? (
                <Button
                  variant="outline-warning"
                  size="sm"
                  onClick={() => setShowMfaModal(true)}
                >
                  Reset MFA
                </Button>
              ) : data.mfaFactors.length > 0 && isSelf ? (
                <OverlayTrigger
                  placement="left"
                  overlay={
                    <Tooltip>
                      Use the self-service flow on your own account.
                    </Tooltip>
                  }
                >
                  <span className="d-inline-block">
                    <Button variant="outline-warning" size="sm" disabled>
                      Reset MFA
                    </Button>
                  </span>
                </OverlayTrigger>
              ) : null
            }
          >
            {data.mfaFactors.length > 0 && (
              <ul className="list-unstyled mb-0 mt-2 ms-4 small text-body-secondary">
                {data.mfaFactors.map(f => (
                  <li key={f.type}>{describeFactor(f)}</li>
                ))}
              </ul>
            )}
          </Section>

          {/* OAuth providers */}
          <div className="pt-3">
            <div className="d-flex align-items-center mb-2">
              <FontAwesomeIcon icon="link" className="me-2 text-700" />
              <strong>OAuth identities</strong>
              <span className="ms-2 text-body-secondary small">
                ({data.oauthProviders.length})
              </span>
            </div>
            {data.oauthProviders.length === 0 ? (
              <p className="small text-body-secondary mb-0 ms-4">
                No OAuth identities are linked to this account.
              </p>
            ) : (
              <ul className="list-unstyled mb-0 ms-4">
                {data.oauthProviders.map(p => {
                  const onlyCredential =
                    !data.hasUsablePassword && data.oauthProviders.length === 1;
                  const blockReason = isSelf
                    ? 'You cannot unlink your own OAuth identity from the admin surface.'
                    : onlyCredential
                      ? 'Send a password-reset first to keep the account accessible.'
                      : null;
                  return (
                    <li
                      key={p.provider}
                      className="d-flex align-items-center justify-content-between py-1"
                    >
                      <span>
                        <strong className="text-capitalize me-2">
                          {p.provider}
                        </strong>
                        <span className="text-body-secondary small">
                          {p.email}
                        </span>
                        {p.isPrimary && (
                          <Badge bg="info" className="ms-2">
                            Primary
                          </Badge>
                        )}
                        <span className="ms-2 small text-body-secondary">
                          linked {formatDate(p.linkedAt)}
                        </span>
                      </span>
                      <ProviderActions
                        provider={p}
                        blockReason={blockReason}
                        unlinkBusy={unlinkBusy}
                        onUnlinkClick={() => setConfirmUnlink(p)}
                      />
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </Card.Body>
      </Card>

      {/* MFA reset modal — reused from /admin/users. tier=operator
         because the profile route serves Tier-1 internal users only. */}
      <AdminResetMfaModal
        show={showMfaModal}
        user={user}
        tier="operator"
        onHide={() => setShowMfaModal(false)}
      />

      {/* Inline confirmation for OAuth unlink — small, focused, no
         step-up prompt: the backend handler is wrapped in
         RequireStepUp and the global StepUpModal will catch a stale
         token and replay the request. */}
      {confirmUnlink && (
        <Alert
          variant="warning"
          className="position-fixed bottom-0 end-0 m-3"
          style={{ zIndex: 1080, maxWidth: 380 }}
        >
          <strong>Unlink {confirmUnlink.provider}?</strong>
          <p className="mb-2 small">
            The user will lose this login method. They can re-link from their
            account settings later.
          </p>
          <div className="d-flex gap-2 justify-content-end">
            <Button
              size="sm"
              variant="outline-secondary"
              onClick={() => setConfirmUnlink(null)}
              disabled={unlinkBusy}
            >
              Cancel
            </Button>
            <Button
              size="sm"
              variant="warning"
              onClick={() => handleUnlink(confirmUnlink.provider)}
              disabled={unlinkBusy}
            >
              {unlinkBusy ? <Spinner size="sm" animation="border" /> : 'Unlink'}
            </Button>
          </div>
        </Alert>
      )}
    </>
  );
};

// --- presentational helpers ---

interface SectionProps {
  icon: string;
  title: string;
  badge: React.ReactNode;
  sub: string;
  action: React.ReactNode;
  children?: React.ReactNode;
}

const Section: React.FC<SectionProps> = ({
  icon,
  title,
  badge,
  sub,
  action,
  children
}) => (
  <div className="py-3 border-bottom">
    <div className="d-flex align-items-start justify-content-between">
      <div>
        <div className="d-flex align-items-center mb-1">
          <FontAwesomeIcon icon={icon as never} className="me-2 text-700" />
          <strong>{title}</strong>
          <span className="ms-2">{badge}</span>
        </div>
        <p className="mb-0 small text-body-secondary ms-4">{sub}</p>
        {children}
      </div>
      <div className="ms-3">{action}</div>
    </div>
  </div>
);

interface ProviderActionsProps {
  provider: AdminAuthOAuthProvider;
  blockReason: string | null;
  unlinkBusy: boolean;
  onUnlinkClick: () => void;
}

const ProviderActions: React.FC<ProviderActionsProps> = ({
  provider,
  blockReason,
  unlinkBusy,
  onUnlinkClick
}) => {
  if (blockReason) {
    return (
      <OverlayTrigger
        placement="left"
        overlay={<Tooltip>{blockReason}</Tooltip>}
      >
        <span className="d-inline-block">
          <Button
            variant="link"
            size="sm"
            disabled
            className="text-body-tertiary"
          >
            <FontAwesomeIcon icon="ellipsis-h" />
          </Button>
        </span>
      </OverlayTrigger>
    );
  }
  return (
    <Dropdown>
      <Dropdown.Toggle
        variant="orkestra-default"
        size="sm"
        disabled={unlinkBusy}
        aria-label={`actions for ${provider.provider}`}
      >
        <FontAwesomeIcon icon="ellipsis-h" />
      </Dropdown.Toggle>
      <Dropdown.Menu className="border py-0">
        <Dropdown.Item onClick={onUnlinkClick} className="text-warning">
          <FontAwesomeIcon icon="unlink" className="me-2" />
          Unlink…
        </Dropdown.Item>
      </Dropdown.Menu>
    </Dropdown>
  );
};

function describeMfa(data: AdminAuthMethods): string {
  if (data.mfaFactors.length === 0) {
    if (data.mfaRequired) {
      if (data.mfaGraceExpiresAt) {
        return `Required, no factor enrolled. Grace period ends ${formatDate(data.mfaGraceExpiresAt)}.`;
      }
      return 'Required for this role; no factor enrolled yet.';
    }
    return 'Not enrolled.';
  }
  return `${data.mfaFactors.length} factor${data.mfaFactors.length === 1 ? '' : 's'} enrolled.`;
}

function describeFactor(f: AdminAuthMfaFactor): string {
  if (f.type === 'totp') {
    const enrolled = f.enrolledAt ? formatDate(f.enrolledAt) : 'unknown';
    const codes =
      typeof f.backupCodesRemaining === 'number'
        ? `, ${f.backupCodesRemaining} backup code${f.backupCodesRemaining === 1 ? '' : 's'} remaining`
        : '';
    return `TOTP authenticator app — enrolled ${enrolled}${codes}`;
  }
  if (f.type === 'webauthn') {
    const n = f.credentials?.length ?? 0;
    const names = (f.credentials ?? [])
      .map(c => c.name)
      .filter(Boolean)
      .join(', ');
    return `${n} passkey${n === 1 ? '' : 's'}${names ? ` (${names})` : ''}`;
  }
  return f.type;
}

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric'
    });
  } catch {
    return iso;
  }
}

function extractError(err: unknown): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || 'unknown error';
  }
  return String(err);
}

export default AdminAuthMethodsCard;

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
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
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
  const { t } = useTranslation();
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

  const unknownErr = t('adminUserProfile.authMethods.errorUnknown');

  const handleSendReset = async () => {
    try {
      await sendPasswordReset(user.id).unwrap();
      toast.success(t('adminUserProfile.authMethods.toastPasswordResetSent'));
    } catch (err) {
      toast.error(
        t('adminUserProfile.authMethods.toastPasswordResetFailed', {
          error: extractError(err, unknownErr)
        })
      );
    }
  };

  const handleResendVerification = async () => {
    try {
      await resendVerification(user.id).unwrap();
      toast.success(t('adminUserProfile.authMethods.toastVerificationResent'));
    } catch (err) {
      toast.error(
        t('adminUserProfile.authMethods.toastVerificationResendFailed', {
          error: extractError(err, unknownErr)
        })
      );
    }
  };

  const handleUnlink = async (provider: OAuthProviderName) => {
    try {
      await unlinkOAuth({ id: user.id, provider }).unwrap();
      toast.success(
        t('adminUserProfile.authMethods.toastUnlinked', { provider })
      );
      setConfirmUnlink(null);
    } catch (err) {
      // Surface the typed last-credential / self-action codes if the
      // server pushes back; otherwise fall through to the generic msg.
      const code = (err as { data?: { type?: string; title?: string } })?.data
        ?.title;
      if (code === 'last_credential') {
        toast.error(
          t('adminUserProfile.authMethods.toastUnlinkLastCredential')
        );
      } else if (code === 'self_action') {
        toast.error(t('adminUserProfile.authMethods.toastUnlinkSelfAction'));
      } else {
        toast.error(
          t('adminUserProfile.authMethods.toastUnlinkFailed', {
            error: extractError(err, unknownErr)
          })
        );
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
            {t('adminUserProfile.authMethods.cardTitle')}
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
            {t('adminUserProfile.authMethods.cardTitle')}
          </h5>
        </Card.Header>
        <Card.Body>
          <Alert variant="warning" className="mb-0">
            {t('adminUserProfile.authMethods.loadError')}{' '}
            <Button variant="link" className="p-0" onClick={() => refetch()}>
              {t('adminUserProfile.authMethods.retry')}
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
            {t('adminUserProfile.authMethods.cardTitle')}
          </h5>
        </Card.Header>
        <Card.Body>
          {/* Password */}
          <Section
            icon="key"
            title={t('adminUserProfile.authMethods.passwordTitle')}
            badge={
              data.hasUsablePassword ? (
                <Badge bg="success">
                  {t('adminUserProfile.authMethods.passwordBadgeSet')}
                </Badge>
              ) : (
                <Badge bg="secondary">
                  {t('adminUserProfile.authMethods.passwordBadgeNotSet')}
                </Badge>
              )
            }
            sub={
              data.hasUsablePassword && data.passwordUpdatedAt
                ? t('adminUserProfile.authMethods.passwordLastChanged', {
                    date: formatDate(data.passwordUpdatedAt)
                  })
                : t('adminUserProfile.authMethods.passwordOauthOnly')
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
                  t('adminUserProfile.authMethods.passwordSendResetButton')
                )}
              </Button>
            }
          />

          {/* Email verification */}
          <Section
            icon="envelope"
            title={t('adminUserProfile.authMethods.emailTitle')}
            badge={
              data.emailVerified ? (
                <Badge bg="success">
                  {t('adminUserProfile.authMethods.emailBadgeVerified')}
                </Badge>
              ) : (
                <Badge bg="warning" text="dark">
                  {t('adminUserProfile.authMethods.emailBadgeUnverified')}
                </Badge>
              )
            }
            sub={
              data.emailVerified
                ? t('adminUserProfile.authMethods.emailConfirmed')
                : t('adminUserProfile.authMethods.emailNotConfirmed')
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
                    t('adminUserProfile.authMethods.emailResendButton')
                  )}
                </Button>
              ) : null
            }
          />

          {/* MFA */}
          <Section
            icon="lock"
            title={t('adminUserProfile.authMethods.mfaTitle')}
            badge={
              data.mfaRequired ? (
                <Badge bg="primary">
                  {t('adminUserProfile.authMethods.mfaBadgeRequired')}
                </Badge>
              ) : (
                <Badge bg="secondary">
                  {t('adminUserProfile.authMethods.mfaBadgeOptional')}
                </Badge>
              )
            }
            sub={describeMfa(data, t)}
            action={
              data.mfaFactors.length > 0 && !isSelf ? (
                <Button
                  variant="outline-warning"
                  size="sm"
                  onClick={() => setShowMfaModal(true)}
                >
                  {t('adminUserProfile.authMethods.mfaResetButton')}
                </Button>
              ) : data.mfaFactors.length > 0 && isSelf ? (
                <OverlayTrigger
                  placement="left"
                  overlay={
                    <Tooltip>
                      {t('adminUserProfile.authMethods.mfaResetSelfTooltip')}
                    </Tooltip>
                  }
                >
                  <span className="d-inline-block">
                    <Button variant="outline-warning" size="sm" disabled>
                      {t('adminUserProfile.authMethods.mfaResetButton')}
                    </Button>
                  </span>
                </OverlayTrigger>
              ) : null
            }
          >
            {data.mfaFactors.length > 0 && (
              <ul className="list-unstyled mb-0 mt-2 ms-4 small text-body-secondary">
                {data.mfaFactors.map(f => (
                  <li key={f.type}>{describeFactor(f, t)}</li>
                ))}
              </ul>
            )}
          </Section>

          {/* OAuth providers */}
          <div className="pt-3">
            <div className="d-flex align-items-center mb-2">
              <FontAwesomeIcon icon="link" className="me-2 text-700" />
              <strong>{t('adminUserProfile.authMethods.oauthHeading')}</strong>
              <span className="ms-2 text-body-secondary small">
                {t('adminUserProfile.authMethods.oauthCount', {
                  count: data.oauthProviders.length
                })}
              </span>
            </div>
            {data.oauthProviders.length === 0 ? (
              <p className="small text-body-secondary mb-0 ms-4">
                {t('adminUserProfile.authMethods.oauthEmpty')}
              </p>
            ) : (
              <ul className="list-unstyled mb-0 ms-4">
                {data.oauthProviders.map(p => {
                  const onlyCredential =
                    !data.hasUsablePassword && data.oauthProviders.length === 1;
                  const blockReason = isSelf
                    ? t('adminUserProfile.authMethods.oauthBlockSelf')
                    : onlyCredential
                      ? t(
                          'adminUserProfile.authMethods.oauthBlockOnlyCredential'
                        )
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
                            {t(
                              'adminUserProfile.authMethods.oauthPrimaryBadge'
                            )}
                          </Badge>
                        )}
                        <span className="ms-2 small text-body-secondary">
                          {t('adminUserProfile.authMethods.oauthLinkedAt', {
                            date: formatDate(p.linkedAt)
                          })}
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
          <strong>
            {t('adminUserProfile.authMethods.unlinkConfirmTitle', {
              provider: confirmUnlink.provider
            })}
          </strong>
          <p className="mb-2 small">
            {t('adminUserProfile.authMethods.unlinkConfirmBody')}
          </p>
          <div className="d-flex gap-2 justify-content-end">
            <Button
              size="sm"
              variant="outline-secondary"
              onClick={() => setConfirmUnlink(null)}
              disabled={unlinkBusy}
            >
              {t('adminUserProfile.authMethods.unlinkConfirmCancel')}
            </Button>
            <Button
              size="sm"
              variant="warning"
              onClick={() => handleUnlink(confirmUnlink.provider)}
              disabled={unlinkBusy}
            >
              {unlinkBusy ? (
                <Spinner size="sm" animation="border" />
              ) : (
                t('adminUserProfile.authMethods.unlinkConfirmSubmit')
              )}
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
  const { t } = useTranslation();
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
        aria-label={t('adminUserProfile.authMethods.oauthActionsAria', {
          provider: provider.provider
        })}
      >
        <FontAwesomeIcon icon="ellipsis-h" />
      </Dropdown.Toggle>
      <Dropdown.Menu className="border py-0">
        <Dropdown.Item onClick={onUnlinkClick} className="text-warning">
          <FontAwesomeIcon icon="unlink" className="me-2" />
          {t('adminUserProfile.authMethods.oauthUnlinkMenuItem')}
        </Dropdown.Item>
      </Dropdown.Menu>
    </Dropdown>
  );
};

function describeMfa(data: AdminAuthMethods, t: TFunction): string {
  if (data.mfaFactors.length === 0) {
    if (data.mfaRequired) {
      if (data.mfaGraceExpiresAt) {
        return t('adminUserProfile.authMethods.mfaRequiredGrace', {
          date: formatDate(data.mfaGraceExpiresAt)
        });
      }
      return t('adminUserProfile.authMethods.mfaRequiredNoFactor');
    }
    return t('adminUserProfile.authMethods.mfaNotEnrolled');
  }
  return t(
    data.mfaFactors.length === 1
      ? 'adminUserProfile.authMethods.mfaFactorsCountOne'
      : 'adminUserProfile.authMethods.mfaFactorsCountOther',
    { count: data.mfaFactors.length }
  );
}

function describeFactor(f: AdminAuthMfaFactor, t: TFunction): string {
  if (f.type === 'totp') {
    const enrolled = f.enrolledAt
      ? formatDate(f.enrolledAt)
      : t('adminUserProfile.authMethods.mfaFactorTotpEnrolledUnknown');
    const codes =
      typeof f.backupCodesRemaining === 'number'
        ? t(
            f.backupCodesRemaining === 1
              ? 'adminUserProfile.authMethods.mfaFactorTotpBackupCodesOne'
              : 'adminUserProfile.authMethods.mfaFactorTotpBackupCodesOther',
            { count: f.backupCodesRemaining }
          )
        : '';
    return (
      t('adminUserProfile.authMethods.mfaFactorTotpEnrolled', {
        date: enrolled
      }) + codes
    );
  }
  if (f.type === 'webauthn') {
    const n = f.credentials?.length ?? 0;
    const names = (f.credentials ?? [])
      .map(c => c.name)
      .filter(Boolean)
      .join(', ');
    const count = t(
      n === 1
        ? 'adminUserProfile.authMethods.mfaFactorWebauthnCountOne'
        : 'adminUserProfile.authMethods.mfaFactorWebauthnCountOther',
      { count: n }
    );
    const suffix = names
      ? t('adminUserProfile.authMethods.mfaFactorWebauthnNamesSuffix', {
          names
        })
      : '';
    return count + suffix;
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

function extractError(err: unknown, unknownLabel: string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || unknownLabel;
  }
  return String(err);
}

export default AdminAuthMethodsCard;

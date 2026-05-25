import { useEffect, useState } from 'react';
import { Badge, Button, Card, Col, Form, Row, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import {
  useGetCurrentUserQuery,
  useUpdateCurrentUserMutation
} from 'store/api/authApi';

// ProfileSettings — self-service profile card on /user/settings.
//
// Backend whitelist for PATCH /v1/auth/operator/me is intentionally
// narrow (fullName + language only). Everything else on this card is
// read-only by design:
//
//   - email / username / role are identity-shaped and changing them
//     requires admin or a dedicated re-verification flow that doesn't
//     exist yet (tracked in [[project_user_security_center]])
//   - createdAt / lastLogin are informational
//
// Dirty-state: the Save button stays disabled until fullName diverges
// from the current user's value, and re-disables after a successful
// save. On failure we revert the form to the cached value and toast.

const MAX_FULL_NAME = 100;

const ProfileSettings: React.FC = () => {
  const { t, i18n } = useTranslation();
  const { data: user, isLoading } = useGetCurrentUserQuery();
  const [updateCurrentUser, { isLoading: isSaving }] =
    useUpdateCurrentUserMutation();

  const [fullName, setFullName] = useState('');

  // Re-sync when the cached user changes (initial load, language switch
  // mutating the cache, avatar mutations refreshing /me, etc.).
  useEffect(() => {
    setFullName(user?.fullName ?? '');
  }, [user?.fullName]);

  const trimmed = fullName.trim();
  const initial = (user?.fullName ?? '').trim();
  const isDirty = trimmed !== initial;
  const isInvalid =
    isDirty && (trimmed.length < 1 || trimmed.length > MAX_FULL_NAME);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!isDirty || isInvalid) return;
    try {
      await updateCurrentUser({ fullName: trimmed }).unwrap();
      toast.success(t('settings.profile.toastSaved'));
    } catch (err) {
      setFullName(initial);
      const detail = (err as { data?: { detail?: string } })?.data?.detail;
      toast.error(detail ?? t('settings.profile.toastFailed'));
    }
  };

  const lastLogin = user?.lastLogin
    ? new Date(user.lastLogin).toLocaleString(i18n.language)
    : '—';
  const createdAt = user?.createdAt
    ? new Date(user.createdAt).toLocaleDateString(i18n.language, {
        year: 'numeric',
        month: 'long',
        day: 'numeric'
      })
    : '—';

  return (
    <Card className="mb-3">
      <OrkestraCardHeader title={t('settings.profile.title')} />
      <Card.Body className="bg-body-tertiary">
        <p className="fs-9 text-muted mb-3">
          {t('settings.profile.description')}
        </p>

        <Form onSubmit={handleSubmit}>
          <Row className="g-3 mb-3">
            <Form.Group as={Col} lg={12} controlId="profile-full-name">
              <Form.Label className="fs-9 fw-semibold">
                {t('settings.profile.fullNameLabel')}
              </Form.Label>
              <Form.Control
                type="text"
                value={fullName}
                onChange={e => setFullName(e.target.value)}
                placeholder={t('settings.profile.fullNamePlaceholder')}
                maxLength={MAX_FULL_NAME}
                disabled={isLoading || isSaving}
                isInvalid={isInvalid}
                autoComplete="name"
              />
              <Form.Control.Feedback type="invalid">
                {t('settings.profile.fullNameInvalid')}
              </Form.Control.Feedback>
            </Form.Group>
          </Row>

          <Row className="g-3 mb-3">
            <Form.Group as={Col} md={6} controlId="profile-email">
              <Form.Label className="fs-9 fw-semibold d-flex align-items-center gap-2">
                {t('settings.profile.emailLabel')}
                {user?.emailVerified ? (
                  <Badge bg="success-subtle" text="success" pill>
                    {t('settings.profile.emailVerified')}
                  </Badge>
                ) : (
                  <Badge bg="warning-subtle" text="warning" pill>
                    {t('settings.profile.emailUnverified')}
                  </Badge>
                )}
              </Form.Label>
              <Form.Control
                type="email"
                value={user?.email ?? ''}
                readOnly
                plaintext={false}
                disabled
              />
            </Form.Group>

            <Form.Group as={Col} md={6} controlId="profile-username">
              <Form.Label className="fs-9 fw-semibold">
                {t('settings.profile.usernameLabel')}
              </Form.Label>
              <Form.Control
                type="text"
                value={user?.username ?? ''}
                readOnly
                disabled
              />
            </Form.Group>
          </Row>

          <Row className="g-3 mb-3">
            <Form.Group as={Col} md={6} controlId="profile-role">
              <Form.Label className="fs-9 fw-semibold">
                {t('settings.profile.roleLabel')}
              </Form.Label>
              <div>
                <Badge
                  bg="primary-subtle"
                  text="primary"
                  className="fs-10 px-2 py-2"
                >
                  {t(`adminUsers.roles.${user?.role ?? 'guest'}`, {
                    defaultValue: user?.role ?? '—'
                  })}
                </Badge>
              </div>
            </Form.Group>

            <Form.Group as={Col} md={6}>
              <Form.Label className="fs-9 fw-semibold">
                {t('settings.profile.memberSinceLabel')}
              </Form.Label>
              <div className="fs-9">{createdAt}</div>
            </Form.Group>
          </Row>

          <Row className="g-3 mb-3">
            <Col md={12}>
              <div className="fs-10 text-muted">
                {t('settings.profile.lastLoginLabel')}: {lastLogin}
              </div>
            </Col>
          </Row>

          <div className="text-end">
            <Button
              variant="primary"
              type="submit"
              disabled={!isDirty || isInvalid || isSaving || isLoading}
            >
              {isSaving && (
                <Spinner animation="border" size="sm" className="me-2" />
              )}
              {t('settings.profile.submit')}
            </Button>
          </div>
        </Form>
      </Card.Body>
    </Card>
  );
};

export default ProfileSettings;

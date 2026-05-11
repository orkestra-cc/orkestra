import { useState, FormEvent } from 'react';
import { Alert, Button, Card, Form, Spinner } from 'react-bootstrap';
import {
  useChangePasswordMutation,
  useGetAuthPolicyQuery,
} from 'store/api/authApi';
import { useGetSelfAuthMethodsQuery } from 'store/api/authApi';

// PasswordTab implements the self-service password-change flow that
// the legacy /user/settings::ChangePassword card stubbed out. Wired
// to the existing /v1/auth/operator/change-password mutation; the
// backend enforces the current admin-managed password policy
// (min/max length, complexity, HIBP) — we display the minimum length
// up-front so the user knows what they're targeting.
const PasswordTab = () => {
  const { data: policy } = useGetAuthPolicyQuery();
  const { data: authMethods } = useGetSelfAuthMethodsQuery();
  const [changePassword, { isLoading }] = useChangePasswordMutation();

  const [oldPassword, setOldPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [success, setSuccess] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const minLength = policy?.passwordMinLength ?? 10;
  const hasPassword = authMethods?.hasUsablePassword ?? true;

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccess(null);
    if (newPassword !== confirmPassword) {
      setError('New password and confirmation do not match.');
      return;
    }
    if (newPassword.length < minLength) {
      setError(`New password must be at least ${minLength} characters.`);
      return;
    }
    try {
      await changePassword({
        currentPassword: oldPassword,
        newPassword,
      }).unwrap();
      setSuccess('Password updated. Other sessions have been signed out.');
      setOldPassword('');
      setNewPassword('');
      setConfirmPassword('');
    } catch (err: unknown) {
      const data = (err as { data?: { detail?: string; title?: string } })?.data;
      setError(data?.detail || data?.title || 'Failed to update password.');
    }
  };

  return (
    <Card className="shadow-none border">
      <Card.Header>
        <Card.Title as="h5" className="mb-0">
          Change password
        </Card.Title>
      </Card.Header>
      <Card.Body>
        {!hasPassword && (
          <Alert variant="info" className="fs-10">
            Your account uses a single sign-on provider only. Set a password
            here to add a second login method.
          </Alert>
        )}
        {success && (
          <Alert variant="success" className="fs-10">
            {success}
          </Alert>
        )}
        {error && (
          <Alert variant="danger" className="fs-10">
            {error}
          </Alert>
        )}
        <Form onSubmit={handleSubmit} noValidate>
          <Form.Group className="mb-3" controlId="self-old-password">
            <Form.Label>Current password</Form.Label>
            <Form.Control
              type="password"
              autoComplete="current-password"
              value={oldPassword}
              onChange={(e) => setOldPassword(e.target.value)}
              required={hasPassword}
              disabled={isLoading}
            />
          </Form.Group>
          <Form.Group className="mb-3" controlId="self-new-password">
            <Form.Label>New password</Form.Label>
            <Form.Control
              type="password"
              autoComplete="new-password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              required
              disabled={isLoading}
              minLength={minLength}
            />
            <Form.Text className="text-muted">
              At least {minLength} characters. Avoid passwords that have
              appeared in known breaches.
            </Form.Text>
          </Form.Group>
          <Form.Group className="mb-4" controlId="self-confirm-password">
            <Form.Label>Confirm new password</Form.Label>
            <Form.Control
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              required
              disabled={isLoading}
              minLength={minLength}
            />
          </Form.Group>
          <Button type="submit" disabled={isLoading}>
            {isLoading ? (
              <>
                <Spinner animation="border" size="sm" className="me-2" />
                Updating…
              </>
            ) : (
              'Update password'
            )}
          </Button>
        </Form>
      </Card.Body>
    </Card>
  );
};

export default PasswordTab;

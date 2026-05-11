import { useState, FormEvent } from 'react';
import { Alert, Button, Form } from 'react-bootstrap';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useResetPasswordMutation } from 'store/api/authApi';

const ResetPasswordForm = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const token = searchParams.get('token') || '';
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [localError, setLocalError] = useState<string | null>(null);
  const [resetPassword, { isLoading, isSuccess }] = useResetPasswordMutation();

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setLocalError(null);

    if (!token) {
      setLocalError('Missing reset token. Please use the link from your email.');
      return;
    }
    if (password.length < 10) {
      setLocalError('Password must be at least 10 characters.');
      return;
    }
    if (password !== confirm) {
      setLocalError('Passwords do not match.');
      return;
    }

    try {
      await resetPassword({ token, newPassword: password }).unwrap();
      setTimeout(() => navigate('/login?reset=1'), 1500);
    } catch (err: unknown) {
      const anyErr = err as { data?: { detail?: string }; status?: number };
      setLocalError(anyErr?.data?.detail || 'Unable to reset password. The link may have expired.');
    }
  };

  if (isSuccess) {
    return (
      <Alert variant="success">
        Password updated. Redirecting you to the sign-in page…
      </Alert>
    );
  }

  return (
    <Form onSubmit={handleSubmit}>
      {localError && (
        <Alert variant="danger" className="mb-3" dismissible onClose={() => setLocalError(null)}>
          {localError}
        </Alert>
      )}

      <p className="text-muted mb-4">Pick a new password for your account.</p>

      <Form.Group className="mb-3">
        <Form.Label>New password</Form.Label>
        <Form.Control
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="new-password"
          minLength={10}
          required
        />
        <Form.Text className="text-muted">Use at least 10 characters.</Form.Text>
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label>Confirm new password</Form.Label>
        <Form.Control
          type="password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          autoComplete="new-password"
          required
        />
      </Form.Group>

      <div className="d-grid mb-3">
        <Button type="submit" variant="primary" size="lg" disabled={isLoading}>
          {isLoading ? 'Saving…' : 'Update password'}
        </Button>
      </div>

      <div className="text-center">
        <small className="text-muted">
          <Link to="/login">Back to sign in</Link>
        </small>
      </div>
    </Form>
  );
};

export default ResetPasswordForm;

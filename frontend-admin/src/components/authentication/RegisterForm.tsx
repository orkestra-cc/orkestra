import { useState, FormEvent } from 'react';
import { Alert, Button, Form } from 'react-bootstrap';
import { Link, useNavigate } from 'react-router-dom';
import { useGetAuthPolicyQuery, useRegisterMutation } from 'store/api/authApi';

const RegisterForm = () => {
  const navigate = useNavigate();
  const [register, { isLoading }] = useRegisterMutation();
  const [fullName, setFullName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [accepted, setAccepted] = useState(false);
  const [localError, setLocalError] = useState<string | null>(null);
  // Read the admin-managed signup policy. registrationEnabled drives
  // the kill-switch banner + submit gating; passwordMinLength replaces
  // the hardcoded 10-char check so the form stays in sync with what the
  // backend will actually accept.
  const { data: policy } = useGetAuthPolicyQuery();
  const registrationEnabled = policy?.registrationEnabled ?? true;
  const passwordMinLength = policy?.passwordMinLength ?? 10;

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setLocalError(null);

    if (!fullName || !email || !password) {
      setLocalError('Please fill in all required fields.');
      return;
    }
    if (password.length < passwordMinLength) {
      setLocalError(`Password must be at least ${passwordMinLength} characters.`);
      return;
    }
    if (password !== confirm) {
      setLocalError('Passwords do not match.');
      return;
    }
    if (!accepted) {
      setLocalError('You must accept the terms to continue.');
      return;
    }

    try {
      const result = await register({ email, password, fullName }).unwrap();
      if (result.requiresVerification) {
        navigate(`/verify-email?pending=${encodeURIComponent(email)}`);
      } else {
        navigate('/login?registered=1');
      }
    } catch (err: unknown) {
      const anyErr = err as { data?: { detail?: string }; status?: number };
      if (anyErr?.status === 503) {
        setLocalError(
          'Sign-up is temporarily unavailable because email delivery is not configured. Please contact an administrator.',
        );
      } else {
        setLocalError(anyErr?.data?.detail || 'Unable to create account. Please try again.');
      }
    }
  };

  return (
    <Form onSubmit={handleSubmit}>
      {!registrationEnabled && (
        <Alert variant="warning" className="mb-3">
          Self-service registration is currently disabled. Please contact an administrator
          if you need an account.
        </Alert>
      )}
      {localError && (
        <Alert variant="danger" className="mb-3" dismissible onClose={() => setLocalError(null)}>
          {localError}
        </Alert>
      )}

      <Form.Group className="mb-3">
        <Form.Label>Full name</Form.Label>
        <Form.Control
          type="text"
          value={fullName}
          onChange={(e) => setFullName(e.target.value)}
          autoComplete="name"
          required
        />
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label>Email</Form.Label>
        <Form.Control
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          autoComplete="email"
          required
        />
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label>Password</Form.Label>
        <Form.Control
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="new-password"
          minLength={passwordMinLength}
          required
        />
        <Form.Text className="text-muted">Use at least {passwordMinLength} characters.</Form.Text>
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label>Confirm password</Form.Label>
        <Form.Control
          type="password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          autoComplete="new-password"
          required
        />
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Check
          type="checkbox"
          id="accept-terms"
          checked={accepted}
          onChange={(e) => setAccepted(e.target.checked)}
          label={
            <span className="fs--1">
              I accept the <Link to="/terms">terms of service</Link> and{' '}
              <Link to="/privacy">privacy policy</Link>
            </span>
          }
        />
      </Form.Group>

      <div className="d-grid mb-3">
        <Button type="submit" variant="primary" size="lg" disabled={isLoading || !registrationEnabled}>
          {isLoading ? 'Creating account…' : 'Create account'}
        </Button>
      </div>

      <div className="text-center">
        <small className="text-muted">
          Already have an account? <Link to="/login">Sign in</Link>
        </small>
      </div>
    </Form>
  );
};

export default RegisterForm;

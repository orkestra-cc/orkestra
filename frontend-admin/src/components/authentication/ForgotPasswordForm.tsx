import { useState, FormEvent } from 'react';
import { Alert, Button, Form } from 'react-bootstrap';
import { Link } from 'react-router-dom';
import { useForgotPasswordMutation } from 'store/api/authApi';

const ForgotPasswordForm = () => {
  const [email, setEmail] = useState('');
  const [submitted, setSubmitted] = useState(false);
  const [forgotPassword, { isLoading }] = useForgotPasswordMutation();

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    if (!email) return;
    try {
      await forgotPassword({ email }).unwrap();
    } catch {
      // Generic error to prevent enumeration — always show success.
    }
    setSubmitted(true);
  };

  if (submitted) {
    return (
      <>
        <Alert variant="success" className="mb-3">
          If an account with that email exists, a password reset email has been sent. Please check
          your inbox.
        </Alert>
        <div className="text-center">
          <Link to="/login">Back to sign in</Link>
        </div>
      </>
    );
  }

  return (
    <Form onSubmit={handleSubmit}>
      <p className="text-muted mb-4">
        Enter your email address and we&apos;ll send you a link to reset your password.
      </p>
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

      <div className="d-grid mb-3">
        <Button type="submit" variant="primary" size="lg" disabled={isLoading}>
          {isLoading ? 'Sending…' : 'Send reset link'}
        </Button>
      </div>

      <div className="text-center">
        <small className="text-muted">
          Remembered your password? <Link to="/login">Sign in</Link>
        </small>
      </div>
    </Form>
  );
};

export default ForgotPasswordForm;

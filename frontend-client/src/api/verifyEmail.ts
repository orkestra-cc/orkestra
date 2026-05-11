// Email verification flow — anonymous endpoints on the client surface
// (ADR-0003 PR-D D-5). Shape mirrors VerifyEmailRequest /
// VerifyEmailResponse in backend/internal/core/auth/handlers/
// password_handler.go.
import { apiBaseURL } from '@/api/client';

export interface VerifyEmailResult {
  success: boolean;
  message: string;
}

export async function verifyEmailToken(token: string): Promise<VerifyEmailResult> {
  const res = await fetch(`${apiBaseURL}/v1/auth/client/verify-email`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
    },
    body: JSON.stringify({ token }),
  });
  if (!res.ok) {
    // Backend returns 400 for any invalid/expired token. Surface a single
    // generic message — discriminating subtypes here would only help an
    // attacker enumerate token lifetimes.
    throw new Error('invalid_or_expired_token');
  }
  return (await res.json()) as VerifyEmailResult;
}

// Resend deliberately returns 200 even when the email is unknown — the
// backend message ("if an account exists…") preserves enumeration
// resistance. The SPA mirrors that and shows a neutral confirmation.
export async function resendVerificationEmail(email: string): Promise<VerifyEmailResult> {
  const res = await fetch(`${apiBaseURL}/v1/auth/client/verify-email/resend`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
    },
    body: JSON.stringify({ email }),
  });
  if (!res.ok) {
    throw new Error(`resend_failed_${res.status}`);
  }
  return (await res.json()) as VerifyEmailResult;
}

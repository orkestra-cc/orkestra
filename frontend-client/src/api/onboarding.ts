// Anonymous self-service signup. Wraps POST /v1/onboarding/register —
// shape mirrors backend/internal/addons/onboarding/handlers/handler.go
// (OnboardingRegisterBody / OnboardingRegisterResultBody).
import { apiBaseURL } from '@/api/client';

export interface OnboardingRegisterInput {
  email: string;
  password: string;
  fullName: string;
  tenantName: string;
  // Optional — backend derives a slug from tenantName when empty.
  tenantSlug?: string;
  // Optional informational label; entitlements come from subscriptions.
  plan?: string;
}

export interface OnboardingRegisterResult {
  success: boolean;
  userUuid: string;
  tenantUuid: string;
  tenantSlug: string;
  message: string;
  requiresVerification: boolean;
}

export interface OnboardingError {
  status: number;
  message: string;
}

export async function registerTenant(
  input: OnboardingRegisterInput,
): Promise<OnboardingRegisterResult> {
  const res = await fetch(`${apiBaseURL}/v1/onboarding/register`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
    },
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { detail?: string; title?: string };
    const err: OnboardingError = {
      status: res.status,
      message: body.detail ?? body.title ?? `Registration failed (${res.status})`,
    };
    throw err;
  }
  return (await res.json()) as OnboardingRegisterResult;
}

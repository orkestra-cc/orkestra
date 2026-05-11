// Authenticated client-tier auth surface. Wraps the per-tier paths
// /v1/auth/client/{login, me, change-password, forgot-password,
// reset-password, mfa/...}. Hand-typed against the backend handlers
// in backend/internal/core/auth/handlers/{password,mfa,auth}_handler.go;
// codegen can sharpen later.
//
// Authenticated calls use authedFetch which pulls the in-memory access
// token from src/auth/tokenStore so AuthProvider doesn't need to wire
// fetch headers manually. credentials:'include' is set on every call so
// the httpOnly refresh cookie is attached cross-origin (Domain-scoped to
// the API host per ADR-0003 D-9).
import { apiBaseURL } from '@/api/client';
import { getAccessToken } from '@/auth/tokenStore';

interface ApiError extends Error {
  status: number;
  code?: string;
}

function err(message: string, status: number, code?: string): ApiError {
  const e = new Error(message) as ApiError;
  e.status = status;
  if (code) e.code = code;
  return e;
}

async function readError(res: Response, fallback: string): Promise<ApiError> {
  const body = (await res.json().catch(() => ({}))) as {
    detail?: string;
    title?: string;
    code?: string;
  };
  return err(body.detail ?? body.title ?? fallback, res.status, body.code);
}

async function jsonFetch(path: string, init?: RequestInit): Promise<Response> {
  return fetch(`${apiBaseURL}${path}`, {
    credentials: 'include',
    ...init,
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
      ...(init?.headers ?? {}),
    },
  });
}

async function authedFetch(path: string, init?: RequestInit): Promise<Response> {
  const token = getAccessToken();
  return jsonFetch(path, {
    ...init,
    headers: {
      ...(init?.headers ?? {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
  });
}

// --- Public auth policy ---

export interface AuthPolicy {
  registrationEnabled: boolean;
  loginEnabled: boolean;
  passwordMinLength: number;
}

// fetchAuthPolicy reads the public policy slice the unauthenticated
// login + signup pages need so kill switches hide the CTA instead of
// surfacing as a raw 403 on submit. Falls open (everything enabled,
// legacy 10-char password floor) on network failure — the backend
// re-validates on submit anyway.
export async function fetchAuthPolicy(): Promise<AuthPolicy> {
  try {
    const res = await jsonFetch('/v1/auth/client/policy', { method: 'GET' });
    if (!res.ok) {
      return { registrationEnabled: true, loginEnabled: true, passwordMinLength: 10 };
    }
    return (await res.json()) as AuthPolicy;
  } catch {
    return { registrationEnabled: true, loginEnabled: true, passwordMinLength: 10 };
  }
}

// --- Register ---

export interface RegisterInput {
  email: string;
  password: string;
  fullName: string;
}

export interface RegisterResult {
  success: boolean;
  userUuid: string;
  message: string;
  requiresVerification: boolean;
}

export async function register(input: RegisterInput): Promise<RegisterResult> {
  const res = await jsonFetch('/v1/auth/client/register', {
    method: 'POST',
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw await readError(res, 'Registration failed');
  }
  return (await res.json()) as RegisterResult;
}

// --- Login ---

export interface LoginInput {
  email: string;
  password: string;
}

export interface LoginUser {
  id: string;
  email: string;
  fullName?: string;
  username?: string;
  avatar?: string;
  role?: string;
  emailVerified?: boolean;
  isActive?: boolean;
}

// Discriminated union — either a full token (success) or a partial
// MFA challenge that the SPA must complete via mfaLoginVerify.
export type LoginResult =
  | {
      kind: 'token';
      accessToken: string;
      tokenType: string;
      expiresIn: number;
      user?: LoginUser;
      mfaEnrollmentRequired?: boolean;
      mfaGraceExpiresAt?: string;
    }
  | {
      kind: 'mfa_required';
      mfaToken: string;
      webauthnAvailable: boolean;
      user?: LoginUser;
    };

interface LoginResponseBody {
  success: boolean;
  accessToken?: string;
  tokenType?: string;
  expiresIn?: number;
  user?: LoginUser;
  requiresMfa?: boolean;
  mfaToken?: string;
  webauthnAvailable?: boolean;
  mfaEnrollmentRequired?: boolean;
  mfaGraceExpiresAt?: string;
}

export async function login(input: LoginInput): Promise<LoginResult> {
  const res = await jsonFetch('/v1/auth/client/login', {
    method: 'POST',
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw await readError(res, 'Login failed');
  }
  const body = (await res.json()) as LoginResponseBody;
  if (body.requiresMfa && body.mfaToken) {
    return {
      kind: 'mfa_required',
      mfaToken: body.mfaToken,
      webauthnAvailable: !!body.webauthnAvailable,
      user: body.user,
    };
  }
  if (!body.accessToken) {
    throw err('Login response missing access token', 500);
  }
  return {
    kind: 'token',
    accessToken: body.accessToken,
    tokenType: body.tokenType ?? 'Bearer',
    expiresIn: body.expiresIn ?? 900,
    user: body.user,
    mfaEnrollmentRequired: body.mfaEnrollmentRequired,
    mfaGraceExpiresAt: body.mfaGraceExpiresAt,
  };
}

// --- MFA login verify (completes a partial login response) ---

export interface MfaLoginVerifyInput {
  challengeId: string;
  code: string;
  useBackup?: boolean;
  trustDevice?: boolean;
}

export interface MfaLoginVerifyResult {
  accessToken: string;
  tokenType: string;
  expiresIn: number;
  user?: LoginUser;
}

export async function mfaLoginVerify(input: MfaLoginVerifyInput): Promise<MfaLoginVerifyResult> {
  const res = await jsonFetch('/v1/auth/client/mfa/login/verify', {
    method: 'POST',
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw await readError(res, 'MFA verification failed');
  }
  const body = (await res.json()) as {
    accessToken: string;
    tokenType: string;
    expiresIn: number;
    user?: LoginUser;
  };
  return body;
}

// --- /me (authenticated) ---

export interface MeResponse {
  id: string;
  email: string;
  username?: string;
  fullName?: string;
  avatar?: string;
  role?: string;
  isActive?: boolean;
  emailVerified?: boolean;
  lastLogin?: string;
  oauthProviders?: Array<{ provider: string; providerId: string }>;
}

export async function getMe(signal?: AbortSignal): Promise<MeResponse> {
  const res = await authedFetch('/v1/auth/client/me', { method: 'GET', signal });
  if (!res.ok) throw await readError(res, 'Failed to load profile');
  return (await res.json()) as MeResponse;
}

// --- Password recovery ---

export async function forgotPassword(email: string): Promise<void> {
  // Always returns 200 (enumeration-resistant); the SPA shows a neutral
  // confirmation regardless of whether the email exists.
  const res = await jsonFetch('/v1/auth/client/forgot-password', {
    method: 'POST',
    body: JSON.stringify({ email }),
  });
  if (!res.ok) throw await readError(res, 'Request failed');
}

export async function resetPassword(token: string, newPassword: string): Promise<void> {
  const res = await jsonFetch('/v1/auth/client/reset-password', {
    method: 'POST',
    body: JSON.stringify({ token, newPassword }),
  });
  if (!res.ok) throw await readError(res, 'Password reset failed');
}

// acceptInvite redeems an admin_invite token: sets the user's password
// and marks the email verified server-side. Same shape as resetPassword
// but a different purpose claim — the backend rejects a reset token
// posted here and vice versa.
export async function acceptInvite(token: string, newPassword: string): Promise<void> {
  const res = await jsonFetch('/v1/auth/client/accept-invite', {
    method: 'POST',
    body: JSON.stringify({ token, newPassword }),
  });
  if (!res.ok) throw await readError(res, 'Invite redemption failed');
}

// --- Change password (authenticated) ---

export async function changePassword(
  currentPassword: string,
  newPassword: string,
): Promise<void> {
  const res = await authedFetch('/v1/auth/client/change-password', {
    method: 'POST',
    body: JSON.stringify({ currentPassword, newPassword }),
  });
  if (!res.ok) throw await readError(res, 'Password change failed');
  // Backend revokes the current session — caller must signOut + re-login.
}

// --- MFA management (authenticated) ---

export interface MfaStatus {
  status: string; // "not_required" | "enrolled" | "required" | etc.
  type?: string; // "totp"
  backupCodesRemaining: number;
  requiresMfa: boolean;
  graceExpiresAt?: string;
  webauthnCredentials: number;
}

export async function getMfaStatus(signal?: AbortSignal): Promise<MfaStatus> {
  const res = await authedFetch('/v1/auth/client/me/mfa', { method: 'GET', signal });
  if (!res.ok) throw await readError(res, 'Failed to load MFA status');
  return (await res.json()) as MfaStatus;
}

export interface MfaEnrollBegin {
  challengeId: string;
  secret: string;
  provisioningUri: string; // otpauth:// URI for QR rendering
}

export async function mfaEnrollBegin(): Promise<MfaEnrollBegin> {
  const res = await authedFetch('/v1/auth/client/mfa/enroll/begin', { method: 'POST' });
  if (!res.ok) throw await readError(res, 'MFA enrolment failed to start');
  return (await res.json()) as MfaEnrollBegin;
}

export interface MfaEnrollConfirm {
  success: boolean;
  backupCodes: string[];
}

export async function mfaEnrollConfirm(
  challengeId: string,
  code: string,
): Promise<MfaEnrollConfirm> {
  const res = await authedFetch('/v1/auth/client/mfa/enroll/confirm', {
    method: 'POST',
    body: JSON.stringify({ challengeId, code }),
  });
  if (!res.ok) throw await readError(res, 'MFA confirmation failed');
  return (await res.json()) as MfaEnrollConfirm;
}

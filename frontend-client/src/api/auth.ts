// Authenticated client-tier auth surface. Wraps the per-tier paths
// /v1/auth/client/{login, me, change-password, forgot-password,
// reset-password, mfa/...}. Hand-typed against the backend handlers
// in backend/internal/core/auth/handlers/{password,mfa,auth}_handler.go.
//
// Two request paths, and the split is DELIBERATE — do not "finish the job"
// by merging them:
//   * authedFetch (src/api/authedFetch.ts) is the shared helper every
//     bearer-carrying call in this SPA goes through. It attaches the token,
//     sets credentials:'include' (the httpOnly refresh cookie is Domain-scoped
//     to the API host per ADR-0003 D-9) and owns the ONLY 401 recovery.
//   * jsonFetch, below, is the ANONYMOUS path and gains nothing from that
//     recovery: a 401 from login, mfa/login/verify, register, forgot-password,
//     reset-password, accept-invite, policy, providers or oauth/login means
//     "those credentials are wrong" or "not signed in", never "the token
//     expired". There is no token to refresh and nothing safe to replay.
import { authedFetch } from "@/api/authedFetch";
import { apiBaseURL } from "@/api/client";
import { isOAuthProvider, type OAuthProviderName } from "@/lib/oauthProviders";
import { stashOAuthReturnTo } from "@/lib/oauthReturnTo";

// Exported: dsr.ts throws the same shape rather than keeping a private copy.
// Pages branch on `code`, so a second, drifting definition is a real hazard.
export interface ApiError extends Error {
  status: number;
  code?: string;
}

function err(message: string, status: number, code?: string): ApiError {
  const e = new Error(message) as ApiError;
  e.status = status;
  if (code) e.code = code;
  return e;
}

export async function readError(
  res: Response,
  fallback: string,
): Promise<ApiError> {
  const body = (await res.json().catch(() => ({}))) as {
    detail?: string;
    title?: string;
    code?: string;
  };
  return err(body.detail ?? body.title ?? fallback, res.status, body.code);
}

// apiErrorCode reads the stable backend `code` off an error thrown by this
// module (undefined for anything else). Pages branch on codes, never on
// localized detail strings.
export function apiErrorCode(e: unknown): string | undefined {
  if (!e || typeof e !== "object") return undefined;
  const code = (e as { code?: unknown }).code;
  return typeof code === "string" ? code : undefined;
}

async function jsonFetch(path: string, init?: RequestInit): Promise<Response> {
  return fetch(`${apiBaseURL}${path}`, {
    credentials: "include",
    ...init,
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
      ...(init?.headers ?? {}),
    },
  });
}

// --- Public auth policy ---

export interface AuthPolicy {
  registrationEnabled: boolean;
  loginEnabled: boolean;
  passwordMinLength: number;
  // PR 3 (spec §4.9): the persisted per-surface email/password policy.
  // The wire type is nullable (null only in the operator emergency case,
  // never produced on this tier) and null must read as "off" (§4.10), so
  // the type says so and passwordLoginUsable() is the only reader.
  passwordLoginEnabled: boolean | null;
  // Always false on the client endpoint; carried so the type mirrors the
  // wire shape and no reader is tempted to fake it.
  passwordLoginBreakGlassEffective: boolean;
}

// The fail-open display fallback (spec §4.10, §5 #15): everything enabled,
// legacy 10-char password floor. The backend re-validates on submit.
const FAIL_OPEN_POLICY: AuthPolicy = {
  registrationEnabled: true,
  loginEnabled: true,
  passwordMinLength: 10,
  passwordLoginEnabled: true,
  passwordLoginBreakGlassEffective: false,
};

// fetchAuthPolicy reads the public policy slice the unauthenticated
// login + signup pages need so kill switches hide the CTA instead of
// surfacing as a raw 403 on submit. Falls open on a non-2xx or a network
// failure. A 2xx body is spread over the fallback so a key an older
// backend omits reads as enabled while a present `null` stays null.
export async function fetchAuthPolicy(): Promise<AuthPolicy> {
  try {
    const res = await jsonFetch("/v1/auth/client/policy", { method: "GET" });
    if (!res.ok) return { ...FAIL_OPEN_POLICY };
    const body = (await res.json()) as Partial<AuthPolicy>;
    return { ...FAIL_OPEN_POLICY, ...body };
  } catch {
    return { ...FAIL_OPEN_POLICY };
  }
}

// passwordLoginUsable is the ONE reader of passwordLoginEnabled. An
// undefined policy (query still pending, or a caller without the query)
// reads as usable — the fail-open display default; a loaded policy must
// say exactly `true`: `null` means the persisted state is unknown and the
// SPA treats it as off (§4.10 "when persisted false/null"). The backend
// refuses regardless of what renders.
export function passwordLoginUsable(policy: AuthPolicy | undefined): boolean {
  if (policy === undefined) return true;
  return policy.passwordLoginEnabled === true;
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
  const res = await jsonFetch("/v1/auth/client/register", {
    method: "POST",
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw await readError(res, "Registration failed");
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
      kind: "token";
      accessToken: string;
      tokenType: string;
      // OPTIONAL, and deliberately so: see the mapping below.
      expiresIn?: number;
      user?: LoginUser;
      mfaEnrollmentRequired?: boolean;
      mfaGraceExpiresAt?: string;
    }
  | {
      kind: "mfa_required";
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
  const res = await jsonFetch("/v1/auth/client/login", {
    method: "POST",
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw await readError(res, "Login failed");
  }
  const body = (await res.json()) as LoginResponseBody;
  if (body.requiresMfa && body.mfaToken) {
    return {
      kind: "mfa_required",
      mfaToken: body.mfaToken,
      webauthnAvailable: !!body.webauthnAvailable,
      user: body.user,
    };
  }
  if (!body.accessToken) {
    throw err("Login response missing access token", 500);
  }
  return {
    kind: "token",
    accessToken: body.accessToken,
    tokenType: body.tokenType ?? "Bearer",
    // `expiresIn` is OPTIONAL on the wire. It used to default to 900, which
    // FABRICATES a fifteen-minute lifetime the server never promised: on a
    // deployment running a 60s TTL the store would then read every 401 as
    // "not a token problem" for the rest of that quarter hour. An unknown
    // lifetime is a fact the store knows how to handle (§4.5's fallback
    // chain); a wrong one is not.
    expiresIn: body.expiresIn,
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
  // Optional for the same reason as LoginResult's: an absent lifetime must
  // reach the store as "unknown", never as an invented number.
  expiresIn?: number;
  user?: LoginUser;
}

export async function mfaLoginVerify(
  input: MfaLoginVerifyInput,
): Promise<MfaLoginVerifyResult> {
  const res = await jsonFetch("/v1/auth/client/mfa/login/verify", {
    method: "POST",
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw await readError(res, "MFA verification failed");
  }
  const body = (await res.json()) as {
    accessToken: string;
    tokenType: string;
    expiresIn?: number;
    user?: LoginUser;
  };
  return body;
}

// --- /me (authenticated) ---

// AvatarSource mirrors the backend iface.AvatarSource* enum. Drives
// the SPA's render choice: "uploaded" + non-empty avatar → presigned
// GET URL, "oauth_*" → the matching linked-provider picture (also
// pre-resolved into `avatar` server-side), "initials" → empty avatar,
// SPA renders initials over a deterministic color.
export type AvatarSource =
  | "initials"
  | "uploaded"
  | "oauth_google"
  | "oauth_apple"
  | "oauth_github"
  | "oauth_discord";

export interface MeOAuthProvider {
  provider: string;
  providerId: string;
  email?: string;
  isPrimary?: boolean;
  metadata?: Record<string, unknown>;
  scopes?: string[];
}

export interface MeResponse {
  id: string;
  email: string;
  username?: string;
  fullName?: string;
  avatar?: string;
  // Server-resolved preference; drives the avatar settings UI and the
  // "currently showing" label on /account/profile.
  avatarSource?: AvatarSource;
  role?: string;
  isActive?: boolean;
  emailVerified?: boolean;
  lastLogin?: string;
  oauthProviders?: MeOAuthProvider[];
}

export async function getMe(signal?: AbortSignal): Promise<MeResponse> {
  const res = await authedFetch("/v1/auth/client/me", {
    method: "GET",
    signal,
  });
  if (!res.ok) throw await readError(res, "Failed to load profile");
  return (await res.json()) as MeResponse;
}

// --- Password recovery ---

export async function forgotPassword(email: string): Promise<void> {
  // 200 for every account outcome (enumeration-resistant) — the SPA shows
  // a neutral confirmation whether or not the email exists. The only
  // errors are the per-surface policy answers evaluated BEFORE the lookup
  // (spec §4.3): 403 auth.password_login_disabled and 503
  // auth.policy_unavailable; the page maps them by code.
  const res = await jsonFetch("/v1/auth/client/forgot-password", {
    method: "POST",
    body: JSON.stringify({ email }),
  });
  if (!res.ok) throw await readError(res, "Request failed");
}

export async function resetPassword(
  token: string,
  newPassword: string,
): Promise<void> {
  const res = await jsonFetch("/v1/auth/client/reset-password", {
    method: "POST",
    body: JSON.stringify({ token, newPassword }),
  });
  if (!res.ok) throw await readError(res, "Password reset failed");
}

// acceptInvite redeems an admin_invite token: sets the user's password
// and marks the email verified server-side. Same shape as resetPassword
// but a different purpose claim — the backend rejects a reset token
// posted here and vice versa.
export async function acceptInvite(
  token: string,
  newPassword: string,
): Promise<void> {
  const res = await jsonFetch("/v1/auth/client/accept-invite", {
    method: "POST",
    body: JSON.stringify({ token, newPassword }),
  });
  if (!res.ok) throw await readError(res, "Invite redemption failed");
}

// --- Change password (authenticated) ---

export async function changePassword(
  currentPassword: string,
  newPassword: string,
): Promise<void> {
  const res = await authedFetch("/v1/auth/client/change-password", {
    method: "POST",
    body: JSON.stringify({ currentPassword, newPassword }),
  });
  if (!res.ok) throw await readError(res, "Password change failed");
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
  const res = await authedFetch("/v1/auth/client/me/mfa", {
    method: "GET",
    signal,
  });
  if (!res.ok) throw await readError(res, "Failed to load MFA status");
  return (await res.json()) as MfaStatus;
}

export interface MfaEnrollBegin {
  challengeId: string;
  secret: string;
  provisioningUri: string; // otpauth:// URI for QR rendering
}

export async function mfaEnrollBegin(): Promise<MfaEnrollBegin> {
  const res = await authedFetch("/v1/auth/client/mfa/enroll/begin", {
    method: "POST",
  });
  if (!res.ok) throw await readError(res, "MFA enrolment failed to start");
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
  const res = await authedFetch("/v1/auth/client/mfa/enroll/confirm", {
    method: "POST",
    body: JSON.stringify({ challengeId, code }),
  });
  if (!res.ok) throw await readError(res, "MFA confirmation failed");
  return (await res.json()) as MfaEnrollConfirm;
}

// --- Web OAuth login (spec §4.10) ---

// fetchOAuthProviders lists the providers the backend will currently
// accept a login from on this surface (GET /v1/auth/client/providers —
// toggle on AND structurally configured, spec §4.4). Unlike
// fetchAuthPolicy this does NOT fall open: a 503 auth.policy_unavailable
// (document-level failure), a network error, or a body that does not carry
// a `providers` array rejects, so the page renders a retryable error
// instead of concluding "no method exists". Only {providers: []} is the
// empty state. Names outside the allowlist are dropped with a console
// warning — a backend that learns a fifth provider needs a matching SPA
// entry first.
export async function fetchOAuthProviders(
  signal?: AbortSignal,
): Promise<OAuthProviderName[]> {
  const res = await jsonFetch("/v1/auth/client/providers", {
    method: "GET",
    signal,
  });
  if (!res.ok) throw await readError(res, "Sign-in providers unavailable");
  const body = (await res.json()) as { providers?: unknown } | null;
  if (!body || typeof body !== "object" || !Array.isArray(body.providers)) {
    throw err("Sign-in providers response malformed", 500);
  }
  const out: OAuthProviderName[] = [];
  for (const name of body.providers) {
    if (isOAuthProvider(name)) {
      out.push(name);
    } else {
      console.warn(
        `fetchOAuthProviders: unknown provider ${JSON.stringify(name)} ignored`,
      );
    }
  }
  return out;
}

// browserNavigation is the one seam through which the SPA leaves for the
// IdP — a plain object so tests can spy on `assign` without touching
// window.location (not configurable under happy-dom / jsdom).
export const browserNavigation = {
  assign(url: string): void {
    window.location.assign(url);
  },
};

// initiateOAuthLogin starts a web OAuth login: POST {provider} to
// /v1/auth/client/oauth/login — credentials:'include' is load-bearing,
// the response sets the HttpOnly `orkestra_oauth_state` cookie the relay
// endpoint later REQUIRES — then stashes the validated `next` target and
// leaves for the IdP's authorization URL. Nothing about the destination
// is sent: the backend redirects to the configured tier SPA. Errors carry
// the backend `code` (auth.login_disabled | auth.oauth_provider_disabled
// | auth.policy_unavailable) for the page to map; on any error nothing is
// stashed and the page is not left.
export async function initiateOAuthLogin(
  provider: OAuthProviderName,
  next: string | null,
): Promise<void> {
  const res = await jsonFetch("/v1/auth/client/oauth/login", {
    method: "POST",
    body: JSON.stringify({ provider }),
  });
  if (!res.ok) throw await readError(res, "Could not start sign-in");
  const body = (await res.json()) as { authUrl?: unknown };
  if (typeof body.authUrl !== "string" || body.authUrl === "") {
    throw err("OAuth start response missing authUrl", 500);
  }
  stashOAuthReturnTo(next);
  browserNavigation.assign(body.authUrl);
}

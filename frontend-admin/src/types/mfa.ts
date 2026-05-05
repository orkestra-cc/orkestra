// MFA — multi-factor authentication types mirroring the backend contracts
// at `/v1/auth/operator/mfa/*` and `/v1/auth/operator/me/mfa`. Kept here (not colocated with
// mfaApi) so UI components can import types without pulling the slice.

export type MfaStatus =
  | 'not_required'
  | 'required_pending_enrollment'
  | 'enrolled'
  | 'grace';
export type MfaFactorType = 'totp' | 'webauthn' | '';

export interface MfaStatusResponse {
  status: MfaStatus;
  type?: MfaFactorType;
  backupCodesRemaining: number;
  // True when the user's role obligates MFA enrollment. Computed server-side
  // from the system role + org memberships via RoleRequiresMFA.
  requiresMfa: boolean;
  // Deadline by which a user whose role requires MFA must enroll. Absent
  // before the grace clock starts (first privileged login).
  graceExpiresAt?: string | null;
  // Number of enrolled passkeys. Drives the passkeys card in settings —
  // the per-credential metadata lives at /v1/auth/operator/me/mfa/webauthn/credentials.
  webauthnCredentials: number;
}

export interface MfaEnrollBeginResponse {
  challengeId: string;
  secret: string;
  provisioningUri: string;
}

export interface MfaEnrollConfirmInput {
  challengeId: string;
  code: string;
}

export interface MfaEnrollConfirmResponse {
  success: boolean;
  backupCodes: string[];
}

export interface MfaVerifyInput {
  code: string;
  useBackup?: boolean;
}

export interface MfaVerifyResponse {
  success: boolean;
  accessToken: string;
  tokenType: string;
  expiresIn: number;
}

export interface MfaLoginVerifyInput {
  challengeId: string;
  code: string;
  useBackup?: boolean;
}

import type { BackendUser } from 'store/api/authApi';

export interface MfaLoginVerifyResponse {
  success: boolean;
  accessToken: string;
  tokenType: string;
  expiresIn: number;
  sessionId: string;
  deviceId?: string;
  user: BackendUser;
}

// --- WebAuthn ---
//
// PublicKey objects are passed through unmodified between backend and
// browser. Treat them as opaque JSON — the browser's
// PublicKeyCredentialCreationOptions / PublicKeyCredentialRequestOptions
// schema is enforced by the W3C library on both sides.

export interface WebAuthnRegisterBeginResponse {
  challengeId: string;
  publicKey: PublicKeyCredentialCreationOptionsJSON;
}

export interface WebAuthnRegisterFinishInput {
  challengeId: string;
  name: string;
  attestationResponse: PublicKeyCredentialJSON;
}

export interface WebAuthnRegisterFinishResponse {
  success: boolean;
  credential: WebAuthnCredentialPublic;
}

export interface WebAuthnVerifyBeginResponse {
  challengeId: string;
  publicKey: PublicKeyCredentialRequestOptionsJSON;
}

export interface WebAuthnVerifyFinishInput {
  challengeId: string;
  assertionResponse: PublicKeyCredentialJSON;
}

export interface WebAuthnVerifyFinishResponse {
  success: boolean;
  accessToken: string;
  tokenType: string;
  expiresIn: number;
}

export interface WebAuthnLoginBeginInput {
  loginChallengeId: string;
}

export interface WebAuthnLoginBeginResponse {
  challengeId: string;
  publicKey: PublicKeyCredentialRequestOptionsJSON;
}

export interface WebAuthnLoginFinishInput {
  loginChallengeId: string;
  webauthnChallengeId: string;
  assertionResponse: PublicKeyCredentialJSON;
}

export interface WebAuthnLoginFinishResponse {
  success: boolean;
  accessToken: string;
  tokenType: string;
  expiresIn: number;
  sessionId: string;
  deviceId?: string;
  user: BackendUser;
}

export interface WebAuthnCredentialPublic {
  credentialId: string; // base64url
  name: string;
  createdAt: string;
  lastUsedAt?: string | null;
  transports?: string[];
  backupState?: boolean;
  cloneWarning?: boolean;
}

export interface WebAuthnCredentialsListResponse {
  credentials: WebAuthnCredentialPublic[];
}

// Loose JSON envelopes for the W3C credential shapes. The browser's actual
// types live on PublicKeyCredential / AuthenticatorAttestationResponse,
// which the helpers in store/api/webauthnCodec convert to this JSON form.
export type PublicKeyCredentialCreationOptionsJSON = Record<string, unknown>;
export type PublicKeyCredentialRequestOptionsJSON = Record<string, unknown>;
export type PublicKeyCredentialJSON = Record<string, unknown>;

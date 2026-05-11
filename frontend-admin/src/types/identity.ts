// Shared type definitions for the identity admin module. Mirrors the
// wire shapes the backend emits — see
// backend/internal/addons/identity/handlers/admin_handler.go and
// scim_admin_handler.go.

export interface IdPConfigPayload {
  displayName: string;
  issuerURL: string;
  clientId: string;
  /**
   * Plaintext client secret. Send an empty string (or omit) on update to
   * preserve the value stored on the backend — the API never returns the
   * real secret, only a redacted placeholder (`***`).
   */
  clientSecret?: string;
  redirectURL: string;
  scopes?: string[];
  subClaim?: string;
  emailClaim?: string;
  nameClaim?: string;
  enabled: boolean;
}

export interface IdPConfigView {
  uuid: string;
  tenantId: string;
  protocol: string;
  displayName: string;
  issuerURL: string;
  clientId: string;
  /** Redacted placeholder (`***`) when a secret is set; empty otherwise. */
  clientSecret?: string;
  redirectURL: string;
  scopes?: string[];
  subClaim?: string;
  emailClaim?: string;
  nameClaim?: string;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ScimTokenStatus {
  exists: boolean;
  uuid?: string;
  createdAt?: string;
}

export interface ScimTokenRotated {
  uuid: string;
  /** The raw bearer token. Shown exactly once — never returned again. */
  token: string;
  createdAt: string;
}

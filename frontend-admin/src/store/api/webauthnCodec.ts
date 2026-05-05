// WebAuthn JSON codec — converts between W3C PublicKeyCredential objects
// (which carry raw ArrayBuffers) and the base64url JSON shape both the
// browser and the go-webauthn library accept on the wire.
//
// The browser's `navigator.credentials.create()` and `.get()` return live
// PublicKeyCredential objects whose binary fields (rawId, attestationObject,
// authenticatorData, signature, userHandle, …) are ArrayBuffers; the
// backend receives JSON, so every binary blob must be base64url-encoded.
// In the other direction, the backend hands us PublicKeyCredentialCreation/
// RequestOptions with base64url strings that the browser API requires as
// BufferSource — we decode them back to Uint8Array before invoking the API.

const b64urlEncode = (buf: ArrayBuffer | Uint8Array): string => {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
  let bin = '';
  for (let i = 0; i < bytes.byteLength; i += 1) {
    bin += String.fromCharCode(bytes[i]);
  }
  // btoa → standard base64; convert to URL-safe (no padding) variant.
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
};

const b64urlDecode = (s: string): Uint8Array => {
  const padded = s.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((s.length + 3) % 4);
  const bin = atob(padded);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i += 1) {
    out[i] = bin.charCodeAt(i);
  }
  return out;
};

// Decode every field on a CredentialCreationOptions / RequestOptions JSON
// payload that the browser API expects as a BufferSource. The set of
// binary fields is fixed by the W3C spec; we walk them rather than
// generically scanning so a backend shape change won't silently corrupt
// unknown fields.
export const decodeCreationOptions = (raw: Record<string, unknown>): PublicKeyCredentialCreationOptions => {
  const o = raw as Record<string, unknown>;
  const challenge = b64urlDecode(o.challenge as string);
  const user = o.user as Record<string, unknown>;
  const userId = b64urlDecode(user.id as string);
  const excludeCredentials = (o.excludeCredentials as Array<Record<string, unknown>> | undefined)?.map((c) => ({
    id: b64urlDecode(c.id as string),
    type: c.type as PublicKeyCredentialType,
    transports: c.transports as AuthenticatorTransport[] | undefined,
  }));
  // Cast through `unknown` because the source shape is the W3C JSON
  // envelope (rp + pubKeyCredParams already populated by the server) and
  // we only swap the binary fields. The browser API will validate the
  // final object structure when it consumes it.
  return {
    ...(o as object),
    challenge,
    user: { ...(user as object), id: userId },
    excludeCredentials,
  } as unknown as PublicKeyCredentialCreationOptions;
};

export const decodeRequestOptions = (raw: Record<string, unknown>): PublicKeyCredentialRequestOptions => {
  const o = raw as Record<string, unknown>;
  const challenge = b64urlDecode(o.challenge as string);
  const allowCredentials = (o.allowCredentials as Array<Record<string, unknown>> | undefined)?.map((c) => ({
    id: b64urlDecode(c.id as string),
    type: c.type as PublicKeyCredentialType,
    transports: c.transports as AuthenticatorTransport[] | undefined,
  }));
  return {
    ...(o as object),
    challenge,
    allowCredentials,
  } as unknown as PublicKeyCredentialRequestOptions;
};

// Encode an attestation (registration) credential to the JSON shape
// go-webauthn's protocol.ParseCredentialCreationResponseBytes consumes.
export const encodeAttestation = (cred: PublicKeyCredential): Record<string, unknown> => {
  const r = cred.response as AuthenticatorAttestationResponse;
  return {
    id: cred.id,
    rawId: b64urlEncode(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    response: {
      attestationObject: b64urlEncode(r.attestationObject),
      clientDataJSON: b64urlEncode(r.clientDataJSON),
      transports: typeof r.getTransports === 'function' ? r.getTransports() : undefined,
    },
    clientExtensionResults: cred.getClientExtensionResults?.() ?? {},
  };
};

// Encode an assertion (login/step-up) credential to the JSON shape
// go-webauthn's protocol.ParseCredentialRequestResponseBytes consumes.
export const encodeAssertion = (cred: PublicKeyCredential): Record<string, unknown> => {
  const r = cred.response as AuthenticatorAssertionResponse;
  return {
    id: cred.id,
    rawId: b64urlEncode(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    response: {
      authenticatorData: b64urlEncode(r.authenticatorData),
      clientDataJSON: b64urlEncode(r.clientDataJSON),
      signature: b64urlEncode(r.signature),
      userHandle: r.userHandle ? b64urlEncode(r.userHandle) : undefined,
    },
    clientExtensionResults: cred.getClientExtensionResults?.() ?? {},
  };
};

// browserSupportsWebAuthn returns true when the runtime exposes the W3C
// API. False on insecure contexts (http on a remote host), older browsers,
// and inside iframes that haven't been granted publickey-credentials-get.
export const browserSupportsWebAuthn = (): boolean =>
  typeof window !== 'undefined' &&
  typeof window.PublicKeyCredential !== 'undefined' &&
  typeof navigator !== 'undefined' &&
  typeof navigator.credentials?.create === 'function' &&
  typeof navigator.credentials?.get === 'function';

// Exported for tests that need to round-trip arbitrary buffers.
export const __test = { b64urlEncode, b64urlDecode };

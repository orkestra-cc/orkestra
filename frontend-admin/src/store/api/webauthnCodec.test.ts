import { describe, expect, it } from 'vitest';
import {
  __test,
  decodeCreationOptions,
  decodeRequestOptions,
  encodeAttestation,
  encodeAssertion,
} from './webauthnCodec';

const { b64urlEncode, b64urlDecode } = __test;

describe('base64url codec', () => {
  it('round-trips arbitrary bytes', () => {
    const input = new Uint8Array([0, 1, 2, 3, 250, 251, 252, 253, 254, 255]);
    const encoded = b64urlEncode(input);
    expect(encoded).not.toContain('+');
    expect(encoded).not.toContain('/');
    expect(encoded).not.toContain('=');
    expect(Array.from(b64urlDecode(encoded))).toEqual(Array.from(input));
  });

  it('matches the W3C base64url shape — RFC 4648 §5 with no padding', () => {
    // "Hello?" = 0x48 0x65 0x6c 0x6c 0x6f 0x3f → standard base64 "SGVsbG8/"
    // → URL-safe form replaces "/" with "_" and strips padding.
    const bytes = new TextEncoder().encode('Hello?');
    expect(b64urlEncode(bytes)).toBe('SGVsbG8_');
  });
});

describe('decodeCreationOptions', () => {
  it('decodes challenge + user.id buffers in place', () => {
    const challengeBytes = new Uint8Array([1, 2, 3, 4]);
    const userIdBytes = new Uint8Array([10, 20, 30]);
    const raw = {
      challenge: b64urlEncode(challengeBytes),
      user: { id: b64urlEncode(userIdBytes), name: 'a@b.com', displayName: 'A B' },
      rp: { name: 'test', id: 'localhost' },
      pubKeyCredParams: [],
      excludeCredentials: [
        { id: b64urlEncode(new Uint8Array([0xAA])), type: 'public-key', transports: ['internal'] },
      ],
    };

    const opts = decodeCreationOptions(raw);
    expect(opts.challenge).toBeInstanceOf(Uint8Array);
    expect(Array.from(opts.challenge as Uint8Array)).toEqual(Array.from(challengeBytes));
    expect(Array.from(opts.user.id as Uint8Array)).toEqual(Array.from(userIdBytes));
    expect(opts.excludeCredentials).toHaveLength(1);
    expect(opts.excludeCredentials?.[0].type).toBe('public-key');
  });
});

describe('decodeRequestOptions', () => {
  it('decodes challenge + allowCredentials.id buffers', () => {
    const challengeBytes = new Uint8Array([5, 6, 7]);
    const credId = new Uint8Array([0xBE, 0xEF]);
    const raw = {
      challenge: b64urlEncode(challengeBytes),
      allowCredentials: [{ id: b64urlEncode(credId), type: 'public-key' }],
    };
    const opts = decodeRequestOptions(raw);
    expect(Array.from(opts.challenge as Uint8Array)).toEqual(Array.from(challengeBytes));
    expect(Array.from(opts.allowCredentials?.[0].id as Uint8Array)).toEqual(Array.from(credId));
  });
});

describe('encodeAttestation / encodeAssertion', () => {
  it('encodes attestation buffers as base64url', () => {
    const cred = {
      id: 'cred-id',
      rawId: new Uint8Array([1, 2]).buffer,
      type: 'public-key',
      response: {
        attestationObject: new Uint8Array([3, 4]).buffer,
        clientDataJSON: new Uint8Array([5, 6]).buffer,
        getTransports: () => ['usb'],
      },
      getClientExtensionResults: () => ({}),
    } as unknown as PublicKeyCredential;

    const out = encodeAttestation(cred) as Record<string, unknown>;
    expect(out.id).toBe('cred-id');
    expect(out.rawId).toBe(b64urlEncode(new Uint8Array([1, 2])));
    const response = out.response as Record<string, unknown>;
    expect(response.attestationObject).toBe(b64urlEncode(new Uint8Array([3, 4])));
    expect(response.transports).toEqual(['usb']);
  });

  it('encodes assertion buffers including optional userHandle', () => {
    const cred = {
      id: 'cred-id',
      rawId: new Uint8Array([1]).buffer,
      type: 'public-key',
      response: {
        authenticatorData: new Uint8Array([10]).buffer,
        clientDataJSON: new Uint8Array([20]).buffer,
        signature: new Uint8Array([30]).buffer,
        userHandle: new Uint8Array([40]).buffer,
      },
      getClientExtensionResults: () => ({}),
    } as unknown as PublicKeyCredential;

    const out = encodeAssertion(cred) as Record<string, unknown>;
    const response = out.response as Record<string, unknown>;
    expect(response.signature).toBe(b64urlEncode(new Uint8Array([30])));
    expect(response.userHandle).toBe(b64urlEncode(new Uint8Array([40])));
  });
});

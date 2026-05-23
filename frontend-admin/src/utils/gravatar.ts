// Gravatar hashing + URL helpers.
//
// Gravatar accepts a SHA-256 hex digest of the lowercased, trimmed email
// (https://docs.gravatar.com/api/avatars/hash/). MD5 still works but is
// deprecated; we use SHA-256 via the browser's SubtleCrypto API.
//
// Hashing is async, so consumers go through `useGravatarUrl` which fills
// a module-level cache and re-renders once the digest is ready. Until
// then `Avatar` falls back to initials (its built-in `name` mode).
//
// GDPR note: the operator console sends a SHA-256 of the contact email
// to Gravatar's CDN, which is a US-based third party. The user opted
// into this trade-off when adding avatars to the contacts table.

import { useEffect, useState } from 'react';

const cache = new Map<string, string>();

const normalize = (email: string) => email.trim().toLowerCase();

const toHex = (buffer: ArrayBuffer) =>
  Array.from(new Uint8Array(buffer))
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');

const sha256Hex = async (input: string): Promise<string> => {
  const bytes = new TextEncoder().encode(input);
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return toHex(digest);
};

export type GravatarFallback =
  | 'identicon'
  | 'monsterid'
  | 'wavatar'
  | 'retro'
  | 'robohash'
  | 'mp'
  | '404';

export interface GravatarOptions {
  size?: number;
  fallback?: GravatarFallback;
  rating?: 'g' | 'pg' | 'r' | 'x';
}

export const gravatarUrl = (
  hash: string,
  { size = 80, fallback = 'identicon', rating = 'g' }: GravatarOptions = {}
): string =>
  `https://www.gravatar.com/avatar/${hash}?s=${size}&d=${fallback}&r=${rating}`;

export const useGravatarUrl = (
  email: string | undefined,
  opts: GravatarOptions = {}
): string | undefined => {
  const key = email ? normalize(email) : '';
  const [hash, setHash] = useState<string | undefined>(() =>
    key ? cache.get(key) : undefined
  );

  useEffect(() => {
    if (!key) {
      setHash(undefined);
      return;
    }
    const cached = cache.get(key);
    if (cached) {
      setHash(cached);
      return;
    }
    let cancelled = false;
    sha256Hex(key).then(h => {
      cache.set(key, h);
      if (!cancelled) setHash(h);
    });
    return () => {
      cancelled = true;
    };
  }, [key]);

  if (!hash) return undefined;
  return gravatarUrl(hash, opts);
};

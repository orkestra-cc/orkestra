import { sanitizeNext } from "@/lib/safeNext";

// sessionStorage record carrying the validated `next` target across the
// OAuth round-trip (router state cannot survive the redirect out to the
// IdP). Written by initiateOAuthLogin just before leaving the SPA; the
// callback page takes-and-deletes it on EVERY outcome and honours it only
// when it is younger than OAUTH_RETURN_TO_TTL_MS and still passes
// sanitizeNext — sessionStorage is client-writable, so the value is
// re-validated on the way out (spec §4.10, §5 #28).
export const OAUTH_RETURN_TO_KEY = "orkestra_client_oauth_return_to";
export const OAUTH_RETURN_TO_TTL_MS = 10 * 60 * 1000;

interface OAuthReturnRecord {
  target: string;
  createdAt: number;
}

export function stashOAuthReturnTo(
  target: unknown,
  now: number = Date.now(),
): void {
  const safe = sanitizeNext(target);
  try {
    if (!safe) {
      // Also drops a stale record from an earlier, abandoned attempt.
      sessionStorage.removeItem(OAUTH_RETURN_TO_KEY);
      return;
    }
    const record: OAuthReturnRecord = { target: safe, createdAt: now };
    sessionStorage.setItem(OAUTH_RETURN_TO_KEY, JSON.stringify(record));
  } catch {
    // Storage can throw (private mode, disabled storage). Losing the deep
    // link degrades to DEFAULT_POST_LOGIN; it never blocks the login.
  }
}

/** Take-and-delete: the record is removed on every call, whatever its state. */
export function takeOAuthReturnTo(now: number = Date.now()): string | null {
  let raw: string | null = null;
  try {
    raw = sessionStorage.getItem(OAUTH_RETURN_TO_KEY);
    sessionStorage.removeItem(OAUTH_RETURN_TO_KEY);
  } catch {
    return null;
  }
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object") return null;
  const { target, createdAt } = parsed as Partial<OAuthReturnRecord>;
  if (typeof createdAt !== "number" || !Number.isFinite(createdAt)) return null;
  if (now < createdAt || now - createdAt > OAUTH_RETURN_TO_TTL_MS) return null;
  return sanitizeNext(target);
}

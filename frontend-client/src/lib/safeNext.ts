// Post-login destination validation — the single open-redirect gate of the
// SPA. A `next` value can arrive from the URL (RequireAuth stamps
// `/login?next=<path>`), from a user-crafted link, or survive an OAuth
// round-trip through sessionStorage (client-writable). Every read funnels
// through sanitizeNext; callers fall back to DEFAULT_POST_LOGIN on null.

export const DEFAULT_POST_LOGIN = "/account";

// Routes a successful login must never bounce back into: the callback page
// itself (a self-loop) and the anonymous auth pages, which would redirect
// straight back out or strand the user on a form they no longer need.
// Stored as segment lists and compared against the DECODED, lower-cased,
// normalised path — the way react-router itself matches (case-insensitively,
// on the decoded pathname), so "/LOGIN" and "/%6cogin" land here exactly as
// the router would treat them. "/auth//callback" and "/login%2Fx" are
// rejected too, but that's this gate being stricter than the router, not a
// router-accurate read — react-router 404s on both instead of routing them
// to the auth page.
const AUTH_ROUTES: readonly (readonly string[])[] = [
  ["auth", "callback"],
  ["login"],
  ["signup"],
  ["forgot-password"],
  ["reset-password"],
  ["verify-email"],
  ["accept-invite"],
];

// eslint-disable-next-line no-control-regex
const CONTROL_CHARS = /[\x00-\x1f\x7f]/;

/**
 * canonicalSegments — the path as the router will match it: percent-decoded
 * (a malformed sequence → null), lower-cased, empty and "." segments
 * dropped, ".." resolved against what precedes it. A control character or
 * a backslash revealed by decoding → null.
 */
function canonicalSegments(pathname: string): string[] | null {
  let decoded: string;
  try {
    decoded = decodeURIComponent(pathname);
  } catch {
    return null;
  }
  if (CONTROL_CHARS.test(decoded) || decoded.includes("\\")) return null;
  const out: string[] = [];
  for (const segment of decoded.toLowerCase().split("/")) {
    if (segment === "" || segment === ".") continue;
    if (segment === "..") {
      out.pop();
      continue;
    }
    out.push(segment);
  }
  return out;
}

const isAuthRoute = (segments: readonly string[]): boolean =>
  AUTH_ROUTES.some((route) =>
    route.every((segment, index) => segments[index] === segment),
  );

/**
 * Validate a candidate post-login destination. Returns the canonical
 * same-origin relative path (`pathname + search + hash` as the URL parser
 * produced it), or null when the value is missing, malformed, points
 * off-site, or would loop back into the auth flow (spec §4.10
 * frontend-client row, §5 #28):
 *
 *  - must begin with exactly one "/" — rejects absolute URLs (same-origin
 *    ones included), bare paths, protocol-relative "//host" and the
 *    "/\host" variant browsers normalise to "//";
 *  - no raw or percent-encoded backslash anywhere, no control characters,
 *    no encoded leading slash ("/%2F…") that could smuggle "//" past the
 *    prefix rule;
 *  - parses against window.location.origin and the parsed origin must be
 *    the same (belt and braces over the prefix rule; the parser also
 *    resolves "." / ".." / "%2e" segments);
 *  - never one of AUTH_ROUTES, judged on the decoded, lower-cased,
 *    normalised segments.
 */
export function sanitizeNext(raw: unknown): string | null {
  if (typeof raw !== "string") return null;
  const value = raw.trim();
  if (value === "") return null;
  if (value[0] !== "/") return null;
  if (value[1] === "/" || value[1] === "\\") return null;
  if (CONTROL_CHARS.test(value)) return null;
  const lower = value.toLowerCase();
  if (lower.includes("\\") || lower.includes("%5c")) return null;
  if (lower.startsWith("/%2f")) return null;

  let parsed: URL;
  try {
    parsed = new URL(value, window.location.origin);
  } catch {
    return null;
  }
  if (parsed.origin !== window.location.origin) return null;

  const segments = canonicalSegments(parsed.pathname);
  if (segments === null || isAuthRoute(segments)) return null;

  // The parser folds "." / ".." / "%2e" segments and can leave an EMPTY
  // first segment: "/.//evil.example" serialises back as "//evil.example",
  // which is protocol-relative. The prefix rule at the top only saw the
  // pre-parse spelling, so re-assert it on what we are about to return.
  if (parsed.pathname[1] === "/") return null;

  return `${parsed.pathname}${parsed.search}${parsed.hash}`;
}

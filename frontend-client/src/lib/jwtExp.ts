// Reads a JWT's `exp` claim WITHOUT verifying the signature.
//
// This is a scheduling hint and never a security decision — the backend
// remains the only authority on whether a token is valid. It exists purely as
// the FALLBACK for §4.5: the expiry the store actually reckons with is derived
// from the `expiresIn` DURATION the server reported at receipt, because both
// ends of that comparison then come from the same clock and a constant offset
// cancels. Comparing a server-issued absolute `exp` against the browser's wall
// clock is only as accurate as the difference between the two, and a badly set
// clock reopens that window every TTL cycle. This function is reached only when
// a response carried no `expiresIn` at all.
//
// Returns the expiry in the Date.now() domain (milliseconds), or null for
// anything unreadable. A null expiry means UNKNOWN, and §4.3 branch 2 treats
// unknown as LIVE: an unknown expiry cannot prove the request never reached
// its handler, and under a rule whose failure mode is a REPLAY rather than a
// wasted refresh, "don't know" has to fall on the safe side.
export function jwtExp(token: string | null | undefined): number | null {
  if (!token) return null;
  const parts = token.split(".");
  if (parts.length !== 3) return null;

  let json: string;
  try {
    // atob throws InvalidCharacterError on `-` and `_`: they are the base64URL
    // alphabet's substitutions for `+` and `/` and are not valid base64. This
    // is the part that actually breaks. Re-padding to a multiple of 4 is
    // belt-and-braces — atob tolerates missing padding in both Node and
    // happy-dom today — but the WHATWG forgiving-base64 algorithm specifies
    // failure at length ≡ 1 mod 4, so a stricter runtime is one upgrade away.
    const b64 = parts[1].replace(/-/g, "+").replace(/_/g, "/");
    json = atob(b64 + "=".repeat((4 - (b64.length % 4)) % 4));
  } catch {
    return null;
  }

  let payload: unknown;
  try {
    payload = JSON.parse(json);
  } catch {
    return null;
  }
  if (typeof payload !== "object" || payload === null) return null;

  const exp = (payload as { exp?: unknown }).exp;
  // Number.isFinite, NOT `typeof exp === "number"`. `{"exp":1e400}` is entirely
  // valid JSON, parses to Infinity, and its typeof IS "number" — an infinite
  // exp would read as a token that never expires and would silently disable
  // the 401 recovery for the life of the tab. `-1e400` is the mirror case.
  if (!Number.isFinite(exp)) return null;
  return (exp as number) * 1000;
}

/** Grammar and bounds, mirroring `recordlist_slug.go`. */
export const MAX_SLUG_LENGTH = 64;
export const MAX_LABEL_LENGTH = 120;

const SLUG_RE = /^[a-z0-9]+(-[a-z0-9]+)*$/;

/**
 * Derives an element's immutable key segment from its display name.
 *
 * A deliberate re-implementation of the backend's `MintSlug`, not an
 * approximation of it: this drives the preview shown while the operator types,
 * and the slug they are shown has to be the slug they get. NFKD-normalise,
 * strip combining marks, lowercase, collapse every non-alphanumeric run to a
 * single dash, trim, then truncate at 64 without leaving a trailing dash.
 *
 * Returns '' when the name carries nothing a slug can be built from — the
 * caller refuses rather than inventing one, exactly as the backend does.
 */
export const mintSlug = (label: string): string => {
  const folded = label
    .normalize('NFKD')
    // Combining diacritical marks — the JS equivalent of Go's unicode.Mn.
    .replace(/\p{Mn}/gu, '');
  let s = folded
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
  if (s.length > MAX_SLUG_LENGTH) {
    s = s.slice(0, MAX_SLUG_LENGTH).replace(/-+$/, '');
  }
  return s;
};

export const isValidSlug = (s: string): boolean =>
  s !== '' && s.length <= MAX_SLUG_LENGTH && SLUG_RE.test(s);

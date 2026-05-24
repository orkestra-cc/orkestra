// Deterministic per-user color picker for initials avatars. Same seed
// always produces the same color so the avatar a user sees in the
// navbar matches the one on their profile page. Hand-tuned palette
// (Bootstrap-aligned hues + dark variants) — each pair carries a
// background and a text color picked for AA contrast on white.

const PALETTE: ReadonlyArray<{ bg: string; fg: string }> = [
  { bg: '#5d6b98', fg: '#ffffff' },
  { bg: '#3874ff', fg: '#ffffff' },
  { bg: '#00d27a', fg: '#0a0e25' },
  { bg: '#27bcfd', fg: '#0a0e25' },
  { bg: '#f5803e', fg: '#ffffff' },
  { bg: '#e63757', fg: '#ffffff' },
  { bg: '#956bd2', fg: '#ffffff' },
  { bg: '#0f7c69', fg: '#ffffff' },
  { bg: '#b54708', fg: '#ffffff' },
  { bg: '#175cd3', fg: '#ffffff' },
  { bg: '#7a5af8', fg: '#ffffff' },
  { bg: '#c11574', fg: '#ffffff' }
];

// FNV-1a — small + branchless, plenty of distribution for a 12-bucket
// palette. We don't need cryptographic strength.
const hash = (input: string): number => {
  let h = 0x811c9dc5;
  for (let i = 0; i < input.length; i += 1) {
    h ^= input.charCodeAt(i);
    h = (h + ((h << 1) + (h << 4) + (h << 7) + (h << 8) + (h << 24))) >>> 0;
  }
  return h;
};

export interface AvatarColor {
  background: string;
  color: string;
}

// avatarColor returns a deterministic palette entry for the given seed.
// Pass the most stable identifier you have — UUID > email > fullName —
// so the color stays steady even if the user renames themselves.
export const avatarColor = (seed?: string): AvatarColor => {
  if (!seed) return { background: PALETTE[0].bg, color: PALETTE[0].fg };
  const entry = PALETTE[hash(seed) % PALETTE.length];
  return { background: entry.bg, color: entry.fg };
};

// initialsFor extracts up to 2 leading word characters from a name,
// falling back to the email local-part's leading 2 characters when the
// name is empty (signups that haven't filled in fullName yet).
export const initialsFor = (name?: string, email?: string): string => {
  const trimmed = (name ?? '').trim();
  if (trimmed) {
    const words = trimmed.split(/\s+/).filter(Boolean);
    if (words.length === 1) {
      return words[0].slice(0, 2).toUpperCase();
    }
    return (words[0][0] + words[words.length - 1][0]).toUpperCase();
  }
  const local = (email ?? '').split('@')[0];
  return local.slice(0, 2).toUpperCase() || '?';
};

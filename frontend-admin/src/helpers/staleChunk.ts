// After a deploy the hashed files of the previous build are gone, so a tab
// that is still running the old bundle fails the first time it opens a lazy
// page ("Failed to fetch dynamically imported module"). React.lazy keeps that
// rejection, so every in-app navigation fails again: only a full page load,
// which fetches the new index.html, recovers. These helpers recognize that
// error and reload once, with a guard so a build that is really broken shows
// the error page instead of reloading forever.

export const STALE_CHUNK_RELOAD_KEY = 'orkestra:stale-chunk-reload';

// A second stale-chunk error this soon after an automatic reload means the
// reload did not help: show the error page instead.
const RELOAD_GUARD_MS = 30_000;

// Messages for a failed dynamic import in Chromium, Firefox and Safari, and
// for Vite's CSS preload of a lazy chunk.
const STALE_CHUNK_PATTERNS = [
  /failed to fetch dynamically imported module/i,
  /error loading dynamically imported module/i,
  /importing a module script failed/i,
  /unable to preload css/i
];

export const isStaleChunkError = (error: unknown): boolean => {
  if (!(error instanceof Error)) return false;
  return STALE_CHUNK_PATTERNS.some(p => p.test(error.message));
};

interface GuardStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

interface ReloadOptions {
  now?: number;
  storage?: GuardStorage;
  reload?: () => void;
}

const sessionStore = (): GuardStorage => window.sessionStorage;

/** True when no automatic reload happened in the last RELOAD_GUARD_MS. */
export const staleChunkReloadAllowed = ({
  now = Date.now(),
  storage
}: Pick<ReloadOptions, 'now' | 'storage'> = {}): boolean => {
  try {
    const last = Number(
      (storage ?? sessionStore()).getItem(STALE_CHUNK_RELOAD_KEY)
    );
    return !(last > 0 && now - last < RELOAD_GUARD_MS);
  } catch {
    // Without storage the guard cannot work: never reload automatically.
    return false;
  }
};

/** Reloads the page unless it already did moments ago; true when it reloads. */
export const reloadForStaleChunk = ({
  now = Date.now(),
  storage,
  reload = () => window.location.reload()
}: ReloadOptions = {}): boolean => {
  if (!staleChunkReloadAllowed({ now, storage })) return false;
  try {
    (storage ?? sessionStore()).setItem(STALE_CHUNK_RELOAD_KEY, String(now));
  } catch {
    return false;
  }
  reload();
  return true;
};

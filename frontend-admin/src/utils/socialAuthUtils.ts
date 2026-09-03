// Social OAuth utility functions for multiple providers

import runtimeConfig from 'config/environment';
import { sanitizeReturnTo } from 'utils/returnTo';

interface SocialOAuthInitResponse {
  authUrl: string;
  state: string;
}

export type SocialProvider = 'google' | 'apple' | 'github' | 'discord';

// sessionStorage key holding the deep link to return to after the OAuth
// round-trip completes. Router state can't survive the redirect out to the
// IdP, so it is stashed here as a `{target, createdAt}` record;
// SocialAuthCallback takes-and-deletes it (in an effect, never during
// render) on EVERY outcome and honours it only when it is younger than
// OAUTH_RETURN_TO_TTL_MS and still passes sanitizeReturnTo (sessionStorage
// is client-writable).
export const OAUTH_RETURN_TO_KEY = 'oauth_return_to';
export const OAUTH_RETURN_TO_TTL_MS = 10 * 60 * 1000;

interface OAuthReturnRecord {
  target: string;
  createdAt: number;
}

export const stashOAuthReturnTo = (
  target: string | null | undefined,
  now: number = Date.now()
): void => {
  const safe = sanitizeReturnTo(target);
  if (!safe) {
    // Also clears any stale value from a previous, abandoned attempt.
    sessionStorage.removeItem(OAUTH_RETURN_TO_KEY);
    return;
  }
  const record: OAuthReturnRecord = { target: safe, createdAt: now };
  sessionStorage.setItem(OAUTH_RETURN_TO_KEY, JSON.stringify(record));
};

/** Take-and-delete: the record is removed on every call, whatever its state. */
export const takeOAuthReturnTo = (now: number = Date.now()): string | null => {
  const raw = sessionStorage.getItem(OAUTH_RETURN_TO_KEY);
  sessionStorage.removeItem(OAUTH_RETURN_TO_KEY);
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== 'object') return null;
  const { target, createdAt } = parsed as Partial<OAuthReturnRecord>;
  if (typeof createdAt !== 'number' || !Number.isFinite(createdAt)) return null;
  if (now < createdAt || now - createdAt > OAUTH_RETURN_TO_TTL_MS) return null;
  return sanitizeReturnTo(target);
};

export const initiateSocialLogin = async (
  provider: SocialProvider,
  backendUrl: string = runtimeConfig.apiUrl,
  returnTo?: string | null
): Promise<void> => {
  try {
    if (!backendUrl || backendUrl === 'undefined') {
      throw new Error(
        'Backend URL is not configured. Please check your environment variables.'
      );
    }

    // The backend redirects to the SPA configured for this tier
    // (OPERATOR_FRONTEND_URL → FRONTEND_URL); nothing about the destination
    // is sent from here.
    const requestPayload = {
      provider: provider
    };

    const response = await fetch(`${backendUrl}/v1/auth/operator/oauth/login`, {
      method: 'POST',
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(requestPayload)
    });

    if (!response.ok) {
      throw new Error(
        `${provider} OAuth initiation failed: ${response.status} ${response.statusText}`
      );
    }

    const data: SocialOAuthInitResponse = await response.json();

    stashOAuthReturnTo(returnTo);

    window.location.href = data.authUrl;
  } catch (error) {
    throw error;
  }
};

export const logoutSocial = async (
  backendUrl: string = runtimeConfig.apiUrl,
  allDevices: boolean = false
): Promise<void> => {
  try {
    await fetch(`${backendUrl}/v1/auth/operator/logout`, {
      method: 'POST',
      credentials: 'include', // Use HttpOnly cookies for authentication
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        allDevices
      })
    });

    clearSessionStorage();
    window.location.href = '/login';
  } catch (error) {
    console.error('Logout error:', error);
    clearSessionStorage();
    window.location.href = '/login';
  }
};

export const clearSessionStorage = (): void => {
  // Clear OAuth session data only - no tokens stored in localStorage
  // Legacy keys older builds wrote; swept so no transient OAuth material lingers.
  sessionStorage.removeItem('oauth_state');
  sessionStorage.removeItem('oauth_provider');
  sessionStorage.removeItem('logout_in_progress');
};

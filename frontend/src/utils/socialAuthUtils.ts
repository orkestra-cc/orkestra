// Social OAuth utility functions for multiple providers

interface SocialOAuthInitResponse {
  authUrl: string;
  state: string;
}

interface SocialOAuthCallbackResponse {
  success: boolean;
  user: {
    id: string;
    email: string;
    fullName: string;
    avatar: string;
    role: string;
    oauthProvider: string;
  };
}

export type SocialProvider = 'google' | 'apple' | 'github' | 'discord';

export const initiateSocialLogin = async (
  provider: SocialProvider,
  backendUrl: string = import.meta.env.VITE_BACKEND_URL
): Promise<void> => {
  try {
    if (!backendUrl || backendUrl === 'undefined') {
      throw new Error('Backend URL is not configured. Please check your environment variables.');
    }

    // Backend automatically determines the frontend redirect URL from:
    // 1. Request Origin header + '/auth/callback'
    // 2. Configured FRONTEND_URL + '/auth/callback' (fallback)
    const requestPayload = {
      provider: provider
    };

    const response = await fetch(`${backendUrl}/api/v1/auth/oauth/login`, {
      method: 'POST',
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(requestPayload),
    });

    if (!response.ok) {
      throw new Error(`${provider} OAuth initiation failed: ${response.status} ${response.statusText}`);
    }

    const data: SocialOAuthInitResponse = await response.json();

    sessionStorage.setItem('oauth_state', data.state);
    sessionStorage.setItem('oauth_provider', provider);

    window.location.href = data.authUrl;
  } catch (error) {
    throw error;
  }
};

export const handleSocialCallback = async (
  code: string,
  state: string,
  backendUrl: string = import.meta.env.VITE_BACKEND_URL
): Promise<SocialOAuthCallbackResponse> => {
  try {
    const storedState = sessionStorage.getItem('oauth_state');
    const provider = sessionStorage.getItem('oauth_provider') as SocialProvider;

    if (storedState !== state) {
      throw new Error('Invalid OAuth state parameter');
    }

    if (!provider) {
      throw new Error('OAuth provider not found in session storage');
    }

    const params = new URLSearchParams({
      code,
      state,
    });

    const callbackUrl = `${backendUrl}/api/v1/auth/oauth/${provider}/callback?${params.toString()}`;

    const response = await fetch(callbackUrl, {
      method: 'GET',
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
      },
    });

    if (!response.ok) {
      throw new Error(`${provider} OAuth callback failed: ${response.status}`);
    }

    const data: SocialOAuthCallbackResponse = await response.json();

    sessionStorage.removeItem('oauth_state');
    sessionStorage.removeItem('oauth_provider');

    // No token storage needed - using HttpOnly cookies exclusively

    return data;
  } catch (error) {
    throw error;
  }
};

export const logoutSocial = async (
  backendUrl: string = import.meta.env.VITE_BACKEND_URL,
  allDevices: boolean = false
): Promise<void> => {
  try {
    await fetch(`${backendUrl}/api/v1/auth/logout`, {
      method: 'POST',
      credentials: 'include', // Use HttpOnly cookies for authentication
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        allDevices,
      }),
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
  sessionStorage.removeItem('oauth_state');
  sessionStorage.removeItem('oauth_provider');
  sessionStorage.removeItem('logout_in_progress');
};

// Deprecated: No longer storing tokens in localStorage
// Using HttpOnly cookies exclusively for authentication
export const getStoredTokens = (): {
  accessToken: string | null;
  tokenType: string | null;
  expiresIn: string | null;
  userId: string | null;
  email: string | null;
} => {
  console.warn('getStoredTokens is deprecated - using HttpOnly cookies for authentication');
  return {
    accessToken: null,
    tokenType: null,
    expiresIn: null,
    userId: null,
    email: null,
  };
};

// Deprecated: Cannot determine authentication status from localStorage
// Use RTK Query auth hooks instead to check authentication via API calls
export const isAuthenticated = (): boolean => {
  console.warn('isAuthenticated is deprecated - use RTK Query auth hooks to check authentication status');
  return false; // Cannot determine from client-side storage with HttpOnly cookies
};
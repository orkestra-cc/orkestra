/**
 * Simplified EVE Online SSO utility for React app
 * Backend handles all OAuth flow, state management, and token exchange
 */

type LoginType = 'login' | 'register';

interface EveAuthResponse {
  auth_url?: string;
  error?: string;
  message?: string;
}

/**
 * Get authorization URL from backend and redirect user
 * @param backendUrl - Backend API URL
 * @param loginType - Type of login: 'login' | 'register'
 * @returns Redirects to EVE Online SSO
 */
export const initiateEveLogin = async (
  backendUrl: string = import.meta.env.VITE_BACKEND_URL,
  loginType: LoginType = 'login'
): Promise<void> => {
  const endpoint = loginType === 'register' ? '/auth/eve/register' : '/auth/eve/login';
  const response = await fetch(`${backendUrl}${endpoint}`, {
    method: 'GET',
    credentials: 'include', // Include cookies for session management
    headers: {
      'Accept': 'application/json'
    }
  });

  if (!response.ok) {
    const errorData: EveAuthResponse = await response.json().catch(() => ({}));
    throw new Error(errorData.error || errorData.message || 'Failed to get authorization URL from backend');
  }

  const data: EveAuthResponse = await response.json();
  
  if (!data.auth_url) {
    throw new Error('No authorization URL received from backend');
  }

  // Redirect to EVE Online SSO
  window.location.href = data.auth_url;
};
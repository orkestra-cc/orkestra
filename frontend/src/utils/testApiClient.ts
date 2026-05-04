/**
 * Browser console test script for HttpOnly cookie authentication
 * Copy and paste this into the browser console to test secure authentication
 */

import { api } from './apiClient';

// Test function that can be run in browser console
export const testHttpOnlyCookieAuth = async () => {
  console.log('=== HttpOnly Cookie Authentication Test Started ===');

  // No localStorage token manipulation - using HttpOnly cookies exclusively
  console.log('1. Testing secure HttpOnly cookie authentication...');
  console.log('Note: No localStorage tokens used - using secure cookies only');

  try {
    const response = await api.get('/v1/auth/operator/me');
    console.log('Response received:', response);
    console.log('Was session refreshed?', response.refreshed);
    console.log('Authentication method: HttpOnly cookies (secure)');

    if (response.refreshed) {
      console.log('✅ SUCCESS: Session was refreshed via HttpOnly cookies!');

      // Test another request
      console.log('2. Testing second request with refreshed session...');
      const response2 = await api.get('/v1/auth/operator/me');
      console.log('Second response:', response2);
      console.log('Second request refreshed?', response2.refreshed);
    } else {
      console.log('ℹ️  No session refresh occurred (session already valid)');
    }

  } catch (error) {
    console.error('❌ API test failed:', error);
  }

  console.log('=== HttpOnly Cookie Authentication Test Completed ===');
};

// Auto-run if in browser environment
if (typeof window !== 'undefined') {
  (window as any).testHttpOnlyCookieAuth = testHttpOnlyCookieAuth;
  console.log('💡 Run testHttpOnlyCookieAuth() in the console to test secure authentication');
}

// Manual test steps for browser console
export const getBrowserTestInstructions = () => {
  return `
=== Secure HttpOnly Cookie Authentication Test Instructions ===

1. Open browser console on the frontend app
2. Run these commands:

// Test secure cookie authentication
import('/src/utils/testApiClient.js').then(module => {
  module.testHttpOnlyCookieAuth();
});

// Or if the function is already available:
testHttpOnlyCookieAuth();

3. Watch the console for detailed logs
4. Note: NO localStorage manipulation - secure cookies only

Expected behavior:
- Request uses HttpOnly cookies exclusively
- Backend manages session refresh via secure cookies
- No tokens stored in localStorage (XSS protection)
- Console shows "Session was refreshed via HttpOnly cookies!"
`;
};

export default testHttpOnlyCookieAuth;
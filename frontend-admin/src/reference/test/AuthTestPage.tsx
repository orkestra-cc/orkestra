import React, { useState } from 'react';
import { Card, Button, Row, Col, Alert } from 'react-bootstrap';
import { api } from '../../utils/apiClient';
import { useAppSelector, useAppDispatch } from '../../store/hooks';
import { selectAccessToken, selectTokenExpiry, setAccessToken, setUserFromApiResponse, clearAccessToken } from '../../store/slices/authSlice';
import { useLazyGetSessionQuery } from '../../store/api/authApi';

interface AuthResponse {
  authenticated?: boolean;
  user_id?: number;
  character_id?: number;
  character_name?: string;
  characters?: any[];
  _source?: string;
  error?: string;
  tokenStatus?: string;
  accessToken?: string;
  tokenType?: string;
  expiresIn?: number;
  [key: string]: any;
}

const AuthTestPage: React.FC = () => {
  const [cookieResponse, setCookieResponse] = useState<AuthResponse | null>(null);
  const [tokenResponse, setTokenResponse] = useState<AuthResponse | null>(null);
  const [apiClientResponse, setApiClientResponse] = useState<AuthResponse | null>(null);
  const [sessionResponse, setSessionResponse] = useState<AuthResponse | null>(null);
  const [loading, setLoading] = useState<{ cookie: boolean; token: boolean; apiClient: boolean; session: boolean }>({
    cookie: false,
    token: false,
    apiClient: false,
    session: false
  });

  // Redux state selectors and dispatch
  const dispatch = useAppDispatch();
  const accessToken = useAppSelector(selectAccessToken);
  const tokenExpiry = useAppSelector(selectTokenExpiry);
  const [triggerGetSession] = useLazyGetSessionQuery();

  const backendUrl = import.meta.env.VITE_BACKEND_URL as string;

  const testWithCookie = async () => {
    setLoading(prev => ({ ...prev, cookie: true }));
    setCookieResponse(null);

    try {
      const response = await fetch(`${backendUrl}/v1/auth/operator/me`, {
        method: 'GET',
        credentials: 'include', // Include cookies
        headers: {
          'Content-Type': 'application/json',
        },
      });

      const data = await response.json();

      if (response.ok) {
        setCookieResponse(data);
      } else {
        setCookieResponse({
          error: `HTTP ${response.status}: ${data.message || 'Unknown error'}`,
          ...data
        });
      }
    } catch (error) {
      setCookieResponse({
        error: `Network error: ${error instanceof Error ? error.message : 'Unknown error'}`
      });
    } finally {
      setLoading(prev => ({ ...prev, cookie: false }));
    }
  };

  const testWithBearerToken = async () => {
    setLoading(prev => ({ ...prev, token: true }));
    setTokenResponse(null);

    // Determine what token to use for testing
    let tokenToUse = accessToken;
    let tokenStatus = 'valid';

    if (!accessToken) {
      tokenToUse = 'invalid_token_for_testing';
      tokenStatus = 'missing_from_redux';
    } else if (tokenExpiry && new Date(tokenExpiry) <= new Date()) {
      tokenStatus = 'expired';
    }

    console.log(`[AUTH_TEST] Testing Bearer token - Status: ${tokenStatus}, Token: ${tokenToUse ? 'present' : 'null'}`);

    try {
      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
      };

      // Always include Authorization header (even if token is invalid/missing)
      headers['Authorization'] = `Bearer ${tokenToUse}`;

      const response = await fetch(`${backendUrl}/v1/auth/operator/me`, {
        method: 'GET',
        credentials: 'omit', // Don't include cookies for Bearer token test
        headers,
      });

      const data = await response.json();

      if (response.ok) {
        setTokenResponse({
          ...data,
          _source: `bearer_token_${tokenStatus}`,
          tokenStatus
        });
      } else {
        setTokenResponse({
          error: `HTTP ${response.status}: ${data.message || 'Unknown error'}`,
          tokenStatus,
          _source: `bearer_token_${tokenStatus}_failed`,
          ...data
        });
      }
    } catch (error) {
      setTokenResponse({
        error: `Network error: ${error instanceof Error ? error.message : 'Unknown error'}`,
        tokenStatus,
        _source: `bearer_token_${tokenStatus}_network_error`
      });
    } finally {
      setLoading(prev => ({ ...prev, token: false }));
    }
  };

  const testWithApiClient = async () => {
    setLoading(prev => ({ ...prev, apiClient: true }));
    setApiClientResponse(null);

    console.log('[AUTH_TEST] Starting API client test...');
    console.log('[AUTH_TEST] Using HttpOnly cookie authentication');

    try {
      const response = await api.get('/v1/auth/operator/me');
      console.log('[AUTH_TEST] API response received:', response);

      // The API client will automatically handle token refresh if needed
      const result: AuthResponse = {
        ...response.data,
        authenticated: response.data.isActive,
        user_id: response.data.id ? parseInt(response.data.id, 10) : null,
        _source: response.refreshed ? 'api_client_with_refresh' : 'api_client_success'
      };

      if (response.refreshed) {
        result._source += ' (SESSION_REFRESHED)';
        console.log('[AUTH_TEST] Session was refreshed via HttpOnly cookies');
      }

      setApiClientResponse(result);
    } catch (error: any) {
      console.error('[AUTH_TEST] API client test failed:', error);
      setApiClientResponse({
        error: `${error.status ? `HTTP ${error.status}` : 'Network error'}: ${error.message}`,
        ...error.data
      });
    } finally {
      setLoading(prev => ({ ...prev, apiClient: false }));
    }
  };

  const testSessionEndpoint = async () => {
    setLoading(prev => ({ ...prev, session: true }));
    setSessionResponse(null);

    console.log('[AUTH_TEST] Testing /auth/session endpoint...');

    try {
      const sessionResult = await triggerGetSession();

      if (sessionResult.error) {
        setSessionResponse({
          error: `Session endpoint error: ${sessionResult.error}`
        });
      } else if (sessionResult.data) {
        // Store access token in Redux state
        dispatch(setAccessToken({
          accessToken: sessionResult.data.accessToken,
          expiresIn: sessionResult.data.expiresIn
        }));

        // Update user info in Redux
        if (sessionResult.data.user) {
          dispatch(setUserFromApiResponse(sessionResult.data.user));
        }

        setSessionResponse({
          authenticated: sessionResult.data.success,
          user_id: sessionResult.data.user?.id ? parseInt(sessionResult.data.user.id, 10) : undefined,
          _source: 'session_endpoint',
          accessToken: sessionResult.data.accessToken,
          tokenType: sessionResult.data.tokenType,
          expiresIn: sessionResult.data.expiresIn,
          ...sessionResult.data.user
        });
        console.log('[AUTH_TEST] Session endpoint successful, token stored in Redux');
      }
    } catch (error: any) {
      console.error('[AUTH_TEST] Session endpoint test failed:', error);
      setSessionResponse({
        error: `Network error: ${error.message || 'Unknown error'}`
      });
    } finally {
      setLoading(prev => ({ ...prev, session: false }));
    }
  };

  // Debug helpers
  const clearAuthState = () => {
    console.log('[DEBUG] Clearing authentication test state');
    // Force re-render to update display
    setApiClientResponse(null);
    setTokenResponse(null);
    setCookieResponse(null);
    setSessionResponse(null);
  };

  const triggerTestRequest = () => {
    console.log('[DEBUG] Test requests use HttpOnly cookies only - no localStorage tokens');
    // Force re-render to update display
    setApiClientResponse(null);
    setTokenResponse(null);
  };

  const testSessionRefresh = () => {
    console.log('[DEBUG] Session refresh testing uses HttpOnly cookies - no localStorage manipulation needed');
    // Force re-render to update display
    setApiClientResponse(null);
    setTokenResponse(null);
  };

  const clearReduxToken = () => {
    console.log('[DEBUG] Clearing access token from Redux state');
    dispatch(clearAccessToken());
    setTokenResponse(null);
    setApiClientResponse(null);
  };

  const renderResponse = (response: AuthResponse | null, title: string) => {
    if (!response) return null;

    return (
      <Alert variant={response.error ? 'danger' : 'success'} className="mt-3">
        <Alert.Heading className="fs-6">{title} Response</Alert.Heading>
        <pre className="mb-0" style={{ fontSize: '0.85em', whiteSpace: 'pre-wrap' }}>
          {JSON.stringify(response, null, 2)}
        </pre>
      </Alert>
    );
  };

  return (
    <div className="container-fluid">
      <Row className="justify-content-center">
        <Col lg={10} xl={8}>
          <Card className="mb-4">
            <Card.Header className="bg-body-tertiary">
              <h4 className="mb-0 text-primary">Auth Endpoint Test Page</h4>
              <div className="mt-2">
                <p className="mb-1 text-muted">
                  Testing authentication endpoints with secure token delivery:
                </p>
                <div className="d-flex flex-wrap gap-2">
                  <span className="badge bg-primary">GET /v1/auth/operator/me</span>
                  <span className="badge bg-warning text-dark">GET /v1/auth/session</span>
                </div>
              </div>
            </Card.Header>
            <Card.Body>
              <Row>
                <Col md={3}>
                  <Card className="h-100">
                    <Card.Header>
                      <h6 className="mb-0">Test with Cookies</h6>
                      <small className="text-muted">GET /v1/auth/operator/me</small>
                      <div><small className="text-muted">Uses credentials: 'include'</small></div>
                    </Card.Header>
                    <Card.Body>
                      <p className="text-muted small">
                        This test will send the request with cookies included, relying on any
                        authentication cookies that might be set in the browser.
                      </p>
                      <Button
                        variant="primary"
                        onClick={testWithCookie}
                        disabled={loading.cookie}
                        className="w-100"
                      >
                        {loading.cookie ? 'Testing...' : 'Test with Cookie'}
                      </Button>
                      {renderResponse(cookieResponse, 'Cookie-based Auth')}
                    </Card.Body>
                  </Card>
                </Col>
                <Col md={3}>
                  <Card className="h-100">
                    <Card.Header>
                      <h6 className="mb-0">Test with Bearer Token</h6>
                      <small className="text-muted">GET /v1/auth/operator/me</small>
                      <div><small className="text-muted">Uses Authorization header from Redux</small></div>
                    </Card.Header>
                    <Card.Body>
                      <Alert variant="info" className="small">
                        <strong>Token Status:</strong> {accessToken ? '✅ Available' : '❌ Not Available'}<br/>
                        <small>Will test with {accessToken ? 'valid token' : 'invalid token to show error handling'}</small>
                      </Alert>
                      <Button
                        variant="primary"
                        onClick={testWithBearerToken}
                        disabled={loading.token}
                        className="w-100"
                      >
                        {loading.token ? 'Testing...' : 'Test with Bearer Token'}
                      </Button>
                      {renderResponse(tokenResponse, 'Bearer Token Auth')}
                    </Card.Body>
                  </Card>
                </Col>
                <Col md={3}>
                  <Card className="h-100">
                    <Card.Header className="bg-warning text-dark">
                      <h6 className="mb-0">Test Session Endpoint</h6>
                      <small className="text-dark">GET /v1/auth/session</small>
                      <div><small className="text-dark-50">OAuth token exchange</small></div>
                    </Card.Header>
                    <Card.Body>
                      <p className="text-muted small">
                        Tests the new <code>/auth/session</code> endpoint that exchanges refresh token
                        from cookie for access token stored in Redux.
                      </p>
                      <Button
                        variant="warning"
                        onClick={testSessionEndpoint}
                        disabled={loading.session}
                        className="w-100"
                      >
                        {loading.session ? 'Testing...' : 'Test Session Endpoint'}
                      </Button>
                      {renderResponse(sessionResponse, 'Session Endpoint')}
                    </Card.Body>
                  </Card>
                </Col>
                <Col md={3}>
                  <Card className="h-100">
                    <Card.Header className="bg-success text-white">
                      <h6 className="mb-0">Test with API Client</h6>
                      <small className="text-white">GET /v1/auth/operator/me</small>
                      <div><small className="text-white-50">Auto-refresh + Bearer tokens</small></div>
                    </Card.Header>
                    <Card.Body>
                      <p className="text-muted small">
                        Uses the API client with both HttpOnly cookies and Bearer tokens from Redux.
                        Demonstrates dual authentication support.
                      </p>
                      <Button
                        variant="success"
                        onClick={testWithApiClient}
                        disabled={loading.apiClient}
                        className="w-100"
                      >
                        {loading.apiClient ? 'Testing...' : 'Test with API Client'}
                      </Button>
                      {renderResponse(apiClientResponse, 'API Client (Hybrid Auth)')}
                    </Card.Body>
                  </Card>
                </Col>
              </Row>
            </Card.Body>
          </Card>

          <Card>
            <Card.Header>
              <h6 className="mb-0">Debug Controls & Information</h6>
            </Card.Header>
            <Card.Body>
              <Row>
                <Col md={6}>
                  <h6>Authentication Debug</h6>
                  <Alert variant="info" className="small mb-3">
                    <strong>New Architecture:</strong> Dual authentication with HttpOnly cookies (refresh) + Redux tokens (access).
                  </Alert>
                  <Alert variant={accessToken ? 'success' : 'secondary'} className="small mb-3">
                    <strong>Redux Token State:</strong><br/>
                    Access Token: {accessToken ? '✅ Present' : '❌ Not Available'}<br/>
                    {tokenExpiry && (
                      <>Expires: {new Date(tokenExpiry).toLocaleString()}<br/></>
                    )}
                    Status: {tokenExpiry && new Date(tokenExpiry) > new Date() ? '🟢 Valid' : '🔴 Expired/Missing'}
                  </Alert>
                  <div className="mb-3">
                    <h6 className="small text-muted mb-2">Test Controls:</h6>
                    <div className="d-flex flex-wrap gap-2 mb-2">
                      <Button variant="outline-warning" size="sm" onClick={clearAuthState} title="Clear all test response data">
                        Clear Test State
                      </Button>
                      <Button variant="outline-info" size="sm" onClick={triggerTestRequest} title="Reset and prepare for cookie-based authentication test">
                        Reset Cookie Test
                      </Button>
                      <Button variant="outline-primary" size="sm" onClick={testSessionEndpoint} title="Test the /auth/session endpoint to exchange refresh token for access token">
                        Test Session Endpoint
                      </Button>
                    </div>
                    <h6 className="small text-muted mb-2">Token Management:</h6>
                    <div className="d-flex flex-wrap gap-2">
                      <Button variant="outline-danger" size="sm" onClick={clearReduxToken} title="Clear access token from Redux state">
                        Clear Access Token
                      </Button>
                      <Button variant="outline-secondary" size="sm" onClick={testSessionRefresh} title="Test session refresh mechanism using HttpOnly cookies">
                        Test Session Refresh
                      </Button>
                    </div>
                  </div>
                </Col>
                <Col md={6}>
                  <h6>Test Information</h6>
                  <dl className="mb-0">
                    <dt>Backend URL:</dt>
                    <dd><code>{backendUrl}</code></dd>
                    <dt>Test Endpoints:</dt>
                    <dd><code>/v1/auth/operator/me</code> & <code>/v1/auth/session</code></dd>
                    <dt>Cookie Method:</dt>
                    <dd><small>HttpOnly cookies with <code>credentials: 'include'</code></small></dd>
                    <dt>Bearer Method:</dt>
                    <dd><small><strong>Active:</strong> Bearer tokens from Redux state (after OAuth)</small></dd>
                    <dt>Session Method:</dt>
                    <dd><small><strong>New:</strong> Cookie → Access token exchange via <code>/auth/session</code></small></dd>
                    <dt>API Client Method:</dt>
                    <dd><small><strong>Hybrid:</strong> Uses both cookies and Bearer tokens automatically</small></dd>
                    <dt>Architecture:</dt>
                    <dd><small>Secure token delivery: Cookies (refresh) + Redux (access)</small></dd>
                  </dl>
                </Col>
              </Row>
            </Card.Body>
          </Card>
        </Col>
      </Row>
    </div>
  );
};

export default AuthTestPage;
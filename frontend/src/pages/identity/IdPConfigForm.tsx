import { useEffect, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Col,
  Form,
  Row,
  Spinner,
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import {
  useDeleteIdPConfigMutation,
  useGetIdPConfigQuery,
  usePutIdPConfigMutation,
} from 'store/api/identityApi';
import type { IdPConfigPayload } from 'types/identity';

// Empty draft used for "create" mode and when the operator clicks Delete.
const emptyDraft: IdPConfigPayload = {
  displayName: '',
  issuerURL: '',
  clientId: '',
  clientSecret: '',
  redirectURL: '',
  scopes: ['openid', 'email', 'profile'],
  subClaim: '',
  emailClaim: '',
  nameClaim: '',
  enabled: true,
};

const IdPConfigForm: React.FC = () => {
  const { data, isLoading, error } = useGetIdPConfigQuery();
  const [putConfig, { isLoading: isSaving }] = usePutIdPConfigMutation();
  const [deleteConfig, { isLoading: isDeleting }] = useDeleteIdPConfigMutation();

  const [draft, setDraft] = useState<IdPConfigPayload>(emptyDraft);
  const [secretTouched, setSecretTouched] = useState(false);

  // Seed the draft from the backend once the config loads. The stored
  // clientSecret is always "***" on read, so we never populate the field —
  // the operator sees a "leave empty to preserve" placeholder instead and
  // only types a new secret when they want to replace it.
  useEffect(() => {
    if (data) {
      setDraft({
        displayName: data.displayName ?? '',
        issuerURL: data.issuerURL ?? '',
        clientId: data.clientId ?? '',
        clientSecret: '',
        redirectURL: data.redirectURL ?? '',
        scopes: data.scopes ?? ['openid', 'email', 'profile'],
        subClaim: data.subClaim ?? '',
        emailClaim: data.emailClaim ?? '',
        nameClaim: data.nameClaim ?? '',
        enabled: data.enabled ?? true,
      });
      setSecretTouched(false);
    } else if (data === null) {
      setDraft(emptyDraft);
      setSecretTouched(false);
    }
  }, [data]);

  const update = (patch: Partial<IdPConfigPayload>) =>
    setDraft((prev) => ({ ...prev, ...patch }));

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      // Preserve the stored secret unless the operator explicitly typed one —
      // the backend keeps the existing ciphertext when the field is empty.
      const payload: IdPConfigPayload = {
        ...draft,
        clientSecret: secretTouched ? draft.clientSecret : '',
      };
      await putConfig(payload).unwrap();
      toast.success('IdP configuration saved');
      setSecretTouched(false);
    } catch (err: unknown) {
      toast.error('Save failed: ' + extractError(err));
    }
  };

  const handleDelete = async () => {
    if (!confirm('Remove the OIDC configuration for this tenant?')) return;
    try {
      await deleteConfig().unwrap();
      toast.success('IdP configuration removed');
    } catch (err: unknown) {
      toast.error('Delete failed: ' + extractError(err));
    }
  };

  const existing = !!data;
  const hasSecret = existing && data?.clientSecret === '***';

  return (
    <Card className="shadow-none border mb-3">
      <Card.Header className="border-bottom border-200 px-4 py-3 d-flex align-items-center justify-content-between">
        <div>
          <h6 className="mb-1">
            <FontAwesomeIcon icon="key" className="me-2 text-primary" />
            OpenID Connect (BYO IdP)
          </h6>
          <p className="fs-11 mb-0 text-body-tertiary">
            Point users at your own IdP (Okta, Entra, Auth0, …). Orkestra
            redeems the ID token and mints its own session.
          </p>
        </div>
        {existing && (
          <Button
            variant="outline-danger"
            size="sm"
            onClick={handleDelete}
            disabled={isDeleting}
          >
            {isDeleting ? 'Removing…' : 'Remove configuration'}
          </Button>
        )}
      </Card.Header>
      <Card.Body>
        {isLoading && (
          <div className="text-center py-4">
            <Spinner animation="border" size="sm" />
          </div>
        )}
        {!isLoading && error && (
          <Alert variant="danger" className="fs-10 mb-0">
            Failed to load IdP configuration. You need the{' '}
            <code>tenant.update</code> permission on the current tenant to
            view or edit this page.
          </Alert>
        )}
        {!isLoading && !error && (
          <Form onSubmit={handleSubmit}>
            <Row className="g-3">
              <Col md={6}>
                <Form.Label className="fs-10 fw-semibold">Display name</Form.Label>
                <Form.Control
                  size="sm"
                  value={draft.displayName}
                  onChange={(e) => update({ displayName: e.target.value })}
                  placeholder="Sign in with Acme Corp"
                />
              </Col>
              <Col md={6} className="d-flex align-items-end">
                <Form.Check
                  type="switch"
                  id="identity-idp-enabled"
                  label={draft.enabled ? 'Enabled — users can sign in via this IdP' : 'Disabled — public /start endpoint returns 404'}
                  checked={draft.enabled}
                  onChange={(e) => update({ enabled: e.target.checked })}
                />
              </Col>

              <Col md={12}>
                <Form.Label className="fs-10 fw-semibold">
                  Issuer URL <span className="text-danger">*</span>
                </Form.Label>
                <Form.Control
                  size="sm"
                  required
                  value={draft.issuerURL}
                  onChange={(e) => update({ issuerURL: e.target.value })}
                  placeholder="https://auth.example.com"
                />
                <Form.Text muted>
                  OIDC discovery base URL — no trailing slash. Orkestra appends
                  <code className="mx-1">/.well-known/openid-configuration</code>
                  at login time.
                </Form.Text>
              </Col>

              <Col md={6}>
                <Form.Label className="fs-10 fw-semibold">
                  Client ID <span className="text-danger">*</span>
                </Form.Label>
                <Form.Control
                  size="sm"
                  required
                  value={draft.clientId}
                  onChange={(e) => update({ clientId: e.target.value })}
                />
              </Col>
              <Col md={6}>
                <Form.Label className="fs-10 fw-semibold">
                  Client secret {!hasSecret && <span className="text-danger">*</span>}
                </Form.Label>
                <Form.Control
                  size="sm"
                  type="password"
                  autoComplete="new-password"
                  value={draft.clientSecret ?? ''}
                  onChange={(e) => {
                    update({ clientSecret: e.target.value });
                    setSecretTouched(true);
                  }}
                  placeholder={
                    hasSecret
                      ? 'Leave empty to keep the stored value'
                      : 'Paste the client secret registered at the IdP'
                  }
                />
                {hasSecret && (
                  <Form.Text muted>
                    A secret is already stored. Typing in this field replaces
                    it; leaving it empty preserves the current value.
                  </Form.Text>
                )}
              </Col>

              <Col md={12}>
                <Form.Label className="fs-10 fw-semibold">
                  Redirect URL <span className="text-danger">*</span>
                </Form.Label>
                <Form.Control
                  size="sm"
                  required
                  value={draft.redirectURL}
                  onChange={(e) => update({ redirectURL: e.target.value })}
                  placeholder="https://app.orkestra.example/v1/identity/oidc/callback"
                />
                <Form.Text muted>
                  Must match the callback URL you registered at the IdP exactly.
                </Form.Text>
              </Col>

              <Col md={12}>
                <Form.Label className="fs-10 fw-semibold">Scopes</Form.Label>
                <Form.Control
                  size="sm"
                  value={(draft.scopes ?? []).join(' ')}
                  onChange={(e) =>
                    update({
                      scopes: e.target.value
                        .split(/\s+/)
                        .map((s) => s.trim())
                        .filter(Boolean),
                    })
                  }
                  placeholder="openid email profile"
                />
                <Form.Text muted>
                  Space-separated. Defaults to <code>openid email profile</code>.
                </Form.Text>
              </Col>

              <Col md={4}>
                <Form.Label className="fs-10 fw-semibold">Subject claim</Form.Label>
                <Form.Control
                  size="sm"
                  value={draft.subClaim ?? ''}
                  onChange={(e) => update({ subClaim: e.target.value })}
                  placeholder="sub"
                />
              </Col>
              <Col md={4}>
                <Form.Label className="fs-10 fw-semibold">Email claim</Form.Label>
                <Form.Control
                  size="sm"
                  value={draft.emailClaim ?? ''}
                  onChange={(e) => update({ emailClaim: e.target.value })}
                  placeholder="email"
                />
              </Col>
              <Col md={4}>
                <Form.Label className="fs-10 fw-semibold">Name claim</Form.Label>
                <Form.Control
                  size="sm"
                  value={draft.nameClaim ?? ''}
                  onChange={(e) => update({ nameClaim: e.target.value })}
                  placeholder="name"
                />
              </Col>
            </Row>

            <div className="d-flex justify-content-end mt-3">
              <Button
                type="submit"
                variant="primary"
                size="sm"
                disabled={isSaving}
              >
                {isSaving ? (
                  <>
                    <Spinner animation="border" size="sm" className="me-2" />
                    Saving…
                  </>
                ) : existing ? (
                  'Save changes'
                ) : (
                  'Create configuration'
                )}
              </Button>
            </div>
          </Form>
        )}
      </Card.Body>
    </Card>
  );
};

function extractError(err: unknown): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || 'unknown error';
  }
  return String(err);
}

export default IdPConfigForm;

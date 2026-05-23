import { useEffect, useState } from 'react';
import { Alert, Button, Card, Col, Form, Row, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import { Trans, useTranslation } from 'react-i18next';
import {
  useDeleteIdPConfigMutation,
  useGetIdPConfigQuery,
  usePutIdPConfigMutation
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
  enabled: true
};

const IdPConfigForm: React.FC = () => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useGetIdPConfigQuery();
  const [putConfig, { isLoading: isSaving }] = usePutIdPConfigMutation();
  const [deleteConfig, { isLoading: isDeleting }] =
    useDeleteIdPConfigMutation();

  // 404 is the happy "no config yet" path. Distinguish it from real
  // errors (403, 5xx, network) so we render the empty form rather than
  // the permission-denied message.
  const notFound =
    !!error &&
    typeof error === 'object' &&
    'status' in error &&
    (error as { status?: number | string }).status === 404;
  const realError = !!error && !notFound;

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
        enabled: data.enabled ?? true
      });
      setSecretTouched(false);
    } else if (notFound) {
      setDraft(emptyDraft);
      setSecretTouched(false);
    }
  }, [data, notFound]);

  const update = (patch: Partial<IdPConfigPayload>) =>
    setDraft(prev => ({ ...prev, ...patch }));

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      // Preserve the stored secret unless the operator explicitly typed one —
      // the backend keeps the existing ciphertext when the field is empty.
      const payload: IdPConfigPayload = {
        ...draft,
        clientSecret: secretTouched ? draft.clientSecret : ''
      };
      await putConfig(payload).unwrap();
      toast.success(t('auth.identity.idpConfig.toastSaved'));
      setSecretTouched(false);
    } catch (err: unknown) {
      toast.error(
        t('auth.identity.idpConfig.toastSaveFailed', {
          message: extractError(err, t('auth.identity.idpConfig.errorUnknown'))
        })
      );
    }
  };

  const handleDelete = async () => {
    if (!confirm(t('auth.identity.idpConfig.removeConfirm'))) return;
    try {
      await deleteConfig().unwrap();
      toast.success(t('auth.identity.idpConfig.toastRemoved'));
    } catch (err: unknown) {
      toast.error(
        t('auth.identity.idpConfig.toastRemoveFailed', {
          message: extractError(err, t('auth.identity.idpConfig.errorUnknown'))
        })
      );
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
            {t('auth.identity.idpConfig.headerTitle')}
          </h6>
          <p className="fs-11 mb-0 text-body-tertiary">
            {t('auth.identity.idpConfig.headerDescription')}
          </p>
        </div>
        {existing && (
          <Button
            variant="outline-danger"
            size="sm"
            onClick={handleDelete}
            disabled={isDeleting}
          >
            {isDeleting
              ? t('auth.identity.idpConfig.removingButton')
              : t('auth.identity.idpConfig.removeButton')}
          </Button>
        )}
      </Card.Header>
      <Card.Body>
        {isLoading && (
          <div className="text-center py-4">
            <Spinner animation="border" size="sm" />
          </div>
        )}
        {!isLoading && realError && (
          <Alert variant="danger" className="fs-10 mb-0">
            <Trans
              i18nKey="auth.identity.idpConfig.loadError"
              components={{ code: <code /> }}
            />
          </Alert>
        )}
        {!isLoading && !realError && (
          <Form onSubmit={handleSubmit}>
            <Row className="g-3">
              <Col md={6}>
                <Form.Label className="fs-10 fw-semibold">
                  {t('auth.identity.idpConfig.displayNameLabel')}
                </Form.Label>
                <Form.Control
                  size="sm"
                  value={draft.displayName}
                  onChange={e => update({ displayName: e.target.value })}
                  placeholder={t(
                    'auth.identity.idpConfig.displayNamePlaceholder'
                  )}
                />
              </Col>
              <Col md={6} className="d-flex align-items-end">
                <Form.Check
                  type="switch"
                  id="identity-idp-enabled"
                  label={
                    draft.enabled
                      ? t('auth.identity.idpConfig.enabledOn')
                      : t('auth.identity.idpConfig.enabledOff')
                  }
                  checked={draft.enabled}
                  onChange={e => update({ enabled: e.target.checked })}
                />
              </Col>

              <Col md={12}>
                <Form.Label className="fs-10 fw-semibold">
                  {t('auth.identity.idpConfig.issuerLabel')}{' '}
                  <span className="text-danger">*</span>
                </Form.Label>
                <Form.Control
                  size="sm"
                  required
                  value={draft.issuerURL}
                  onChange={e => update({ issuerURL: e.target.value })}
                  placeholder={t('auth.identity.idpConfig.issuerPlaceholder')}
                />
                <Form.Text muted>
                  <Trans
                    i18nKey="auth.identity.idpConfig.issuerHint"
                    components={{ code: <code className="mx-1" /> }}
                  />
                </Form.Text>
              </Col>

              <Col md={6}>
                <Form.Label className="fs-10 fw-semibold">
                  {t('auth.identity.idpConfig.clientIdLabel')}{' '}
                  <span className="text-danger">*</span>
                </Form.Label>
                <Form.Control
                  size="sm"
                  required
                  value={draft.clientId}
                  onChange={e => update({ clientId: e.target.value })}
                />
              </Col>
              <Col md={6}>
                <Form.Label className="fs-10 fw-semibold">
                  {t('auth.identity.idpConfig.clientSecretLabel')}{' '}
                  {!hasSecret && <span className="text-danger">*</span>}
                </Form.Label>
                <Form.Control
                  size="sm"
                  type="password"
                  autoComplete="new-password"
                  value={draft.clientSecret ?? ''}
                  onChange={e => {
                    update({ clientSecret: e.target.value });
                    setSecretTouched(true);
                  }}
                  placeholder={
                    hasSecret
                      ? t('auth.identity.idpConfig.clientSecretKeepPlaceholder')
                      : t('auth.identity.idpConfig.clientSecretNewPlaceholder')
                  }
                />
                {hasSecret && (
                  <Form.Text muted>
                    {t('auth.identity.idpConfig.clientSecretReplaceHint')}
                  </Form.Text>
                )}
              </Col>

              <Col md={12}>
                <Form.Label className="fs-10 fw-semibold">
                  {t('auth.identity.idpConfig.redirectUrlLabel')}{' '}
                  <span className="text-danger">*</span>
                </Form.Label>
                <Form.Control
                  size="sm"
                  required
                  value={draft.redirectURL}
                  onChange={e => update({ redirectURL: e.target.value })}
                  placeholder={t(
                    'auth.identity.idpConfig.redirectUrlPlaceholder'
                  )}
                />
                <Form.Text muted>
                  {t('auth.identity.idpConfig.redirectUrlHint')}
                </Form.Text>
              </Col>

              <Col md={12}>
                <Form.Label className="fs-10 fw-semibold">
                  {t('auth.identity.idpConfig.scopesLabel')}
                </Form.Label>
                <Form.Control
                  size="sm"
                  value={(draft.scopes ?? []).join(' ')}
                  onChange={e =>
                    update({
                      scopes: e.target.value
                        .split(/\s+/)
                        .map(s => s.trim())
                        .filter(Boolean)
                    })
                  }
                  placeholder={t('auth.identity.idpConfig.scopesPlaceholder')}
                />
                <Form.Text muted>
                  <Trans
                    i18nKey="auth.identity.idpConfig.scopesHint"
                    components={{ code: <code /> }}
                  />
                </Form.Text>
              </Col>

              <Col md={4}>
                <Form.Label className="fs-10 fw-semibold">
                  {t('auth.identity.idpConfig.subClaimLabel')}
                </Form.Label>
                <Form.Control
                  size="sm"
                  value={draft.subClaim ?? ''}
                  onChange={e => update({ subClaim: e.target.value })}
                  placeholder={t('auth.identity.idpConfig.subClaimPlaceholder')}
                />
              </Col>
              <Col md={4}>
                <Form.Label className="fs-10 fw-semibold">
                  {t('auth.identity.idpConfig.emailClaimLabel')}
                </Form.Label>
                <Form.Control
                  size="sm"
                  value={draft.emailClaim ?? ''}
                  onChange={e => update({ emailClaim: e.target.value })}
                  placeholder={t(
                    'auth.identity.idpConfig.emailClaimPlaceholder'
                  )}
                />
              </Col>
              <Col md={4}>
                <Form.Label className="fs-10 fw-semibold">
                  {t('auth.identity.idpConfig.nameClaimLabel')}
                </Form.Label>
                <Form.Control
                  size="sm"
                  value={draft.nameClaim ?? ''}
                  onChange={e => update({ nameClaim: e.target.value })}
                  placeholder={t(
                    'auth.identity.idpConfig.nameClaimPlaceholder'
                  )}
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
                    {t('auth.identity.idpConfig.submitSaving')}
                  </>
                ) : existing ? (
                  t('auth.identity.idpConfig.submitSave')
                ) : (
                  t('auth.identity.idpConfig.submitCreate')
                )}
              </Button>
            </div>
          </Form>
        )}
      </Card.Body>
    </Card>
  );
};

function extractError(err: unknown, fallback: string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || fallback;
  }
  return String(err);
}

export default IdPConfigForm;

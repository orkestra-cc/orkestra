import { useState } from 'react';
import { Alert, Button, Card, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import { Trans, useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import {
  useGetScimTokenStatusQuery,
  useRotateScimTokenMutation
} from 'store/api/identityApi';
import type { ScimTokenRotated } from 'types/identity';

// SCIM bearer token rotate + status. The backend returns the raw token
// exactly once, at rotation time — this component keeps the raw token in
// state only until the operator closes the reveal banner, at which point
// it is stripped and the only recovery path is another rotation.
const ScimTokenSection: React.FC = () => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useGetScimTokenStatusQuery();
  const [rotate, { isLoading: isRotating }] = useRotateScimTokenMutation();
  const [fresh, setFresh] = useState<ScimTokenRotated | null>(null);
  const [confirming, setConfirming] = useState(false);

  const onRotate = async () => {
    setConfirming(false);
    try {
      const res = await rotate().unwrap();
      setFresh(res);
      toast.success(t('auth.identity.scimToken.toastRotated'));
    } catch (err: unknown) {
      toast.error(
        t('auth.identity.scimToken.toastRotateFailed', {
          message: extractError(err, t('auth.identity.scimToken.errorUnknown'))
        })
      );
    }
  };

  const copyToken = () => {
    if (!fresh?.token) return;
    navigator.clipboard.writeText(fresh.token);
    toast.success(t('auth.identity.scimToken.toastCopied'));
  };

  const exists = !!data?.exists;

  return (
    <Card className="shadow-none border">
      <Card.Header className="border-bottom border-200 px-4 py-3 d-flex align-items-center justify-content-between">
        <div>
          <h6 className="mb-1">
            <FontAwesomeIcon
              icon="exchange-alt"
              className="me-2 text-primary"
            />
            {t('auth.identity.scimToken.headerTitle')}
          </h6>
          <p className="fs-11 mb-0 text-body-tertiary">
            <Trans
              i18nKey="auth.identity.scimToken.headerDescription"
              components={{ code: <code /> }}
            />
          </p>
        </div>
        {!confirming ? (
          <Button
            variant={exists ? 'outline-warning' : 'primary'}
            size="sm"
            onClick={() => setConfirming(true)}
            disabled={isRotating}
          >
            {exists
              ? t('auth.identity.scimToken.rotateButton')
              : t('auth.identity.scimToken.generateButton')}
          </Button>
        ) : (
          <div className="d-flex gap-2">
            <Button
              variant="outline-secondary"
              size="sm"
              onClick={() => setConfirming(false)}
              disabled={isRotating}
            >
              {t('auth.identity.scimToken.cancelButton')}
            </Button>
            <Button
              variant="danger"
              size="sm"
              onClick={onRotate}
              disabled={isRotating}
            >
              {isRotating ? (
                <>
                  <Spinner animation="border" size="sm" className="me-2" />
                  {t('auth.identity.scimToken.rotatingButton')}
                </>
              ) : exists ? (
                t('auth.identity.scimToken.confirmRotateExisting')
              ) : (
                t('auth.identity.scimToken.confirmGenerate')
              )}
            </Button>
          </div>
        )}
      </Card.Header>
      <Card.Body>
        {fresh?.token && (
          <Alert
            variant="warning"
            dismissible
            onClose={() => setFresh(null)}
            className="fs-10"
          >
            <strong>{t('auth.identity.scimToken.revealHeading')}</strong>
            <div className="d-flex align-items-center gap-2 mt-2">
              <code className="flex-grow-1 fs-11 text-break">
                {fresh.token}
              </code>
              <Button size="sm" variant="outline-dark" onClick={copyToken}>
                <FontAwesomeIcon icon="copy" className="me-1" />
                {t('auth.identity.scimToken.copyButton')}
              </Button>
            </div>
            <div className="fs-11 text-body-tertiary mt-2">
              {t('auth.identity.scimToken.revealFooter', {
                date: new Date(fresh.createdAt).toLocaleString()
              })}
            </div>
          </Alert>
        )}

        {isLoading && (
          <div className="text-center py-3">
            <Spinner animation="border" size="sm" />
          </div>
        )}

        {!isLoading && error && (
          <Alert variant="danger" className="fs-10 mb-0">
            {t('auth.identity.scimToken.loadError')}
          </Alert>
        )}

        {!isLoading && !error && (
          <div className="fs-10">
            {exists ? (
              <>
                <SubtleBadge bg="success" pill className="me-2">
                  {t('auth.identity.scimToken.statusActive')}
                </SubtleBadge>
                <span className="text-body-secondary">
                  <Trans
                    i18nKey="auth.identity.scimToken.statusActiveText"
                    values={{
                      uuid: data?.uuid ?? '',
                      date: data?.createdAt
                        ? new Date(data.createdAt).toLocaleString()
                        : t('auth.identity.scimToken.createdAtUnknown')
                    }}
                    components={{ code: <code className="fs-11" /> }}
                  />
                </span>
              </>
            ) : (
              <>
                <SubtleBadge bg="secondary" pill className="me-2">
                  {t('auth.identity.scimToken.statusNone')}
                </SubtleBadge>
                <span className="text-body-secondary">
                  <Trans
                    i18nKey="auth.identity.scimToken.statusNoneText"
                    components={{ code: <code /> }}
                  />
                </span>
              </>
            )}
          </div>
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

export default ScimTokenSection;

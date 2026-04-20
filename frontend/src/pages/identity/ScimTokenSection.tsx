import { useState } from 'react';
import { Alert, Button, Card, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import SubtleBadge from 'components/common/SubtleBadge';
import {
  useGetScimTokenStatusQuery,
  useRotateScimTokenMutation,
} from 'store/api/identityApi';
import type { ScimTokenRotated } from 'types/identity';

// SCIM bearer token rotate + status. The backend returns the raw token
// exactly once, at rotation time — this component keeps the raw token in
// state only until the operator closes the reveal banner, at which point
// it is stripped and the only recovery path is another rotation.
const ScimTokenSection: React.FC = () => {
  const { data, isLoading, error } = useGetScimTokenStatusQuery();
  const [rotate, { isLoading: isRotating }] = useRotateScimTokenMutation();
  const [fresh, setFresh] = useState<ScimTokenRotated | null>(null);
  const [confirming, setConfirming] = useState(false);

  const onRotate = async () => {
    setConfirming(false);
    try {
      const res = await rotate().unwrap();
      setFresh(res);
      toast.success('SCIM token rotated');
    } catch (err: unknown) {
      toast.error('Rotate failed: ' + extractError(err));
    }
  };

  const copyToken = () => {
    if (!fresh?.token) return;
    navigator.clipboard.writeText(fresh.token);
    toast.success('Token copied to clipboard');
  };

  const exists = !!data?.exists;

  return (
    <Card className="shadow-none border">
      <Card.Header className="border-bottom border-200 px-4 py-3 d-flex align-items-center justify-content-between">
        <div>
          <h6 className="mb-1">
            <FontAwesomeIcon icon="exchange-alt" className="me-2 text-primary" />
            SCIM 2.0 bearer token
          </h6>
          <p className="fs-11 mb-0 text-body-tertiary">
            The IdP uses this token to call <code>/scim/v2/*</code> for user
            provisioning. Rotating reveals a fresh value exactly once.
          </p>
        </div>
        {!confirming ? (
          <Button
            variant={exists ? 'outline-warning' : 'primary'}
            size="sm"
            onClick={() => setConfirming(true)}
            disabled={isRotating}
          >
            {exists ? 'Rotate token' : 'Generate token'}
          </Button>
        ) : (
          <div className="d-flex gap-2">
            <Button
              variant="outline-secondary"
              size="sm"
              onClick={() => setConfirming(false)}
              disabled={isRotating}
            >
              Cancel
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
                  Rotating…
                </>
              ) : exists ? (
                'Revoke & rotate'
              ) : (
                'Confirm generate'
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
            <strong>Copy this token now — it cannot be shown again.</strong>
            <div className="d-flex align-items-center gap-2 mt-2">
              <code className="flex-grow-1 fs-11 text-break">{fresh.token}</code>
              <Button size="sm" variant="outline-dark" onClick={copyToken}>
                <FontAwesomeIcon icon="copy" className="me-1" />
                Copy
              </Button>
            </div>
            <div className="fs-11 text-body-tertiary mt-2">
              Rotated at {new Date(fresh.createdAt).toLocaleString()}.
              Store it in the IdP's SCIM configuration. Closing this alert
              strips the value from the browser — another rotation is the
              only recovery path.
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
            Failed to load SCIM token status.
          </Alert>
        )}

        {!isLoading && !error && (
          <div className="fs-10">
            {exists ? (
              <>
                <SubtleBadge bg="success" pill className="me-2">
                  active
                </SubtleBadge>
                <span className="text-body-secondary">
                  Token <code className="fs-11">{data?.uuid}</code> created at{' '}
                  {data?.createdAt
                    ? new Date(data.createdAt).toLocaleString()
                    : '—'}
                  . The raw value is no longer recoverable — rotate to mint a
                  new one.
                </span>
              </>
            ) : (
              <>
                <SubtleBadge bg="secondary" pill className="me-2">
                  none
                </SubtleBadge>
                <span className="text-body-secondary">
                  No SCIM token configured for this tenant. Generate one to
                  allow the IdP to call <code>/scim/v2/*</code>.
                </span>
              </>
            )}
          </div>
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

export default ScimTokenSection;

import { useState } from 'react';
import { Alert, Button, Card, Modal, Spinner, Table } from 'react-bootstrap';
import {
  useListTrustedDevicesQuery,
  useRevokeAllTrustedDevicesMutation,
  useRevokeTrustedDeviceMutation,
} from 'store/api/deviceTrustApi';

// TrustedDevicesTab shows the "remember this device 30d" grants the
// user holds. Each grant lets the user skip the MFA prompt on the
// listed device for 30 days; revoking forces MFA on the next login.
const TrustedDevicesTab = () => {
  const { data, isLoading, isFetching } = useListTrustedDevicesQuery();
  const [revokeOne, { isLoading: revokingOne }] = useRevokeTrustedDeviceMutation();
  const [revokeAll, { isLoading: revokingAll }] = useRevokeAllTrustedDevicesMutation();
  const [showRevokeAll, setShowRevokeAll] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" size="sm" />
      </div>
    );
  }

  const devices = data?.devices ?? [];

  const onRevoke = async (deviceId: string) => {
    setError(null);
    try {
      await revokeOne({ deviceId }).unwrap();
    } catch (err: unknown) {
      const e = err as { data?: { detail?: string; title?: string } };
      setError(e?.data?.detail || e?.data?.title || 'Failed to revoke trust.');
    }
  };

  const onConfirmRevokeAll = async () => {
    setError(null);
    try {
      await revokeAll().unwrap();
      setShowRevokeAll(false);
    } catch (err: unknown) {
      const e = err as { data?: { detail?: string; title?: string } };
      setError(e?.data?.detail || e?.data?.title || 'Failed to revoke trusts.');
    }
  };

  return (
    <>
      <Card className="shadow-none border">
        <Card.Header className="d-flex justify-content-between align-items-center flex-wrap gap-2">
          <Card.Title as="h5" className="mb-0">
            Trusted devices
          </Card.Title>
          <Button
            variant="outline-danger"
            size="sm"
            onClick={() => setShowRevokeAll(true)}
            disabled={devices.length === 0 || revokingAll}
          >
            Revoke all
          </Button>
        </Card.Header>
        <Card.Body>
          {error && (
            <Alert variant="danger" className="fs-10">
              {error}
            </Alert>
          )}
          {devices.length === 0 ? (
            <p className="fs-10 text-muted mb-0">
              No devices remembered. Trust grants expire after 30 days.
            </p>
          ) : (
            <Table responsive size="sm" className="mb-0 align-middle">
              <thead>
                <tr>
                  <th>Device</th>
                  <th>Platform</th>
                  <th>Trusted since</th>
                  <th>Expires</th>
                  <th className="text-end">Actions</th>
                </tr>
              </thead>
              <tbody>
                {devices.map((d) => (
                  <tr key={d.uuid}>
                    <td>{d.deviceName || d.deviceId}</td>
                    <td className="fs-10 text-muted">{d.platform || '—'}</td>
                    <td className="fs-10 text-muted">
                      {new Date(d.trustedAt).toLocaleDateString()}
                    </td>
                    <td className="fs-10 text-muted">
                      {new Date(d.trustedUntil).toLocaleDateString()}
                    </td>
                    <td className="text-end">
                      <Button
                        variant="outline-danger"
                        size="sm"
                        onClick={() => onRevoke(d.deviceId)}
                        disabled={revokingOne || isFetching}
                      >
                        Revoke
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card.Body>
      </Card>

      <Modal show={showRevokeAll} onHide={() => setShowRevokeAll(false)} centered>
        <Modal.Header closeButton>
          <Modal.Title>Revoke all trusted devices</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          Every device will require MFA on its next login. Continue?
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowRevokeAll(false)}>
            Cancel
          </Button>
          <Button variant="danger" onClick={onConfirmRevokeAll} disabled={revokingAll}>
            {revokingAll ? 'Revoking…' : 'Revoke all'}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default TrustedDevicesTab;

import { useCallback, useState } from 'react';
import { Button, Card, Col, Row, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import { useNavigate } from 'react-router';
import { useDispatch } from 'react-redux';
import { useAuth } from 'hooks/auth/useAuthRTK';
import { logout as logoutAction } from 'store/slices/authSlice';
import {
  useExportMyDataMutation,
  useEraseMyDataMutation,
} from 'store/api/complianceApi';
import EraseAccountModal from './EraseAccountModal';

// /user/privacy is the GDPR self-service page. Any authenticated user can
// reach it — access is gated by ProtectedRoute in the module manifest, the
// backend endpoints enforce userUUID scoping via the JWT. Matching backend:
// backend/internal/addons/compliance/handlers/me_handler.go.
const UserPrivacyPage: React.FC = () => {
  const navigate = useNavigate();
  const dispatch = useDispatch();
  const { currentUser } = useAuth();

  const [exportData, { isLoading: isExporting }] = useExportMyDataMutation();
  const [eraseData, { isLoading: isErasing }] = useEraseMyDataMutation();
  const [showEraseModal, setShowEraseModal] = useState(false);

  const userEmail = currentUser?.email ?? '';
  const userId = currentUser?.id ?? 'user';

  const handleExport = useCallback(async () => {
    try {
      const bundle = await exportData().unwrap();
      const filename = `orkestra-data-export-${userId}-${Date.now()}.json`;
      const blob = new Blob([JSON.stringify(bundle, null, 2)], {
        type: 'application/json',
      });
      const url = URL.createObjectURL(blob);
      // Trigger download via synthetic anchor — works without navigation.
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      toast.success(
        `Exported data from ${bundle.producers.length} module${bundle.producers.length === 1 ? '' : 's'}.`,
      );
      if (bundle.errors && Object.keys(bundle.errors).length > 0) {
        toast.warn(
          'Some modules failed to export. Open the file to review the errors section.',
        );
      }
    } catch (err) {
      toast.error('Failed to export your data. Please try again later.');
      console.error('DSR export failed', err);
    }
  }, [exportData, userId]);

  const handleErase = useCallback(async () => {
    try {
      const result = await eraseData().unwrap();
      toast.success(
        `Account erased. ${result.totalRows} row${result.totalRows === 1 ? '' : 's'} removed.`,
      );
      // Wipe the client auth state so the 15-minute access token is not
      // reused by accident, then bounce to /login. The refresh-token path
      // is already dead on the backend.
      dispatch(logoutAction());
      setShowEraseModal(false);
      navigate('/login', { replace: true });
    } catch (err) {
      toast.error('Failed to erase your account. Please try again later.');
      console.error('DSR erase failed', err);
    }
  }, [dispatch, eraseData, navigate]);

  return (
    <>
      <Row className="g-3 mb-3">
        <Col>
          <Card className="shadow-none border">
            <Card.Body>
              <h5 className="mb-1">
                <FontAwesomeIcon icon="user-shield" className="me-2 text-primary" />
                Privacy & your data
              </h5>
              <p className="fs-10 mb-0 text-body-secondary">
                Under the GDPR you have the right to access a copy of your
                personal data (<em>right of access</em>) and to request its
                deletion (<em>right to erasure</em>). Both flows run against
                every module that holds data about you.
              </p>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      <Row className="g-3">
        <Col lg={6}>
          <Card className="h-100 shadow-none border">
            <Card.Body className="d-flex flex-column">
              <div className="mb-3">
                <h6 className="mb-1">
                  <FontAwesomeIcon icon="file-download" className="me-2 text-info" />
                  Export your data
                </h6>
                <p className="fs-10 mb-0 text-body-secondary">
                  Downloads a JSON bundle with the personal data every module
                  holds for your account. Safe to retry — the endpoint is
                  read-only and does not mutate state.
                </p>
              </div>
              <div className="mt-auto">
                <Button
                  variant="primary"
                  size="sm"
                  onClick={handleExport}
                  disabled={isExporting}
                >
                  {isExporting ? (
                    <>
                      <Spinner animation="border" size="sm" className="me-2" />
                      Gathering your data…
                    </>
                  ) : (
                    <>
                      <FontAwesomeIcon icon="file-download" className="me-2" />
                      Download JSON bundle
                    </>
                  )}
                </Button>
              </div>
            </Card.Body>
          </Card>
        </Col>

        <Col lg={6}>
          <Card className="h-100 shadow-none border border-danger-subtle">
            <Card.Body className="d-flex flex-column">
              <div className="mb-3">
                <h6 className="mb-1 text-danger">
                  <FontAwesomeIcon icon="trash" className="me-2" />
                  Delete your account
                </h6>
                <p className="fs-10 mb-0 text-body-secondary">
                  Permanently removes your personal data across every module.
                  You will be signed out immediately. There is no undo and no
                  recovery path.
                </p>
              </div>
              <div className="mt-auto">
                <Button
                  variant="outline-danger"
                  size="sm"
                  onClick={() => setShowEraseModal(true)}
                  disabled={!userEmail || isErasing}
                >
                  {isErasing ? (
                    <>
                      <Spinner animation="border" size="sm" className="me-2" />
                      Erasing…
                    </>
                  ) : (
                    <>Delete my account</>
                  )}
                </Button>
                {!userEmail && (
                  <p className="fs-11 text-body-tertiary mt-2 mb-0">
                    Loading your profile…
                  </p>
                )}
              </div>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      <EraseAccountModal
        show={showEraseModal}
        onHide={() => setShowEraseModal(false)}
        onConfirm={handleErase}
        userEmail={userEmail}
        isProcessing={isErasing}
      />
    </>
  );
};

export default UserPrivacyPage;

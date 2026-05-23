import { useCallback, useState } from 'react';
import { Button, Card, Col, Row, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import { useNavigate } from 'react-router';
import { useDispatch } from 'react-redux';
import { useAuth } from 'hooks/auth/useAuthRTK';
import { logout as logoutAction } from 'store/slices/authSlice';
import {
  useExportMyDataMutation,
  useEraseMyDataMutation
} from 'store/api/complianceApi';
import EraseAccountModal from './EraseAccountModal';

// /user/privacy is the GDPR self-service page. Any authenticated user can
// reach it — access is gated by ProtectedRoute in the module manifest, the
// backend endpoints enforce userUUID scoping via the JWT. Matching backend:
// backend/internal/addons/compliance/handlers/me_handler.go.
const UserPrivacyPage: React.FC = () => {
  const { t } = useTranslation();
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
      const filename = t('userPrivacy.export.filename', {
        user: userId,
        ts: Date.now()
      });
      const blob = new Blob([JSON.stringify(bundle, null, 2)], {
        type: 'application/json'
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
        t(
          bundle.producers.length === 1
            ? 'userPrivacy.export.toastSuccessOne'
            : 'userPrivacy.export.toastSuccessOther',
          { count: bundle.producers.length }
        )
      );
      if (bundle.errors && Object.keys(bundle.errors).length > 0) {
        toast.warn(t('userPrivacy.export.toastPartial'));
      }
    } catch (err) {
      toast.error(t('userPrivacy.export.toastFailure'));
      console.error('DSR export failed', err);
    }
  }, [exportData, userId, t]);

  const handleErase = useCallback(async () => {
    try {
      const result = await eraseData().unwrap();
      toast.success(
        t(
          result.totalRows === 1
            ? 'userPrivacy.erase.toastSuccessOne'
            : 'userPrivacy.erase.toastSuccessOther',
          { count: result.totalRows }
        )
      );
      // Wipe the client auth state so the 15-minute access token is not
      // reused by accident, then bounce to /login. The refresh-token path
      // is already dead on the backend.
      dispatch(logoutAction());
      setShowEraseModal(false);
      navigate('/login', { replace: true });
    } catch (err) {
      toast.error(t('userPrivacy.erase.toastFailure'));
      console.error('DSR erase failed', err);
    }
  }, [dispatch, eraseData, navigate, t]);

  return (
    <>
      <Row className="g-3 mb-3">
        <Col>
          <Card className="shadow-none border">
            <Card.Body>
              <h5 className="mb-1">
                <FontAwesomeIcon
                  icon="user-shield"
                  className="me-2 text-primary"
                />
                {t('userPrivacy.intro.title')}
              </h5>
              <p className="fs-10 mb-0 text-body-secondary">
                <Trans
                  i18nKey="userPrivacy.intro.body"
                  components={{ em: <em /> }}
                />
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
                  <FontAwesomeIcon
                    icon="file-download"
                    className="me-2 text-info"
                  />
                  {t('userPrivacy.export.title')}
                </h6>
                <p className="fs-10 mb-0 text-body-secondary">
                  {t('userPrivacy.export.description')}
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
                      {t('userPrivacy.export.submitting')}
                    </>
                  ) : (
                    <>
                      <FontAwesomeIcon icon="file-download" className="me-2" />
                      {t('userPrivacy.export.button')}
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
                  {t('userPrivacy.erase.title')}
                </h6>
                <p className="fs-10 mb-0 text-body-secondary">
                  {t('userPrivacy.erase.description')}
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
                      {t('userPrivacy.erase.submitting')}
                    </>
                  ) : (
                    <>{t('userPrivacy.erase.button')}</>
                  )}
                </Button>
                {!userEmail && (
                  <p className="fs-11 text-body-tertiary mt-2 mb-0">
                    {t('userPrivacy.erase.loadingProfile')}
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

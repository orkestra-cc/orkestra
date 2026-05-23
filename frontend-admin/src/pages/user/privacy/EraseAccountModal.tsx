import { useEffect, useState } from 'react';
import { Button, Form, Modal, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';

interface Props {
  show: boolean;
  onHide: () => void;
  onConfirm: () => Promise<void> | void;
  userEmail: string;
  isProcessing: boolean;
}

// Triple-confirm erase flow. Three distinct states so there is no single
// slip that can wipe a user's data — the modal deliberately gets more
// hostile-looking with each step to disrupt muscle memory.
type Stage = 'warn' | 'type-email' | 'final';

const EraseAccountModal: React.FC<Props> = ({
  show,
  onHide,
  onConfirm,
  userEmail,
  isProcessing
}) => {
  const { t } = useTranslation();
  const [stage, setStage] = useState<Stage>('warn');
  const [typed, setTyped] = useState('');

  useEffect(() => {
    if (show) {
      setStage('warn');
      setTyped('');
    }
  }, [show]);

  const emailMatches =
    typed.trim().toLowerCase() === userEmail.trim().toLowerCase();

  const handleClose = () => {
    if (isProcessing) return;
    onHide();
  };

  return (
    <Modal
      show={show}
      onHide={handleClose}
      centered
      backdrop={isProcessing ? 'static' : true}
    >
      <Modal.Header closeButton={!isProcessing} className="bg-danger-subtle">
        <Modal.Title className="fs-8 text-danger">
          <FontAwesomeIcon icon="trash" className="me-2" />
          {t('userPrivacy.modal.title')}
        </Modal.Title>
      </Modal.Header>

      {stage === 'warn' && (
        <>
          <Modal.Body className="fs-10">
            <p className="mb-3">
              <Trans
                i18nKey="userPrivacy.modal.warn.intro"
                components={{ strong: <strong /> }}
              />
            </p>
            <ul className="mb-3">
              <li>{t('userPrivacy.modal.warn.bullet1')}</li>
              <li>{t('userPrivacy.modal.warn.bullet2')}</li>
              <li>
                <Trans
                  i18nKey="userPrivacy.modal.warn.bullet3"
                  components={{ em: <em /> }}
                />
              </li>
              <li>{t('userPrivacy.modal.warn.bullet4')}</li>
            </ul>
            <p className="mb-0 text-body-secondary">
              <Trans
                i18nKey="userPrivacy.modal.warn.stopNotifications"
                components={{ strong: <strong /> }}
              />
            </p>
          </Modal.Body>
          <Modal.Footer>
            <Button variant="outline-secondary" onClick={handleClose}>
              {t('userPrivacy.modal.cancel')}
            </Button>
            <Button variant="danger" onClick={() => setStage('type-email')}>
              {t('userPrivacy.modal.continue')}
            </Button>
          </Modal.Footer>
        </>
      )}

      {stage === 'type-email' && (
        <>
          <Modal.Body className="fs-10">
            <p className="mb-2">{t('userPrivacy.modal.typeEmail.prompt')}</p>
            <p className="fs-11 text-body-tertiary mb-3">
              <code>{userEmail}</code>
            </p>
            <Form.Control
              autoFocus
              type="email"
              value={typed}
              onChange={e => setTyped(e.target.value)}
              placeholder={t('userPrivacy.modal.typeEmail.placeholder')}
              isInvalid={typed.length > 0 && !emailMatches}
            />
            <Form.Control.Feedback type="invalid">
              {t('userPrivacy.modal.typeEmail.feedback')}
            </Form.Control.Feedback>
          </Modal.Body>
          <Modal.Footer>
            <Button variant="outline-secondary" onClick={handleClose}>
              {t('userPrivacy.modal.cancel')}
            </Button>
            <Button
              variant="danger"
              disabled={!emailMatches}
              onClick={() => setStage('final')}
            >
              {t('userPrivacy.modal.continue')}
            </Button>
          </Modal.Footer>
        </>
      )}

      {stage === 'final' && (
        <>
          <Modal.Body className="fs-10">
            <div className="alert alert-danger mb-3">
              <FontAwesomeIcon icon="exclamation-triangle" className="me-2" />
              <Trans
                i18nKey="userPrivacy.modal.finalStep.alert"
                components={{ strong: <strong /> }}
              />
            </div>
            <p className="mb-0">{t('userPrivacy.modal.finalStep.body')}</p>
          </Modal.Body>
          <Modal.Footer>
            <Button
              variant="outline-secondary"
              onClick={handleClose}
              disabled={isProcessing}
            >
              {t('userPrivacy.modal.cancel')}
            </Button>
            <Button
              variant="danger"
              onClick={() => onConfirm()}
              disabled={isProcessing}
            >
              {isProcessing ? (
                <>
                  <Spinner animation="border" size="sm" className="me-2" />
                  {t('userPrivacy.erase.submitting')}
                </>
              ) : (
                <>{t('userPrivacy.modal.finalSubmit')}</>
              )}
            </Button>
          </Modal.Footer>
        </>
      )}
    </Modal>
  );
};

export default EraseAccountModal;

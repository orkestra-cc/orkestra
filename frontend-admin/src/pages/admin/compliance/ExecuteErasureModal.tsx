import { useEffect, useState } from 'react';
import { Button, Form, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

interface Props {
  show: boolean;
  busy: boolean;
  // Mode the request already carries; shown read-only and never sent.
  lockedMode?: string;
  onCancel: () => void;
  onConfirm: (mode?: string) => void;
}

// ExecuteErasureModal confirms an execution. A request with no locked mode
// asks for one (default hard delete); a locked one shows it read-only.
const ExecuteErasureModal = ({
  show,
  busy,
  lockedMode,
  onCancel,
  onConfirm
}: Props) => {
  const { t } = useTranslation();
  const [mode, setMode] = useState('hard_delete');
  useEffect(() => {
    if (show) setMode('hard_delete');
  }, [show]);
  return (
    <Modal show={show} onHide={onCancel} centered>
      <Modal.Header closeButton>
        <Modal.Title as="h5">
          {t('adminCompliance.erasure.execute_modal.title')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <Form.Group controlId="execute-erasure-mode">
          <Form.Label>
            {t('adminCompliance.erasure.execute_modal.mode')}
          </Form.Label>
          {lockedMode ? (
            <>
              <Form.Control
                readOnly
                value={t(`adminCompliance.erasure.mode.${lockedMode}`, {
                  defaultValue: lockedMode
                })}
              />
              <Form.Text muted>
                {t('adminCompliance.erasure.execute_modal.locked')}
              </Form.Text>
            </>
          ) : (
            <Form.Select value={mode} onChange={e => setMode(e.target.value)}>
              <option value="hard_delete">
                {t('adminCompliance.erasure.mode.hard_delete')}
              </option>
              <option value="anonymize">
                {t('adminCompliance.erasure.mode.anonymize')}
              </option>
            </Form.Select>
          )}
        </Form.Group>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="orkestra-default" onClick={onCancel} disabled={busy}>
          {t('adminCompliance.saveModal.cancel')}
        </Button>
        <Button
          variant="orkestra-danger"
          disabled={busy}
          onClick={() => onConfirm(lockedMode ? undefined : mode)}
        >
          {t('adminCompliance.erasure.execute_modal.submit')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

export default ExecuteErasureModal;

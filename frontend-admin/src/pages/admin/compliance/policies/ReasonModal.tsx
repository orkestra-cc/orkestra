import { useEffect } from 'react';
import { Button, Form, Modal } from 'react-bootstrap';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useTranslation } from 'react-i18next';

const schema = yup.object({ reason: yup.string().trim().required().max(500) });
type ReasonForm = yup.InferType<typeof schema>;

interface Props {
  show: boolean;
  title: string;
  body: string;
  confirmLabel: string;
  busy: boolean;
  onCancel: () => void;
  onConfirm: (reason: string) => void;
}

// ReasonModal confirms an irreversible action with a mandatory reason (spec D9).
const ReasonModal = ({
  show,
  title,
  body,
  confirmLabel,
  busy,
  onCancel,
  onConfirm
}: Props) => {
  const { t } = useTranslation();
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors }
  } = useForm<ReasonForm>({
    resolver: yupResolver(schema),
    defaultValues: { reason: '' }
  });
  useEffect(() => {
    if (show) reset({ reason: '' });
  }, [show, reset]);
  return (
    <Modal show={show} onHide={onCancel} centered>
      <Form noValidate onSubmit={handleSubmit(v => onConfirm(v.reason.trim()))}>
        <Modal.Header closeButton>
          <Modal.Title as="h5">{title}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <p className="fs-10">{body}</p>
          <Form.Group controlId="reason-modal-reason">
            <Form.Label>
              {t('adminCompliance.saveModal.reasonLabel')}
            </Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              isInvalid={!!errors.reason}
              {...register('reason')}
            />
            <Form.Control.Feedback type="invalid">
              {t('adminCompliance.issues.reason_required')}
            </Form.Control.Feedback>
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="orkestra-default" onClick={onCancel} disabled={busy}>
            {t('adminCompliance.saveModal.cancel')}
          </Button>
          <Button type="submit" variant="orkestra-danger" disabled={busy}>
            {confirmLabel}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default ReasonModal;

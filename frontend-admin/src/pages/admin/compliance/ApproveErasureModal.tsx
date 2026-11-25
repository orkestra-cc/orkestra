import { useEffect } from 'react';
import { Button, Form, Modal } from 'react-bootstrap';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useTranslation } from 'react-i18next';

const schema = yup.object({
  mode: yup.string().required(),
  note: yup.string().trim().required().max(500)
});
type ApproveForm = yup.InferType<typeof schema>;

interface Props {
  show: boolean;
  busy: boolean;
  // Mode already locked by a previous approval; the select is then fixed.
  lockedMode?: string;
  onCancel: () => void;
  onConfirm: (v: ApproveForm) => void;
}

// ApproveErasureModal records one approval with its mode and a mandatory note.
const ApproveErasureModal = ({
  show,
  busy,
  lockedMode,
  onCancel,
  onConfirm
}: Props) => {
  const { t } = useTranslation();
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors }
  } = useForm<ApproveForm>({
    resolver: yupResolver(schema),
    defaultValues: { mode: lockedMode ?? 'hard_delete', note: '' }
  });
  useEffect(() => {
    if (show) reset({ mode: lockedMode ?? 'hard_delete', note: '' });
  }, [show, lockedMode, reset]);
  return (
    <Modal show={show} onHide={onCancel} centered>
      <Form
        noValidate
        onSubmit={handleSubmit(v => onConfirm({ ...v, note: v.note.trim() }))}
      >
        <Modal.Header closeButton>
          <Modal.Title as="h5">
            {t('adminCompliance.erasure.approve.title')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <Form.Group className="mb-3" controlId="approve-erasure-mode">
            <Form.Label>{t('adminCompliance.erasure.open.mode')}</Form.Label>
            <Form.Select disabled={!!lockedMode} {...register('mode')}>
              <option value="hard_delete">
                {t('adminCompliance.erasure.mode.hard_delete')}
              </option>
              <option value="anonymize">
                {t('adminCompliance.erasure.mode.anonymize')}
              </option>
            </Form.Select>
          </Form.Group>
          <Form.Group controlId="approve-erasure-note">
            <Form.Label>{t('adminCompliance.erasure.approve.note')}</Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              isInvalid={!!errors.note}
              {...register('note')}
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
          <Button type="submit" variant="orkestra-primary" disabled={busy}>
            {t('adminCompliance.erasure.approve.submit')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default ApproveErasureModal;

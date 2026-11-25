import { useEffect } from 'react';
import { Button, Form, Modal } from 'react-bootstrap';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useTranslation } from 'react-i18next';
import { UUID_RE } from './legalHoldDates';

const schema = yup.object({
  userUuid: yup
    .string()
    .trim()
    .required()
    .test('uuid', 'invalid', v => UUID_RE.test(v ?? '')),
  mode: yup.string().required(),
  reason: yup.string().trim().required().max(500)
});
export type OpenErasureForm = yup.InferType<typeof schema>;

interface Props {
  show: boolean;
  busy: boolean;
  // Pre-filled subject (e.g. from the Subject tab); editable.
  defaultUserUuid?: string;
  onCancel: () => void;
  onSubmit: (v: OpenErasureForm) => void;
}

// OpenErasureRequestModal opens an erasure request on behalf of a subject.
const OpenErasureRequestModal = ({
  show,
  busy,
  defaultUserUuid = '',
  onCancel,
  onSubmit
}: Props) => {
  const { t } = useTranslation();
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors }
  } = useForm<OpenErasureForm>({
    resolver: yupResolver(schema),
    defaultValues: {
      userUuid: defaultUserUuid,
      mode: 'hard_delete',
      reason: ''
    }
  });
  useEffect(() => {
    if (show)
      reset({ userUuid: defaultUserUuid, mode: 'hard_delete', reason: '' });
  }, [show, defaultUserUuid, reset]);
  return (
    <Modal show={show} onHide={onCancel} centered>
      <Form
        noValidate
        onSubmit={handleSubmit(v =>
          onSubmit({
            // Lower-cased: the shared UUID pattern accepts either case, the
            // backend stores one.
            userUuid: v.userUuid.trim().toLowerCase(),
            mode: v.mode,
            reason: v.reason.trim()
          })
        )}
      >
        <Modal.Header closeButton>
          <Modal.Title as="h5">
            {t('adminCompliance.erasure.open.title')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <Form.Group className="mb-3" controlId="open-erasure-user">
            <Form.Label>{t('adminCompliance.erasure.open.subject')}</Form.Label>
            <Form.Control
              isInvalid={!!errors.userUuid}
              {...register('userUuid')}
            />
            <Form.Control.Feedback type="invalid">
              {t('adminCompliance.subject.invalidUuid')}
            </Form.Control.Feedback>
          </Form.Group>
          <Form.Group className="mb-3" controlId="open-erasure-mode">
            <Form.Label>{t('adminCompliance.erasure.open.mode')}</Form.Label>
            <Form.Select {...register('mode')}>
              <option value="hard_delete">
                {t('adminCompliance.erasure.mode.hard_delete')}
              </option>
              <option value="anonymize">
                {t('adminCompliance.erasure.mode.anonymize')}
              </option>
            </Form.Select>
          </Form.Group>
          <Form.Group controlId="open-erasure-reason">
            <Form.Label>{t('adminCompliance.erasure.open.reason')}</Form.Label>
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
          <Button type="submit" variant="orkestra-primary" disabled={busy}>
            {t('adminCompliance.erasure.open.submit')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default OpenErasureRequestModal;

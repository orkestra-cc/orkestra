import { useMemo } from 'react';
import { Button, Form, Modal, Spinner } from 'react-bootstrap';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';

// The schema depends on the live auth.pinLength, so it is built per length.
// The backend is authoritative (422 auth.pin_invalid / auth.pin_too_weak);
// this only spares a round trip for the obvious mistakes.
const makeSchema = (length: number, t: TFunction) =>
  yup.object({
    pin: yup
      .string()
      .required(t('pinMethod.setModal.errorRequired'))
      .matches(/^[0-9]+$/, t('pinMethod.setModal.errorDigits'))
      .length(length, t('pinMethod.setModal.errorLength', { count: length })),
    confirm: yup
      .string()
      .required(t('pinMethod.setModal.errorRequired'))
      .oneOf([yup.ref('pin')], t('pinMethod.setModal.errorMismatch'))
  });

type PinForm = yup.InferType<ReturnType<typeof makeSchema>>;

interface SetPinModalProps {
  show: boolean;
  length: number;
  replacing: boolean;
  busy: boolean;
  onHide: () => void;
  onSubmit: (pin: string) => Promise<boolean>;
}

const SetPinModal = ({
  show,
  length,
  replacing,
  busy,
  onHide,
  onSubmit
}: SetPinModalProps) => {
  const { t } = useTranslation();
  const schema = useMemo(() => makeSchema(length, t), [length, t]);
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors }
  } = useForm<PinForm>({
    resolver: yupResolver(schema),
    defaultValues: { pin: '', confirm: '' }
  });

  const close = () => {
    reset();
    onHide();
  };

  const submit = async (values: PinForm) => {
    if (await onSubmit(values.pin)) {
      reset();
    }
  };

  return (
    <Modal show={show} onHide={close} centered>
      <Form noValidate onSubmit={handleSubmit(submit)}>
        <Modal.Header closeButton>
          <Modal.Title>
            {t(
              replacing
                ? 'pinMethod.setModal.titleReplace'
                : 'pinMethod.setModal.titleSet'
            )}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <p className="fs-10 text-700">
            {t('pinMethod.setModal.intro', { count: length })}
          </p>
          <Form.Group className="mb-3" controlId="pin-new">
            <Form.Label>{t('pinMethod.setModal.labelPin')}</Form.Label>
            <Form.Control
              type="password"
              inputMode="numeric"
              autoComplete="new-password"
              maxLength={length}
              isInvalid={!!errors.pin}
              {...register('pin')}
            />
            <Form.Control.Feedback type="invalid">
              {errors.pin?.message}
            </Form.Control.Feedback>
          </Form.Group>
          <Form.Group controlId="pin-confirm">
            <Form.Label>{t('pinMethod.setModal.labelConfirm')}</Form.Label>
            <Form.Control
              type="password"
              inputMode="numeric"
              autoComplete="new-password"
              maxLength={length}
              isInvalid={!!errors.confirm}
              {...register('confirm')}
            />
            <Form.Control.Feedback type="invalid">
              {errors.confirm?.message}
            </Form.Control.Feedback>
          </Form.Group>
          {replacing && (
            <p className="fs-11 text-600 mt-3 mb-0">
              {t('pinMethod.setModal.replaceNote')}
            </p>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="orkestra-default" onClick={close} disabled={busy}>
            {t('pinMethod.cancel')}
          </Button>
          <Button variant="orkestra-primary" type="submit" disabled={busy}>
            {busy ? (
              <Spinner size="sm" animation="border" />
            ) : (
              t('pinMethod.setModal.submit')
            )}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default SetPinModal;

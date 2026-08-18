import { useEffect, useState } from 'react';
import { Button, Form, Modal, Spinner } from 'react-bootstrap';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import { useCreateServiceAccountMutation } from 'store/api/serviceAccountApi';
import SecretOnceDisplay from 'components/common/SecretOnceDisplay';
import type { ServiceAccountWithSecret } from 'types/serviceAccounts';

const schema = yup.object({
  name: yup.string().trim().required().max(100)
});

type FormData = yup.InferType<typeof schema>;

interface Props {
  show: boolean;
  onHide: () => void;
}

type Phase = 'form' | 'secret';

// baseApi already suppresses the generic 4xx toast for this mutation, so
// every failure path here is ours to translate. 409/422 get a page-local
// message; anything else falls back to the shared errors.<code> namespace
// (mirrors extractToastError in hooks/ui/useUserTable.tsx) and finally a
// generic label.
function extractError(err: unknown, t: (key: string) => string): string {
  const anyErr = err as {
    status?: number;
    data?: { code?: string; detail?: string };
  };
  if (anyErr?.status === 409) {
    return t('adminServiceAccounts.errors.duplicate');
  }
  if (anyErr?.status === 422) {
    return t('adminServiceAccounts.errors.validation');
  }
  if (anyErr?.data?.code) {
    const translated = t(`errors.${anyErr.data.code}`);
    if (translated && translated !== `errors.${anyErr.data.code}`) {
      return translated;
    }
  }
  if (anyErr?.data?.detail) {
    return anyErr.data.detail;
  }
  return t('adminServiceAccounts.errors.generic');
}

/**
 * Two phases in one modal:
 *   1. form — react-hook-form + yup, POSTs {name}.
 *   2. secret — the AccountWithSecret response is shown EXACTLY ONCE via
 *      SecretOnceDisplay. Closing is blocked until the operator acknowledges
 *      they saved it (mirrors MfaEnrollWizard's handleClose idiom — this is
 *      the only chance they get to see the client secret again).
 *
 * No step-up handling here: the baseApi interceptor + global StepUpModal
 * handle it automatically for any mutation that needs it.
 */
const CreateServiceAccountModal: React.FC<Props> = ({ show, onHide }) => {
  const { t } = useTranslation();
  const [createServiceAccount, { isLoading, reset: resetCreateMutation }] =
    useCreateServiceAccountMutation();
  const [phase, setPhase] = useState<Phase>('form');
  const [created, setCreated] = useState<ServiceAccountWithSecret | null>(null);
  const [ack, setAck] = useState(false);

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors }
  } = useForm<FormData>({ resolver: yupResolver(schema) });

  // Reset every piece of state whenever the modal opens or closes, so a
  // second create starts clean and a dismissed secret is never reachable
  // again from a stale render.
  useEffect(() => {
    if (show) return;
    setPhase('form');
    setCreated(null);
    setAck(false);
    reset({ name: '' });
    // Also clears the RTK Query mutation cache entry, so the plaintext
    // secret doesn't linger in the store's api.mutations slice after close.
    resetCreateMutation();
  }, [show, reset, resetCreateMutation]);

  const onSubmit = handleSubmit(async data => {
    try {
      const result = await createServiceAccount({ name: data.name }).unwrap();
      toast.success(
        t('adminServiceAccounts.createModal.successToast', {
          name: result.name
        })
      );
      setCreated(result);
      setPhase('secret');
    } catch (err) {
      toast.error(extractError(err, t));
    }
  });

  const handleClose = () => {
    if (phase === 'secret' && !ack) return;
    onHide();
  };

  return (
    <Modal show={show} onHide={handleClose} backdrop="static" centered>
      <Modal.Header closeButton={phase !== 'secret' || ack}>
        <Modal.Title>
          {phase === 'form'
            ? t('adminServiceAccounts.createModal.title')
            : t('adminServiceAccounts.createModal.secretTitle')}
        </Modal.Title>
      </Modal.Header>

      {phase === 'form' ? (
        <Form onSubmit={onSubmit} noValidate>
          <Modal.Body>
            <Form.Group controlId="sa-create-name">
              <Form.Label>
                {t('adminServiceAccounts.createModal.nameLabel')}
              </Form.Label>
              <Form.Control
                autoFocus
                isInvalid={!!errors.name}
                placeholder={t(
                  'adminServiceAccounts.createModal.namePlaceholder'
                )}
                {...register('name')}
              />
              <Form.Control.Feedback type="invalid">
                {errors.name?.type === 'max'
                  ? t('adminServiceAccounts.createModal.nameTooLong')
                  : t('adminServiceAccounts.createModal.nameRequired')}
              </Form.Control.Feedback>
            </Form.Group>
          </Modal.Body>
          <Modal.Footer>
            <Button
              variant="secondary"
              onClick={handleClose}
              disabled={isLoading}
            >
              {t('adminServiceAccounts.createModal.cancel')}
            </Button>
            <Button variant="primary" type="submit" disabled={isLoading}>
              {isLoading ? (
                <>
                  <Spinner size="sm" animation="border" className="me-2" />
                  {t('adminServiceAccounts.createModal.creating')}
                </>
              ) : (
                t('adminServiceAccounts.createModal.create')
              )}
            </Button>
          </Modal.Footer>
        </Form>
      ) : (
        <>
          <Modal.Body>
            {created && (
              <SecretOnceDisplay
                label={t('adminServiceAccounts.createModal.clientSecretLabel')}
                secret={created.clientSecret}
                secondaryLabel={t(
                  'adminServiceAccounts.createModal.clientIdLabel'
                )}
                secondaryValue={created.clientId}
                ack={ack}
                onAckChange={setAck}
              />
            )}
          </Modal.Body>
          <Modal.Footer>
            <Button variant="primary" disabled={!ack} onClick={onHide}>
              {t('adminServiceAccounts.createModal.done')}
            </Button>
          </Modal.Footer>
        </>
      )}
    </Modal>
  );
};

export default CreateServiceAccountModal;

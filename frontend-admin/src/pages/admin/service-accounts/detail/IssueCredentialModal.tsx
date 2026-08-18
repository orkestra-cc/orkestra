import { useEffect, useState } from 'react';
import { Button, Form, Modal, Spinner } from 'react-bootstrap';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import { useIssueCredentialMutation } from 'store/api/serviceAccountApi';
import SecretOnceDisplay from 'components/common/SecretOnceDisplay';
import type { CredentialWithSecret } from 'types/serviceAccounts';

const schema = yup.object({
  label: yup.string().trim().max(60)
});

type FormData = yup.InferType<typeof schema>;

interface Props {
  show: boolean;
  onHide: () => void;
  accountId: string;
}

type Phase = 'form' | 'secret';

// Mirrors CreateServiceAccountModal's two-phase form→secret flow (Task 4):
//   1. form — react-hook-form + yup, label is OPTIONAL (unlike account
//      name), POSTs {label} (omitted entirely when blank).
//   2. secret — the CredentialWithSecret response is shown EXACTLY ONCE via
//      SecretOnceDisplay; closing is blocked until the operator acks.
//
// No step-up handling here: the baseApi interceptor + global StepUpModal
// handle it automatically for any mutation that needs it.
function extractError(err: unknown, t: (key: string) => string): string {
  const anyErr = err as {
    status?: number;
    data?: { code?: string; detail?: string };
  };
  if (anyErr?.status === 409) {
    return t('adminServiceAccounts.errors.credentialCap');
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

const IssueCredentialModal: React.FC<Props> = ({ show, onHide, accountId }) => {
  const { t } = useTranslation();
  const [issueCredential, { isLoading, reset: resetIssueMutation }] =
    useIssueCredentialMutation();
  const [phase, setPhase] = useState<Phase>('form');
  const [issued, setIssued] = useState<CredentialWithSecret | null>(null);
  const [ack, setAck] = useState(false);

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors }
  } = useForm<FormData>({ resolver: yupResolver(schema) });

  // Reset every piece of state whenever the modal opens or closes, so a
  // second issue starts clean and a dismissed secret is never reachable
  // again from a stale render.
  useEffect(() => {
    if (show) return;
    setPhase('form');
    setIssued(null);
    setAck(false);
    reset({ label: '' });
    // Also clears the RTK Query mutation cache entry, so the plaintext
    // secret doesn't linger in the store's api.mutations slice after close.
    resetIssueMutation();
  }, [show, reset, resetIssueMutation]);

  const onSubmit = handleSubmit(async data => {
    try {
      const label = data.label?.trim();
      const result = await issueCredential({
        id: accountId,
        ...(label ? { label } : {})
      }).unwrap();
      toast.success(t('adminServiceAccounts.issueModal.successToast'));
      setIssued(result);
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
            ? t('adminServiceAccounts.issueModal.title')
            : t('adminServiceAccounts.issueModal.secretTitle')}
        </Modal.Title>
      </Modal.Header>

      {phase === 'form' ? (
        <Form onSubmit={onSubmit} noValidate>
          <Modal.Body>
            <Form.Group controlId="sa-issue-label">
              <Form.Label>
                {t('adminServiceAccounts.issueModal.labelLabel')}
              </Form.Label>
              <Form.Control
                autoFocus
                isInvalid={!!errors.label}
                placeholder={t(
                  'adminServiceAccounts.issueModal.labelPlaceholder'
                )}
                {...register('label')}
              />
              <Form.Control.Feedback type="invalid">
                {t('adminServiceAccounts.issueModal.labelTooLong')}
              </Form.Control.Feedback>
            </Form.Group>
          </Modal.Body>
          <Modal.Footer>
            <Button
              variant="secondary"
              onClick={handleClose}
              disabled={isLoading}
            >
              {t('adminServiceAccounts.issueModal.cancel')}
            </Button>
            <Button variant="primary" type="submit" disabled={isLoading}>
              {isLoading ? (
                <>
                  <Spinner size="sm" animation="border" className="me-2" />
                  {t('adminServiceAccounts.issueModal.issuing')}
                </>
              ) : (
                t('adminServiceAccounts.issueModal.issue')
              )}
            </Button>
          </Modal.Footer>
        </Form>
      ) : (
        <>
          <Modal.Body>
            {issued && (
              <SecretOnceDisplay
                label={t('adminServiceAccounts.createModal.clientSecretLabel')}
                secret={issued.clientSecret}
                secondaryLabel={t(
                  'adminServiceAccounts.createModal.clientIdLabel'
                )}
                secondaryValue={issued.clientId}
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

export default IssueCredentialModal;

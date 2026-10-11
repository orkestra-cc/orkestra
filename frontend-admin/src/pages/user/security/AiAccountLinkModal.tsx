import { useMemo, useState } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { useForm, type Resolver } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { faArrowUpRightFromSquare } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import IconButton from 'components/common/IconButton';
import {
  useCompleteLlmAccountMutation,
  type LlmAccount
} from 'store/api/llmApi';
import { isQuietLlmError, llmErrorMessage } from 'pages/admin/llm/llmErrors';

// Backend bound (handlers.LLMAccountCompleteRequest).
const MAX_CALLBACK_URL = 4096;

const isWebAddress = (value?: string): boolean => {
  if (!value) return false;
  try {
    const parsed = new URL(value.trim());
    return parsed.protocol === 'http:' || parsed.protocol === 'https:';
  } catch {
    return false;
  }
};

export interface LinkWizardState {
  step: 1 | 2;
  // Absent when the wizard opens straight on the paste step (the
  // loopback_local fallback): the attempt was started elsewhere.
  authorizeUrl?: string;
  // Set when signing in again to an existing registration.
  reauthLabel?: string;
}

interface Props {
  state: LinkWizardState;
  onStep: (step: 1 | 2) => void;
  onClose: () => void;
  onLinked: (account: LlmAccount) => void;
}

// AiAccountLinkModal is the two-step paste flow of Sign in with ChatGPT
// (hosted_paste). Step 1 opens the OpenAI authorize URL in a NEW tab, so
// this wizard keeps its state (ruling 5.11); step 2 takes the address that
// tab landed on — an http://127.0.0.1 callback the browser cannot open — and
// posts it to the completion route. No token passes through here, and
// neither the authorize URL (a re-authorization one may carry an
// id_token_hint) nor the pasted address is ever logged.
const AiAccountLinkModal = ({ state, onStep, onClose, onLinked }: Props) => {
  const { t } = useTranslation();
  const [complete, { isLoading }] = useCompleteLlmAccountMutation();
  const [error, setError] = useState<string | null>(null);

  const schema = useMemo(
    () =>
      yup.object({
        callbackUrl: yup
          .string()
          .trim()
          .required(t('userSecurity.aiAccounts.wizard.validation.required'))
          .max(
            MAX_CALLBACK_URL,
            t('userSecurity.aiAccounts.wizard.validation.tooLong')
          )
          .test(
            'web-address',
            t('userSecurity.aiAccounts.wizard.validation.invalid'),
            isWebAddress
          )
      }),
    [t]
  );
  const {
    register,
    handleSubmit,
    formState: { errors }
  } = useForm<{ callbackUrl: string }>({
    resolver: yupResolver(schema) as unknown as Resolver<{
      callbackUrl: string;
    }>,
    defaultValues: { callbackUrl: '' }
  });

  const openOpenAI = () => {
    if (!state.authorizeUrl) return;
    // Synchronous inside the click, so no popup blocker stands in the way;
    // noopener keeps the OpenAI tab from reaching back into the console.
    window.open(state.authorizeUrl, '_blank', 'noopener');
    onStep(2);
  };

  const onSubmit = async ({ callbackUrl }: { callbackUrl: string }) => {
    setError(null);
    try {
      const account = await complete({ callbackUrl }).unwrap();
      onLinked(account);
    } catch (e) {
      if (!isQuietLlmError(e)) setError(llmErrorMessage(t, e));
    }
  };

  const title = state.reauthLabel
    ? t('userSecurity.aiAccounts.wizard.reauthTitle', {
        label: state.reauthLabel
      })
    : t('userSecurity.aiAccounts.wizard.title');

  return (
    <Modal show onHide={onClose} centered>
      <Form noValidate onSubmit={handleSubmit(onSubmit)}>
        <Modal.Header closeButton>
          <Modal.Title as="h5">{title}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <p className="fs-11 text-600 text-uppercase fw-semibold mb-2">
            {t('userSecurity.aiAccounts.wizard.stepOf', {
              step: state.step,
              total: 2
            })}
          </p>
          {state.step === 1 ? (
            <>
              <h6 className="mb-2">
                {t('userSecurity.aiAccounts.wizard.step1Title')}
              </h6>
              <p className="fs-10 mb-3">
                {t('userSecurity.aiAccounts.wizard.step1')}
              </p>
              <IconButton
                variant="orkestra-primary"
                icon={faArrowUpRightFromSquare}
                iconAlign="right"
                onClick={openOpenAI}
              >
                {t('userSecurity.aiAccounts.wizard.open')}
              </IconButton>
            </>
          ) : (
            <>
              <h6 className="mb-2">
                {t('userSecurity.aiAccounts.wizard.step2Title')}
              </h6>
              <p className="fs-10 mb-3">
                {t('userSecurity.aiAccounts.wizard.step2')}
              </p>
              {error && (
                <Alert variant="danger" className="fs-10 py-2">
                  {error}
                </Alert>
              )}
              <Form.Group controlId="llm-siwc-callback-url">
                <Form.Label>
                  {t('userSecurity.aiAccounts.wizard.pasteLabel')}
                </Form.Label>
                <Form.Control
                  type="url"
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={t(
                    'userSecurity.aiAccounts.wizard.pastePlaceholder'
                  )}
                  isInvalid={!!errors.callbackUrl}
                  {...register('callbackUrl')}
                />
                <Form.Control.Feedback type="invalid">
                  {errors.callbackUrl?.message}
                </Form.Control.Feedback>
                <Form.Text className="text-600">
                  {t('userSecurity.aiAccounts.wizard.pasteHint')}
                </Form.Text>
              </Form.Group>
            </>
          )}
        </Modal.Body>
        <Modal.Footer>
          {state.step === 2 && state.authorizeUrl && (
            <Button
              variant="orkestra-default"
              className="me-auto"
              onClick={() => onStep(1)}
            >
              {t('userSecurity.aiAccounts.wizard.back')}
            </Button>
          )}
          <Button variant="orkestra-default" onClick={onClose}>
            {t('userSecurity.aiAccounts.wizard.cancel')}
          </Button>
          {state.step === 2 && (
            <Button
              variant="orkestra-primary"
              type="submit"
              disabled={isLoading}
            >
              {isLoading
                ? t('userSecurity.aiAccounts.wizard.completing')
                : t('userSecurity.aiAccounts.wizard.complete')}
            </Button>
          )}
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default AiAccountLinkModal;

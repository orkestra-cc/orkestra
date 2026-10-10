import { useMemo, useState } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { useForm, type Resolver } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import {
  LLM_FIXED_BASE_URL,
  LLM_PROVIDERS,
  useCreateLlmCredentialMutation,
  usePatchLlmCredentialMutation,
  useRotateLlmCredentialMutation,
  type LlmCredential,
  type LlmProvider
} from 'store/api/llmApi';
import { useGetModuleQuery } from 'store/api/moduleApi';
import { isReauthCancelled, llmErrorMessage } from './llmErrors';
import {
  isHostedProvider,
  isHttpUrl,
  LLM_NAME_RE,
  needsBaseUrl,
  needsSecret
} from './llmRules';

export type EditorMode = 'create' | 'edit' | 'rotate';

interface CredentialFormValues {
  name: string;
  provider: LlmProvider;
  baseUrl: string;
  secret: string;
}

interface Props {
  mode: EditorMode;
  credential?: LlmCredential;
  onClose: () => void;
}

// The secret field is write-only: it is never prefilled, and after save the
// list shows only the last four characters the backend echoes back. The
// provider of an existing credential is fixed: in edit mode it is shown as
// read-only text and travels through defaultValues, never through a
// disabled input (react-hook-form drops a disabled input's value).
// Create, edit and rotate are step-up-gated; baseApi replays them after the
// StepUpModal. The backend stays authoritative for every rule mirrored here.
const CredentialEditorModal = ({ mode, credential, onClose }: Props) => {
  const { t } = useTranslation();
  const [create, { isLoading: creating }] = useCreateLlmCredentialMutation();
  const [patch, { isLoading: patching }] = usePatchLlmCredentialMutation();
  const [rotate, { isLoading: rotating }] = useRotateLlmCredentialMutation();
  const [error, setError] = useState<string | null>(null);
  const moduleConfig = useGetModuleQuery('llm', { skip: mode === 'rotate' });
  // Only a known "off" warns: a caller who cannot read the module config
  // gets no warning rather than a wrong one — the backend decides anyway.
  const hostedOff =
    moduleConfig.data !== undefined &&
    moduleConfig.data.configValues?.allow_hosted !== 'true';

  const schema = useMemo(
    () =>
      yup.object({
        name: yup
          .string()
          .trim()
          .required(t('adminLlm.validation.required'))
          .matches(LLM_NAME_RE, t('adminLlm.validation.name')),
        provider: yup
          .mixed<LlmProvider>()
          .oneOf(LLM_PROVIDERS)
          .required(t('adminLlm.validation.required')),
        baseUrl: yup
          .string()
          .trim()
          .max(512, t('adminLlm.validation.tooLong'))
          .when('provider', {
            is: (p: LlmProvider) => needsBaseUrl(p),
            then: s =>
              s
                .required(t('adminLlm.validation.required'))
                .test('http-url', t('adminLlm.validation.url'), v =>
                  isHttpUrl(v ?? '')
                ),
            otherwise: s => s.optional()
          }),
        secret: yup
          .string()
          .max(4096, t('adminLlm.validation.tooLong'))
          .when('provider', {
            is: (p: LlmProvider) =>
              mode === 'rotate' || (mode === 'create' && needsSecret(p)),
            then: s => s.required(t('adminLlm.validation.required')),
            otherwise: s => s.optional()
          })
      }),
    [t, mode]
  );

  const {
    register,
    handleSubmit,
    watch,
    formState: { errors }
  } = useForm<CredentialFormValues>({
    resolver: yupResolver(schema) as unknown as Resolver<CredentialFormValues>,
    defaultValues: {
      name: credential?.name ?? '',
      provider: credential?.provider ?? 'openai',
      baseUrl: credential?.baseUrl ?? '',
      secret: ''
    }
  });
  const provider = watch('provider');
  const fixedBase = LLM_FIXED_BASE_URL[provider];
  const showBaseUrl = needsBaseUrl(provider);
  const showSecret =
    mode === 'rotate' || (mode === 'create' && needsSecret(provider));

  const onSubmit = async (values: CredentialFormValues) => {
    setError(null);
    try {
      if (mode === 'create') {
        await create({
          name: values.name,
          provider: values.provider,
          baseUrl: showBaseUrl ? values.baseUrl : undefined,
          secret: showSecret ? values.secret : undefined
        }).unwrap();
      } else if (mode === 'edit' && credential) {
        // Partial PATCH: only what the operator changed.
        const body: { name?: string; baseUrl?: string } = {};
        if (values.name !== credential.name) body.name = values.name;
        if (showBaseUrl && values.baseUrl !== (credential.baseUrl ?? '')) {
          body.baseUrl = values.baseUrl;
        }
        if (Object.keys(body).length > 0) {
          await patch({ uuid: credential.uuid, ...body }).unwrap();
        }
      } else if (mode === 'rotate' && credential) {
        await rotate({ uuid: credential.uuid, secret: values.secret }).unwrap();
      }
      toast.success(t('adminLlm.credentials.saveSuccess'));
      onClose();
    } catch (e) {
      if (!isReauthCancelled(e)) setError(llmErrorMessage(t, e));
    }
  };

  return (
    <Modal show onHide={onClose} centered>
      <Form noValidate onSubmit={handleSubmit(onSubmit)}>
        <Modal.Header closeButton>
          <Modal.Title as="h5">
            {t(`adminLlm.credentials.editorTitle.${mode}`, {
              name: credential?.name ?? ''
            })}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && (
            <Alert variant="danger" className="fs-10 py-2">
              {error}
            </Alert>
          )}
          {mode !== 'rotate' && (
            <>
              <Form.Group className="mb-3" controlId="llm-credential-name">
                <Form.Label>{t('adminLlm.credentials.fields.name')}</Form.Label>
                <Form.Control isInvalid={!!errors.name} {...register('name')} />
                <Form.Control.Feedback type="invalid">
                  {errors.name?.message}
                </Form.Control.Feedback>
              </Form.Group>
              <Form.Group className="mb-3" controlId="llm-credential-provider">
                <Form.Label>
                  {t('adminLlm.credentials.fields.provider')}
                </Form.Label>
                {mode === 'create' ? (
                  <Form.Select {...register('provider')}>
                    {LLM_PROVIDERS.map(p => (
                      <option key={p} value={p}>
                        {t(`adminLlm.providers.${p}`)}
                      </option>
                    ))}
                  </Form.Select>
                ) : (
                  <Form.Control
                    plaintext
                    readOnly
                    value={t(`adminLlm.providers.${provider}`)}
                  />
                )}
              </Form.Group>
              {fixedBase && (
                <p className="fs-10 text-600 mb-3">
                  {t('adminLlm.credentials.fixedEndpoint', {
                    url: fixedBase
                  })}
                </p>
              )}
              {showBaseUrl && (
                <Form.Group className="mb-3" controlId="llm-credential-baseurl">
                  <Form.Label>
                    {t('adminLlm.credentials.fields.baseUrl')}
                  </Form.Label>
                  <Form.Control
                    placeholder={t(
                      'adminLlm.credentials.fields.baseUrlPlaceholder'
                    )}
                    isInvalid={!!errors.baseUrl}
                    {...register('baseUrl')}
                  />
                  <Form.Control.Feedback type="invalid">
                    {errors.baseUrl?.message}
                  </Form.Control.Feedback>
                </Form.Group>
              )}
              {hostedOff && isHostedProvider(provider) && (
                <Alert variant="warning" className="fs-10">
                  {t('adminLlm.credentials.hostedDisabledWarning')}
                </Alert>
              )}
            </>
          )}
          {showSecret && (
            <Form.Group className="mb-1" controlId="llm-credential-secret">
              <Form.Label>{t('adminLlm.credentials.fields.secret')}</Form.Label>
              <Form.Control
                type="password"
                autoComplete="off"
                isInvalid={!!errors.secret}
                {...register('secret')}
              />
              <Form.Control.Feedback type="invalid">
                {errors.secret?.message}
              </Form.Control.Feedback>
              <Form.Text className="text-600">
                {t('adminLlm.credentials.secretHint')}
              </Form.Text>
            </Form.Group>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="orkestra-default" onClick={onClose}>
            {t('adminLlm.actions.cancel')}
          </Button>
          <Button
            variant="orkestra-primary"
            type="submit"
            disabled={creating || patching || rotating}
          >
            {t('adminLlm.actions.save')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default CredentialEditorModal;

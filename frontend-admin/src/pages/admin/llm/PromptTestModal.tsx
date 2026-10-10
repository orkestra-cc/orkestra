import { useMemo, useState } from 'react';
import { Alert, Button, Form, Modal, Spinner } from 'react-bootstrap';
import { useForm, type Resolver } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import {
  usePromptTestLlmModelMutation,
  type LlmModel,
  type LlmPromptTestResult
} from 'store/api/llmApi';
import { isQuietLlmError, llmErrorMessage } from './llmErrors';

// Backend bound (models.LLMPromptTestBody).
const MAX_PROMPT = 4000;

interface Props {
  model: LlmModel;
  onClose: () => void;
}

// PromptTestModal sends one test prompt to a configured model
// (llm.models.admin + MFA): grants are ignored, the call is reserved against
// the budget and ledgered as llm.admin_test, the prompt is stored nowhere.
// The answer does not name the model that replied, only whether a
// fallback did — so the result says which configured model was asked and
// flags a fallback. Errors go through the shared llm.* copy (budget,
// provider, credential, quota, rate limit).
const PromptTestModal = ({ model, onClose }: Props) => {
  const { t, i18n } = useTranslation();
  const [run, { isLoading }] = usePromptTestLlmModelMutation();
  const [result, setResult] = useState<LlmPromptTestResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  const schema = useMemo(
    () =>
      yup.object({
        prompt: yup
          .string()
          .trim()
          .required(t('adminLlm.validation.required'))
          .max(MAX_PROMPT, t('adminLlm.prompt.tooLong', { max: MAX_PROMPT }))
      }),
    [t]
  );
  const {
    register,
    handleSubmit,
    formState: { errors }
  } = useForm<{ prompt: string }>({
    resolver: yupResolver(schema) as unknown as Resolver<{ prompt: string }>,
    defaultValues: { prompt: '' }
  });

  const onSubmit = async ({ prompt }: { prompt: string }) => {
    setError(null);
    setResult(null);
    try {
      setResult(await run({ uuid: model.uuid, prompt }).unwrap());
    } catch (e) {
      if (!isQuietLlmError(e)) setError(llmErrorMessage(t, e));
    }
  };

  const num = (n: number) => n.toLocaleString(i18n.language);

  return (
    <Modal show onHide={onClose} centered size="lg">
      <Form noValidate onSubmit={handleSubmit(onSubmit)}>
        <Modal.Header closeButton>
          <Modal.Title as="h5">
            {t('adminLlm.prompt.title', { name: model.name })}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && (
            <Alert variant="danger" className="fs-10 py-2">
              {error}
            </Alert>
          )}
          <Form.Group className="mb-3" controlId="llm-prompt-text">
            <Form.Label>{t('adminLlm.prompt.field')}</Form.Label>
            <Form.Control
              as="textarea"
              rows={4}
              isInvalid={!!errors.prompt}
              {...register('prompt')}
            />
            <Form.Control.Feedback type="invalid">
              {errors.prompt?.message}
            </Form.Control.Feedback>
            <Form.Text className="text-600">
              {t('adminLlm.prompt.hint')}
            </Form.Text>
          </Form.Group>
          {result && (
            <section aria-label={t('adminLlm.prompt.answer')}>
              <h6 className="text-900 mb-2">{t('adminLlm.prompt.answer')}</h6>
              <div className="d-flex flex-wrap align-items-center gap-2 fs-10 mb-2">
                {result.fallback ? (
                  <SubtleBadge pill bg="warning">
                    {t('adminLlm.prompt.fallback', { name: model.name })}
                  </SubtleBadge>
                ) : (
                  <span className="text-700">
                    {t('adminLlm.prompt.answeredBy', {
                      name: model.name,
                      modelId: model.modelId
                    })}
                  </span>
                )}
              </div>
              <div className="font-monospace fs-10 bg-body-tertiary border rounded p-3 mb-2 text-pre-line text-break">
                {result.text || (
                  <span className="text-600">{t('adminLlm.prompt.empty')}</span>
                )}
              </div>
              <div className="d-flex flex-wrap gap-3 fs-10 text-600">
                <span>
                  {t('adminLlm.prompt.usage', {
                    input: num(result.usage.inputTokens),
                    output: num(result.usage.outputTokens)
                  })}
                </span>
                <span>
                  {t('adminLlm.prompt.finishReason', {
                    reason: result.finishReason || '—'
                  })}
                </span>
                {result.truncated && (
                  <span>{t('adminLlm.prompt.truncated')}</span>
                )}
              </div>
            </section>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="orkestra-default" onClick={onClose}>
            {t('adminLlm.prompt.close')}
          </Button>
          <Button variant="orkestra-primary" type="submit" disabled={isLoading}>
            {isLoading && (
              <Spinner animation="border" size="sm" className="me-2" />
            )}
            {isLoading
              ? t('adminLlm.prompt.running')
              : t('adminLlm.prompt.run')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default PromptTestModal;

import { useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Col,
  Form,
  Modal,
  Row,
  Spinner,
  Table
} from 'react-bootstrap';
import { useForm, type Resolver } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import {
  useRunRetrievalDiagnosticsMutation,
  type RetrievalDiagnosticsReport,
  type RetrievalMode
} from 'store/api/retrievalApi';
import { isReauthCancelled, retrievalErrorMessage } from './retrievalErrors';

// The search pipeline's bounds: the query in characters (code points, as
// the backend counts them), the result count.
const MAX_QUERY = 2000;
const MAX_LIMIT = 100;
const MODES: RetrievalMode[] = ['hybrid', 'dense', 'keyword'];

interface FormValues {
  query: string;
  mode: RetrievalMode;
  limit: number;
}

interface Props {
  corpus: string;
  onClose: () => void;
}

// DiagnosticsModal runs one real search on a corpus as the caller
// (retrieval.corpus.admin + MFA + fresh step-up) and reports each hit's
// per-stage scores. The caller's own principals apply: an operator sees
// only what the same user would see through the product, and the search
// is written to the search log and the audit trail like any other. A
// dismissed step-up shows nothing.
const DiagnosticsModal = ({ corpus, onClose }: Props) => {
  const { t, i18n } = useTranslation();
  const [run, { isLoading }] = useRunRetrievalDiagnosticsMutation();
  const [result, setResult] = useState<RetrievalDiagnosticsReport | null>(null);
  const [error, setError] = useState<string | null>(null);

  const schema = useMemo(
    () =>
      yup.object({
        query: yup
          .string()
          .trim()
          .required(t('adminRetrieval.diagnostics.validation.queryRequired'))
          .test(
            'max-chars',
            t('adminRetrieval.diagnostics.validation.queryTooLong', {
              max: MAX_QUERY
            }),
            v => Array.from(v ?? '').length <= MAX_QUERY
          ),
        mode: yup.mixed<RetrievalMode>().oneOf(MODES).required(),
        limit: yup
          .number()
          .typeError(t('adminRetrieval.diagnostics.validation.limitRange'))
          .integer(t('adminRetrieval.diagnostics.validation.limitRange'))
          .min(1, t('adminRetrieval.diagnostics.validation.limitRange'))
          .max(MAX_LIMIT, t('adminRetrieval.diagnostics.validation.limitRange'))
          .required(t('adminRetrieval.diagnostics.validation.limitRange'))
      }),
    [t]
  );
  const {
    register,
    handleSubmit,
    formState: { errors }
  } = useForm<FormValues>({
    resolver: yupResolver(schema) as unknown as Resolver<FormValues>,
    defaultValues: { query: '', mode: 'hybrid', limit: 10 }
  });

  const onSubmit = async (values: FormValues) => {
    setError(null);
    setResult(null);
    try {
      setResult(await run({ corpus, ...values }).unwrap());
    } catch (e) {
      if (!isReauthCancelled(e)) setError(retrievalErrorMessage(t, e));
    }
  };

  const score = (n: number | null | undefined) =>
    n == null
      ? '—'
      : n.toLocaleString(i18n.language, { maximumFractionDigits: 4 });

  return (
    <Modal show onHide={onClose} centered size="xl">
      <Form noValidate onSubmit={handleSubmit(onSubmit)}>
        <Modal.Header closeButton>
          <Modal.Title as="h5">
            {t('adminRetrieval.diagnostics.title', { name: corpus })}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && (
            <Alert variant="danger" className="fs-10 py-2">
              {error}
            </Alert>
          )}
          <Row className="g-3 mb-2">
            <Form.Group as={Col} md={8} controlId="retrieval-diag-query">
              <Form.Label>{t('adminRetrieval.diagnostics.query')}</Form.Label>
              <Form.Control isInvalid={!!errors.query} {...register('query')} />
              <Form.Control.Feedback type="invalid">
                {errors.query?.message}
              </Form.Control.Feedback>
            </Form.Group>
            <Form.Group as={Col} xs={6} md={2} controlId="retrieval-diag-mode">
              <Form.Label>{t('adminRetrieval.diagnostics.mode')}</Form.Label>
              <Form.Select {...register('mode')}>
                {MODES.map(mode => (
                  <option key={mode} value={mode}>
                    {t(`adminRetrieval.diagnostics.modes.${mode}`)}
                  </option>
                ))}
              </Form.Select>
            </Form.Group>
            <Form.Group as={Col} xs={6} md={2} controlId="retrieval-diag-limit">
              <Form.Label>{t('adminRetrieval.diagnostics.limit')}</Form.Label>
              <Form.Control
                type="number"
                min={1}
                max={MAX_LIMIT}
                isInvalid={!!errors.limit}
                {...register('limit', { valueAsNumber: true })}
              />
              <Form.Control.Feedback type="invalid">
                {errors.limit?.message}
              </Form.Control.Feedback>
            </Form.Group>
          </Row>
          <Form.Text className="text-600 d-block mb-3">
            {t('adminRetrieval.diagnostics.hint')}
          </Form.Text>

          {result && (
            <section aria-label={t('adminRetrieval.diagnostics.result')}>
              <h6 className="text-900 mb-2">
                {t('adminRetrieval.diagnostics.result')}
              </h6>
              <p className="fs-10 text-700 mb-2">
                {t('adminRetrieval.diagnostics.pinned', {
                  collection: result.collection || '—',
                  profile: result.profile || '—'
                })}
              </p>
              <div className="d-flex flex-wrap align-items-center gap-2 fs-10 mb-3">
                <span className="text-700">
                  {t('adminRetrieval.diagnostics.principals')}
                </span>
                {result.principals.map(p => (
                  <SubtleBadge key={p} pill bg="secondary">
                    <span className="font-monospace">{p}</span>
                  </SubtleBadge>
                ))}
              </div>
              {result.degraded && (
                <Alert variant="warning" className="fs-10 py-2">
                  {t('adminRetrieval.diagnostics.degraded')}
                </Alert>
              )}
              {result.hits.length === 0 ? (
                <p className="fs-10 text-600 mb-0">
                  {t('adminRetrieval.diagnostics.noHits')}
                </p>
              ) : (
                <Table size="sm" responsive striped className="fs-10 mb-0">
                  <thead>
                    <tr>
                      {(
                        [
                          'rank',
                          'sourceId',
                          'chunkId',
                          'score',
                          'fused',
                          'dense',
                          'keyword',
                          'rerank',
                          'snippet'
                        ] as const
                      ).map(col => (
                        <th key={col} scope="col" className="text-nowrap">
                          {t(`adminRetrieval.diagnostics.columns.${col}`)}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {result.hits.map(hit => (
                      <tr key={`${hit.rank}:${hit.chunkId}`}>
                        <td>{hit.rank}</td>
                        <td className="font-monospace text-nowrap">
                          {hit.sourceId}
                        </td>
                        <td className="font-monospace text-nowrap">
                          {hit.chunkId}
                        </td>
                        <td className="fw-semibold">{score(hit.score)}</td>
                        <td>{score(hit.fused)}</td>
                        <td>{score(hit.dense)}</td>
                        <td>{score(hit.keyword)}</td>
                        <td>{score(hit.rerank)}</td>
                        <td className="text-wrap text-break">{hit.snippet}</td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}
            </section>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="orkestra-default" onClick={onClose}>
            {t('adminRetrieval.actions.close')}
          </Button>
          <Button variant="orkestra-primary" type="submit" disabled={isLoading}>
            {isLoading && (
              <Spinner animation="border" size="sm" className="me-2" />
            )}
            {isLoading
              ? t('adminRetrieval.actions.running')
              : t('adminRetrieval.actions.run')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default DiagnosticsModal;

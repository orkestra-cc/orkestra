import { useState, useRef, useCallback } from 'react';
import { useParams } from 'react-router-dom';
import { Row, Col, Card, Form, Button, Spinner, Badge } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faMagic, faPaperPlane } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import Background from 'components/common/Background';
import greetingsBg from 'assets/img/illustrations/ticket-greetings-bg.png';
import {
  useSubmitSkillMutation,
  useLazyPollSkillTaskQuery
} from '../../../store/api/salesApi';

const SKILL_KEYS = [
  'research',
  'qualify',
  'contacts',
  'outreach',
  'prep',
  'proposal',
  'competitors',
  'followup',
  'objections',
  'icp'
] as const;

const POLL_INTERVAL = 2000;

const SkillPage = () => {
  const { t } = useTranslation();
  const { skill } = useParams<{ skill: string }>();
  const meta = (SKILL_KEYS as readonly string[]).includes(skill || '')
    ? {
        title: t(`sales.skills.meta.${skill}Title`),
        description: t(`sales.skills.meta.${skill}Description`)
      }
    : { title: skill || '', description: '' };

  const [url, setUrl] = useState('');
  const [context, setContext] = useState('');
  const [locale, setLocale] = useState('it');
  const [result, setResult] = useState<any>(null);
  const [running, setRunning] = useState(false);

  const [submitSkill] = useSubmitSkillMutation();
  const [pollTask] = useLazyPollSkillTaskQuery();
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const stopPolling = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  const handleSubmit = async () => {
    if (!url || !skill) return;
    setResult(null);
    setRunning(true);
    stopPolling();

    try {
      const { taskId } = await submitSkill({
        skill,
        url,
        locale,
        context: context || undefined
      }).unwrap();

      pollRef.current = setInterval(async () => {
        try {
          const data = await pollTask(taskId, false).unwrap();
          if (data.status === 'completed') {
            stopPolling();
            setRunning(false);
            setResult({
              skill: data.skill,
              result: data.result,
              inputTokens: data.inputTokens,
              outputTokens: data.outputTokens,
              latencyMs: data.latencyMs,
              modelUsed: data.modelUsed
            });
          } else if (data.status === 'failed') {
            stopPolling();
            setRunning(false);
            setResult({
              error: data.error || t('sales.skills.errors.executionFailed')
            });
          }
        } catch {
          stopPolling();
          setRunning(false);
          setResult({ error: t('sales.skills.errors.pollFailed') });
        }
      }, POLL_INTERVAL);
    } catch (err: any) {
      setRunning(false);
      const detail = err?.data?.detail || err?.data?.errors?.[0]?.message;
      setResult({
        error: detail || err?.message || t('sales.skills.errors.submitFailed')
      });
    }
  };

  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <Card className="bg-body-tertiary dark__bg-opacity-50 shadow-none h-100">
            <Background
              image={greetingsBg}
              className="bg-card d-none d-sm-block"
            />
            <Card.Header className="d-flex align-items-center z-1 p-0">
              <div className="bg-primary rounded-circle p-3 ms-3">
                <FontAwesomeIcon
                  icon={faMagic}
                  className="text-white"
                  size="2x"
                />
              </div>
              <div className="ms-3">
                <h6 className="mb-1 text-primary">
                  {t('sales.skills.kicker')}
                </h6>
                <h4 className="mb-0 text-primary fw-bold">
                  {meta.title}
                  {meta.description && (
                    <span className="text-info fw-medium fs-6 ms-2">
                      — {meta.description}
                    </span>
                  )}
                </h4>
              </div>
            </Card.Header>
          </Card>
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col lg={12}>
          <Card>
            <Card.Body>
              <Form
                onSubmit={e => {
                  e.preventDefault();
                  handleSubmit();
                }}
              >
                <Row className="g-3">
                  <Col md={5}>
                    <Form.Group>
                      <Form.Label>{t('sales.skills.urlLabel')}</Form.Label>
                      <Form.Control
                        type="url"
                        placeholder={t('sales.skills.urlPlaceholder')}
                        value={url}
                        onChange={e => setUrl(e.target.value)}
                        required
                      />
                    </Form.Group>
                  </Col>
                  <Col md={2}>
                    <Form.Group>
                      <Form.Label>{t('sales.skills.localeLabel')}</Form.Label>
                      <Form.Select
                        value={locale}
                        onChange={e => setLocale(e.target.value)}
                      >
                        <option value="it">
                          {t('sales.skills.localeItalian')}
                        </option>
                        <option value="en">
                          {t('sales.skills.localeEnglish')}
                        </option>
                      </Form.Select>
                    </Form.Group>
                  </Col>
                  <Col md={5}>
                    <Form.Group>
                      <Form.Label>
                        {t('sales.skills.contextLabel')}{' '}
                        <span className="text-muted">
                          {t('sales.skills.contextOptional')}
                        </span>
                      </Form.Label>
                      <Form.Control
                        type="text"
                        placeholder={t('sales.skills.contextPlaceholder')}
                        value={context}
                        onChange={e => setContext(e.target.value)}
                      />
                    </Form.Group>
                  </Col>
                </Row>
                <Button
                  variant="primary"
                  type="submit"
                  disabled={!url || running}
                  className="mt-3"
                >
                  {running ? (
                    <Spinner size="sm" className="me-1" />
                  ) : (
                    <FontAwesomeIcon icon={faPaperPlane} className="me-1" />
                  )}
                  {running
                    ? t('sales.skills.processing')
                    : t('sales.skills.runButton', { title: meta.title })}
                </Button>
              </Form>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      {result && (
        <Row className="g-3 mb-3">
          <Col lg={12}>
            <Card>
              <Card.Header className="d-flex justify-content-between align-items-center">
                <h5 className="mb-0">{t('sales.skills.resultTitle')}</h5>
                <div className="d-flex gap-2">
                  {result.inputTokens !== undefined && (
                    <Badge bg="secondary">
                      {t('sales.skills.tokensBadge', {
                        input: result.inputTokens,
                        output: result.outputTokens
                      })}
                    </Badge>
                  )}
                  {result.latencyMs !== undefined && (
                    <Badge bg="info">
                      {t('sales.skills.latencyBadge', {
                        seconds: (result.latencyMs / 1000).toFixed(1)
                      })}
                    </Badge>
                  )}
                  {result.modelUsed && (
                    <Badge bg="dark">{result.modelUsed}</Badge>
                  )}
                  {result.error && (
                    <Badge bg="danger">{t('sales.skills.errorBadge')}</Badge>
                  )}
                </div>
              </Card.Header>
              <Card.Body>
                <pre
                  className="bg-body-tertiary p-3 rounded"
                  style={{ maxHeight: 600, overflow: 'auto' }}
                >
                  {JSON.stringify(result.result || result, null, 2)}
                </pre>
              </Card.Body>
            </Card>
          </Col>
        </Row>
      )}
    </>
  );
};

export default SkillPage;

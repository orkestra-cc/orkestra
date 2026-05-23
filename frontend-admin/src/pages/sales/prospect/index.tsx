import { useState } from 'react';
import { Row, Col, Card, Form, Button, Badge, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faCrosshairs,
  faRocket,
  faBolt
} from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import Background from 'components/common/Background';
import greetingsBg from 'assets/img/illustrations/ticket-greetings-bg.png';
import {
  useCreateProspectJobMutation,
  useRunQuickProspectMutation
} from '../../../store/api/salesApi';

function ProspectGreetings() {
  const { t } = useTranslation();
  return (
    <Card className="bg-body-tertiary dark__bg-opacity-50 shadow-none h-100">
      <Background image={greetingsBg} className="bg-card d-none d-sm-block" />
      <Card.Header className="d-flex align-items-center z-1 p-0">
        <div className="bg-primary rounded-circle p-3 ms-3">
          <FontAwesomeIcon
            icon={faCrosshairs}
            className="text-white"
            size="2x"
          />
        </div>
        <div className="ms-3">
          <h6 className="mb-1 text-primary">{t('sales.kicker')}</h6>
          <h4 className="mb-0 text-primary fw-bold">
            {t('sales.prospect.title')}
            <span className="text-info fw-medium">
              {t('sales.prospect.titleAccent')}
            </span>
          </h4>
        </div>
      </Card.Header>
    </Card>
  );
}

const ProspectPage = () => {
  const { t } = useTranslation();
  const [url, setUrl] = useState('');
  const [locale, setLocale] = useState('it');
  const [result, setResult] = useState<any>(null);

  const [createJob, { isLoading: fullLoading }] =
    useCreateProspectJobMutation();
  const [runQuick, { isLoading: quickLoading }] = useRunQuickProspectMutation();

  const handleFull = async () => {
    if (!url) return;
    try {
      const data = await createJob({ url, locale }).unwrap();
      setResult(data);
    } catch (err: any) {
      setResult({
        error:
          err?.data?.detail || err?.message || t('sales.prospect.requestFailed')
      });
    }
  };

  const handleQuick = async () => {
    if (!url) return;
    try {
      const data = await runQuick({ url, locale }).unwrap();
      setResult(data);
    } catch (err: any) {
      setResult({
        error:
          err?.data?.detail || err?.message || t('sales.prospect.requestFailed')
      });
    }
  };

  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <ProspectGreetings />
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col lg={12}>
          <Card>
            <Card.Header>
              <h5 className="mb-0">{t('sales.prospect.analyzeTitle')}</h5>
            </Card.Header>
            <Card.Body>
              <Form
                onSubmit={e => {
                  e.preventDefault();
                  handleFull();
                }}
              >
                <Row className="g-3 align-items-end">
                  <Col md={6}>
                    <Form.Group>
                      <Form.Label>{t('sales.prospect.urlLabel')}</Form.Label>
                      <Form.Control
                        type="url"
                        placeholder={t('sales.prospect.urlPlaceholder')}
                        value={url}
                        onChange={e => setUrl(e.target.value)}
                        required
                      />
                    </Form.Group>
                  </Col>
                  <Col md={2}>
                    <Form.Group>
                      <Form.Label>{t('sales.prospect.localeLabel')}</Form.Label>
                      <Form.Select
                        value={locale}
                        onChange={e => setLocale(e.target.value)}
                      >
                        <option value="it">
                          {t('sales.prospect.localeItalian')}
                        </option>
                        <option value="en">
                          {t('sales.prospect.localeEnglish')}
                        </option>
                      </Form.Select>
                    </Form.Group>
                  </Col>
                  <Col md={4} className="d-flex gap-2">
                    <Button
                      variant="primary"
                      type="submit"
                      disabled={!url || fullLoading}
                    >
                      {fullLoading ? (
                        <Spinner size="sm" className="me-1" />
                      ) : (
                        <FontAwesomeIcon icon={faRocket} className="me-1" />
                      )}
                      {t('sales.prospect.fullButton')}
                    </Button>
                    <Button
                      variant="outline-primary"
                      onClick={handleQuick}
                      disabled={!url || quickLoading}
                    >
                      {quickLoading ? (
                        <Spinner size="sm" className="me-1" />
                      ) : (
                        <FontAwesomeIcon icon={faBolt} className="me-1" />
                      )}
                      {t('sales.prospect.quickButton')}
                    </Button>
                  </Col>
                </Row>
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
                <h5 className="mb-0">{t('sales.prospect.resultTitle')}</h5>
                <div className="d-flex gap-2">
                  {result.score !== undefined && (
                    <Badge
                      bg={
                        result.score >= 75
                          ? 'success'
                          : result.score >= 50
                            ? 'warning'
                            : 'danger'
                      }
                      className="fs-6"
                    >
                      {t('sales.prospect.scoreBadge', {
                        score: result.score,
                        grade: result.grade
                      })}
                    </Badge>
                  )}
                  {result.jobId && (
                    <Badge bg="info" className="fs-6">
                      {t('sales.prospect.jobBadge', { id: result.jobId })}
                    </Badge>
                  )}
                  {result.error && (
                    <Badge bg="danger" className="fs-6">
                      {t('sales.prospect.errorBadge')}
                    </Badge>
                  )}
                </div>
              </Card.Header>
              <Card.Body>
                <pre
                  className="bg-body-tertiary p-3 rounded"
                  style={{ maxHeight: 500, overflow: 'auto' }}
                >
                  {JSON.stringify(result, null, 2)}
                </pre>
              </Card.Body>
            </Card>
          </Col>
        </Row>
      )}
    </>
  );
};

export default ProspectPage;

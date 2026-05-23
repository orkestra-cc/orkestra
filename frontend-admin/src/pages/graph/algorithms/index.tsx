import { useState, useCallback } from 'react';
import {
  Row,
  Col,
  Card,
  Button,
  Form,
  Table,
  Badge,
  Spinner,
  Accordion
} from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useRunAlgorithmMutation,
  useListAlgorithmsQuery
} from '../../../store/api/graphApi';
import ResultsTable from '../components/ResultsTable';
import type { QueryResult } from '../../../types/graph';

const GraphAlgorithms: React.FC = () => {
  const { t } = useTranslation();
  const [database, setDatabase] = useState('');
  const [algorithmResult, setAlgorithmResult] = useState<QueryResult | null>(
    null
  );

  // Algorithms
  const { data: algoData } = useListAlgorithmsQuery();
  const [runAlgorithm, { isLoading: running }] = useRunAlgorithmMutation();

  // Algorithm form
  const [selectedAlgo, setSelectedAlgo] = useState('');
  const [algoConfig, setAlgoConfig] = useState('{}');

  const handleRunAlgorithm = useCallback(async () => {
    if (!selectedAlgo) return;
    try {
      let config: Record<string, unknown> = {};
      try {
        config = JSON.parse(algoConfig);
      } catch {
        // Use empty config if JSON is invalid
      }
      const res = await runAlgorithm({
        algorithm: selectedAlgo,
        config,
        database: database || undefined
      }).unwrap();
      setAlgorithmResult(res);
    } catch {
      setAlgorithmResult(null);
    }
  }, [runAlgorithm, selectedAlgo, algoConfig, database]);

  // Group algorithms by category
  const algosByCategory =
    algoData?.algorithms?.reduce(
      (acc, algo) => {
        if (!acc[algo.category]) acc[algo.category] = [];
        acc[algo.category].push(algo);
        return acc;
      },
      {} as Record<string, typeof algoData.algorithms>
    ) ?? {};

  return (
    <>
      <Row className="g-3 mb-3">
        <Col>
          <div className="d-flex align-items-center justify-content-between">
            <h5 className="mb-0">{t('graph.algorithms.pageTitle')}</h5>
            <Form.Group className="d-flex align-items-center gap-2">
              <Form.Label className="mb-0 small text-muted">
                {t('graph.algorithms.databaseLabel')}
              </Form.Label>
              <Form.Control
                size="sm"
                type="text"
                placeholder={t('graph.algorithms.databasePlaceholder')}
                value={database}
                onChange={e => setDatabase(e.target.value)}
                style={{ width: 150 }}
              />
            </Form.Group>
          </div>
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        {/* Available Algorithms */}
        <Col lg={5}>
          <Card className="h-100">
            <Card.Header>
              <div className="d-flex align-items-center justify-content-between">
                <h6 className="mb-0">
                  {t('graph.algorithms.available.title')}
                </h6>
                <Badge bg="secondary">
                  {algoData?.algorithms?.length ?? 0}
                </Badge>
              </div>
            </Card.Header>
            <Card.Body className="p-0">
              <Table size="sm" hover className="mb-0">
                <thead>
                  <tr>
                    <th>{t('graph.algorithms.available.cols.name')}</th>
                    <th>{t('graph.algorithms.available.cols.category')}</th>
                    <th>{t('graph.algorithms.available.cols.procedure')}</th>
                  </tr>
                </thead>
                <tbody>
                  {algoData?.algorithms?.map(a => (
                    <tr
                      key={a.name}
                      style={{ cursor: 'pointer' }}
                      className={selectedAlgo === a.name ? 'table-primary' : ''}
                      onClick={() => setSelectedAlgo(a.name)}
                    >
                      <td className="fw-semibold">{a.name}</td>
                      <td>
                        <Badge bg="info" className="text-capitalize">
                          {a.category}
                        </Badge>
                      </td>
                      <td>
                        <code className="small">{a.procedure}</code>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </Card.Body>
          </Card>
        </Col>

        {/* Run Algorithm */}
        <Col lg={7}>
          <Card className="h-100">
            <Card.Header>
              <h6 className="mb-0">{t('graph.algorithms.run.title')}</h6>
            </Card.Header>
            <Card.Body>
              <Form.Group className="mb-3">
                <Form.Label className="small">
                  {t('graph.algorithms.run.algorithmLabel')}
                </Form.Label>
                <Form.Select
                  size="sm"
                  value={selectedAlgo}
                  onChange={e => setSelectedAlgo(e.target.value)}
                >
                  <option value="">
                    {t('graph.algorithms.run.selectPlaceholder')}
                  </option>
                  {Object.entries(algosByCategory).map(([category, algos]) => (
                    <optgroup
                      key={category}
                      label={
                        category.charAt(0).toUpperCase() + category.slice(1)
                      }
                    >
                      {algos.map(a => (
                        <option key={a.name} value={a.name}>
                          {a.name} - {a.description}
                        </option>
                      ))}
                    </optgroup>
                  ))}
                </Form.Select>
              </Form.Group>

              <Accordion className="mb-3">
                <Accordion.Item eventKey="0">
                  <Accordion.Header>
                    <small>{t('graph.algorithms.run.configHeading')}</small>
                  </Accordion.Header>
                  <Accordion.Body className="p-2">
                    <Form.Control
                      as="textarea"
                      rows={3}
                      size="sm"
                      className="font-monospace"
                      value={algoConfig}
                      onChange={e => setAlgoConfig(e.target.value)}
                      placeholder={t('graph.algorithms.run.configPlaceholder')}
                    />
                  </Accordion.Body>
                </Accordion.Item>
              </Accordion>

              <Button
                variant="primary"
                size="sm"
                onClick={handleRunAlgorithm}
                disabled={running || !selectedAlgo}
              >
                {running ? (
                  <>
                    <Spinner size="sm" className="me-1" />{' '}
                    {t('graph.algorithms.run.running')}
                  </>
                ) : (
                  t('graph.algorithms.run.submit')
                )}
              </Button>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      {/* Algorithm Results */}
      {algorithmResult && (
        <Row className="g-3">
          <Col>
            <ResultsTable result={algorithmResult} isLoading={running} />
          </Col>
        </Row>
      )}
    </>
  );
};

export default GraphAlgorithms;

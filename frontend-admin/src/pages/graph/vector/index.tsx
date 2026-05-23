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
  Alert
} from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useListVectorIndexesQuery,
  useCreateVectorIndexMutation,
  useDropVectorIndexMutation,
  useVectorSearchMutation
} from '../../../store/api/graphApi';
import ResultsTable from '../components/ResultsTable';
import type { QueryResult } from '../../../types/graph';

const VectorSearch: React.FC = () => {
  const { t } = useTranslation();
  const [database, setDatabase] = useState('');
  const [searchResult, setSearchResult] = useState<QueryResult | null>(null);

  // Vector indexes
  const { data: indexData, isLoading: indexLoading } =
    useListVectorIndexesQuery(database ? { database } : undefined);
  const [createIndex, { isLoading: creating }] = useCreateVectorIndexMutation();
  const [dropIndex] = useDropVectorIndexMutation();
  const [vectorSearch, { isLoading: searching }] = useVectorSearchMutation();

  // Search form
  const [searchIndex, setSearchIndex] = useState('');
  const [vectorInput, setVectorInput] = useState('');
  const [topK, setTopK] = useState(10);
  const [minScore, setMinScore] = useState(0);

  // Create index form
  const [newName, setNewName] = useState('');
  const [newLabel, setNewLabel] = useState('');
  const [newProperty, setNewProperty] = useState('');
  const [newDimensions, setNewDimensions] = useState(256);
  const [newSimilarity, setNewSimilarity] = useState<'cos' | 'l2sq' | 'ip'>(
    'cos'
  );

  const handleSearch = useCallback(async () => {
    if (!searchIndex || !vectorInput) return;
    try {
      const queryVector = JSON.parse(vectorInput) as number[];
      if (!Array.isArray(queryVector)) throw new Error('Not an array');

      const res = await vectorSearch({
        indexName: searchIndex,
        queryVector,
        topK,
        minScore: minScore > 0 ? minScore : undefined,
        database: database || undefined
      }).unwrap();
      setSearchResult(res);
    } catch {
      setSearchResult(null);
    }
  }, [vectorSearch, searchIndex, vectorInput, topK, minScore, database]);

  const handleCreateIndex = useCallback(async () => {
    if (!newName || !newLabel || !newProperty || newDimensions <= 0) return;
    try {
      await createIndex({
        name: newName,
        label: newLabel,
        property: newProperty,
        dimensions: newDimensions,
        similarity: newSimilarity,
        database: database || undefined
      }).unwrap();
      setNewName('');
      setNewLabel('');
      setNewProperty('');
    } catch {
      // Error handled by RTK Query
    }
  }, [
    createIndex,
    newName,
    newLabel,
    newProperty,
    newDimensions,
    newSimilarity,
    database
  ]);

  const handleDropIndex = useCallback(
    async (name: string) => {
      try {
        await dropIndex({ name, database: database || undefined }).unwrap();
      } catch {
        // Error handled by RTK Query
      }
    },
    [dropIndex, database]
  );

  const indexes = indexData?.indexes ?? [];

  return (
    <>
      <Row className="g-3 mb-3">
        <Col>
          <div className="d-flex align-items-center justify-content-between">
            <h5 className="mb-0">{t('graph.vector.pageTitle')}</h5>
            <Form.Group className="d-flex align-items-center gap-2">
              <Form.Label className="mb-0 small text-muted">
                {t('graph.vector.databaseLabel')}
              </Form.Label>
              <Form.Control
                size="sm"
                type="text"
                placeholder={t('graph.vector.databasePlaceholder')}
                value={database}
                onChange={e => setDatabase(e.target.value)}
                style={{ width: 150 }}
              />
            </Form.Group>
          </div>
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        {/* Vector Indexes */}
        <Col lg={6}>
          <Card className="h-100">
            <Card.Header>
              <div className="d-flex align-items-center justify-content-between">
                <h6 className="mb-0">{t('graph.vector.indexes.title')}</h6>
                <Badge bg="secondary">{indexes.length}</Badge>
              </div>
            </Card.Header>
            <Card.Body>
              {indexLoading ? (
                <div className="text-center">
                  <Spinner size="sm" />
                </div>
              ) : indexes.length === 0 ? (
                <Alert variant="info" className="mb-3">
                  {t('graph.vector.indexes.empty')}
                </Alert>
              ) : (
                <Table size="sm" hover className="mb-3">
                  <thead>
                    <tr>
                      <th>{t('graph.vector.indexes.cols.name')}</th>
                      <th>{t('graph.vector.indexes.cols.label')}</th>
                      <th>{t('graph.vector.indexes.cols.property')}</th>
                      <th>{t('graph.vector.indexes.cols.dimensions')}</th>
                      <th>{t('graph.vector.indexes.cols.similarity')}</th>
                      <th>{t('graph.vector.indexes.cols.state')}</th>
                      <th></th>
                    </tr>
                  </thead>
                  <tbody>
                    {indexes.map(idx => (
                      <tr key={idx.name}>
                        <td className="fw-semibold">{idx.name}</td>
                        <td>
                          <Badge bg="soft-primary" text="dark">
                            :{idx.label}
                          </Badge>
                        </td>
                        <td>
                          <code className="small">{idx.property}</code>
                        </td>
                        <td>{idx.dimensions}</td>
                        <td>
                          <small>{idx.similarity}</small>
                        </td>
                        <td>
                          <Badge
                            bg={
                              idx.state === 'ONLINE' ? 'success' : 'secondary'
                            }
                          >
                            {idx.state}
                          </Badge>
                        </td>
                        <td>
                          <Button
                            variant="outline-danger"
                            size="sm"
                            onClick={() => handleDropIndex(idx.name)}
                          >
                            {t('graph.vector.indexes.drop')}
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}

              {/* Create Index Form */}
              <div className="border rounded p-2">
                <small className="text-muted fw-semibold d-block mb-2">
                  {t('graph.vector.create.title')}
                </small>
                <Row className="g-2 mb-2">
                  <Col xs={6}>
                    <Form.Control
                      size="sm"
                      placeholder={t('graph.vector.create.namePlaceholder')}
                      value={newName}
                      onChange={e => setNewName(e.target.value)}
                    />
                  </Col>
                  <Col xs={6}>
                    <Form.Control
                      size="sm"
                      placeholder={t('graph.vector.create.labelPlaceholder')}
                      value={newLabel}
                      onChange={e => setNewLabel(e.target.value)}
                    />
                  </Col>
                </Row>
                <Row className="g-2 mb-2">
                  <Col>
                    <Form.Control
                      size="sm"
                      placeholder={t('graph.vector.create.propertyPlaceholder')}
                      value={newProperty}
                      onChange={e => setNewProperty(e.target.value)}
                    />
                  </Col>
                  <Col xs={3}>
                    <Form.Control
                      size="sm"
                      type="number"
                      placeholder={t(
                        'graph.vector.create.dimensionsPlaceholder'
                      )}
                      value={newDimensions}
                      onChange={e => setNewDimensions(Number(e.target.value))}
                    />
                  </Col>
                  <Col xs={3}>
                    <Form.Select
                      size="sm"
                      value={newSimilarity}
                      onChange={e =>
                        setNewSimilarity(
                          e.target.value as 'cos' | 'l2sq' | 'ip'
                        )
                      }
                    >
                      <option value="cos">
                        {t('graph.vector.create.similarity.cos')}
                      </option>
                      <option value="l2sq">
                        {t('graph.vector.create.similarity.l2sq')}
                      </option>
                      <option value="ip">
                        {t('graph.vector.create.similarity.ip')}
                      </option>
                    </Form.Select>
                  </Col>
                </Row>
                <Button
                  size="sm"
                  variant="primary"
                  onClick={handleCreateIndex}
                  disabled={creating || !newName || !newLabel || !newProperty}
                >
                  {creating ? (
                    <Spinner size="sm" />
                  ) : (
                    t('graph.vector.create.submit')
                  )}
                </Button>
              </div>
            </Card.Body>
          </Card>
        </Col>

        {/* Vector Search */}
        <Col lg={6}>
          <Card className="h-100">
            <Card.Header>
              <h6 className="mb-0">{t('graph.vector.search.title')}</h6>
            </Card.Header>
            <Card.Body>
              <Form.Group className="mb-2">
                <Form.Label className="small">
                  {t('graph.vector.search.indexLabel')}
                </Form.Label>
                <Form.Select
                  size="sm"
                  value={searchIndex}
                  onChange={e => setSearchIndex(e.target.value)}
                >
                  <option value="">
                    {t('graph.vector.search.indexPlaceholder')}
                  </option>
                  {indexes.map(idx => (
                    <option key={idx.name} value={idx.name}>
                      {t('graph.vector.search.indexOption', {
                        name: idx.name,
                        dimensions: idx.dimensions,
                        similarity: idx.similarity
                      })}
                    </option>
                  ))}
                </Form.Select>
              </Form.Group>

              <Form.Group className="mb-2">
                <Form.Label className="small">
                  {t('graph.vector.search.vectorLabel')}
                </Form.Label>
                <Form.Control
                  as="textarea"
                  rows={3}
                  size="sm"
                  className="font-monospace"
                  placeholder={t('graph.vector.search.vectorPlaceholder')}
                  value={vectorInput}
                  onChange={e => setVectorInput(e.target.value)}
                />
              </Form.Group>

              <Row className="g-2 mb-3">
                <Col>
                  <Form.Group>
                    <Form.Label className="small">
                      {t('graph.vector.search.topKLabel')}
                    </Form.Label>
                    <Form.Control
                      size="sm"
                      type="number"
                      value={topK}
                      onChange={e => setTopK(Number(e.target.value))}
                      min={1}
                      max={1000}
                    />
                  </Form.Group>
                </Col>
                <Col>
                  <Form.Group>
                    <Form.Label className="small">
                      {t('graph.vector.search.minScoreLabel')}
                    </Form.Label>
                    <Form.Control
                      size="sm"
                      type="number"
                      step={0.01}
                      value={minScore}
                      onChange={e => setMinScore(Number(e.target.value))}
                      min={0}
                      max={1}
                    />
                  </Form.Group>
                </Col>
              </Row>

              <Button
                variant="primary"
                size="sm"
                onClick={handleSearch}
                disabled={searching || !searchIndex || !vectorInput}
              >
                {searching ? (
                  <>
                    <Spinner size="sm" className="me-1" />{' '}
                    {t('graph.vector.search.searching')}
                  </>
                ) : (
                  t('graph.vector.search.submit')
                )}
              </Button>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      {/* Search Results */}
      {searchResult && (
        <Row className="g-3">
          <Col>
            <ResultsTable result={searchResult} isLoading={searching} />
          </Col>
        </Row>
      )}
    </>
  );
};

export default VectorSearch;

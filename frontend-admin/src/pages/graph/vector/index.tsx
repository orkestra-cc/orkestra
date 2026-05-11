import { useState, useCallback } from 'react';
import { Row, Col, Card, Button, Form, Table, Badge, Spinner, Alert } from 'react-bootstrap';
import {
  useListVectorIndexesQuery,
  useCreateVectorIndexMutation,
  useDropVectorIndexMutation,
  useVectorSearchMutation,
} from '../../../store/api/graphApi';
import ResultsTable from '../components/ResultsTable';
import type { QueryResult } from '../../../types/graph';

const VectorSearch: React.FC = () => {
  const [database, setDatabase] = useState('');
  const [searchResult, setSearchResult] = useState<QueryResult | null>(null);

  // Vector indexes
  const { data: indexData, isLoading: indexLoading } = useListVectorIndexesQuery(database ? { database } : undefined);
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
  const [newSimilarity, setNewSimilarity] = useState<'cos' | 'l2sq' | 'ip'>('cos');

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
        database: database || undefined,
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
        database: database || undefined,
      }).unwrap();
      setNewName('');
      setNewLabel('');
      setNewProperty('');
    } catch {
      // Error handled by RTK Query
    }
  }, [createIndex, newName, newLabel, newProperty, newDimensions, newSimilarity, database]);

  const handleDropIndex = useCallback(async (name: string) => {
    try {
      await dropIndex({ name, database: database || undefined }).unwrap();
    } catch {
      // Error handled by RTK Query
    }
  }, [dropIndex, database]);

  const indexes = indexData?.indexes ?? [];

  return (
    <>
      <Row className="g-3 mb-3">
        <Col>
          <div className="d-flex align-items-center justify-content-between">
            <h5 className="mb-0">Vector Search</h5>
            <Form.Group className="d-flex align-items-center gap-2">
              <Form.Label className="mb-0 small text-muted">Database:</Form.Label>
              <Form.Control
                size="sm"
                type="text"
                placeholder="default"
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
                <h6 className="mb-0">Vector Indexes</h6>
                <Badge bg="secondary">{indexes.length}</Badge>
              </div>
            </Card.Header>
            <Card.Body>
              {indexLoading ? (
                <div className="text-center"><Spinner size="sm" /></div>
              ) : indexes.length === 0 ? (
                <Alert variant="info" className="mb-3">No vector indexes found.</Alert>
              ) : (
                <Table size="sm" hover className="mb-3">
                  <thead>
                    <tr>
                      <th>Name</th>
                      <th>Label</th>
                      <th>Property</th>
                      <th>Dims</th>
                      <th>Similarity</th>
                      <th>State</th>
                      <th></th>
                    </tr>
                  </thead>
                  <tbody>
                    {indexes.map(idx => (
                      <tr key={idx.name}>
                        <td className="fw-semibold">{idx.name}</td>
                        <td><Badge bg="soft-primary" text="dark">:{idx.label}</Badge></td>
                        <td><code className="small">{idx.property}</code></td>
                        <td>{idx.dimensions}</td>
                        <td><small>{idx.similarity}</small></td>
                        <td>
                          <Badge bg={idx.state === 'ONLINE' ? 'success' : 'secondary'}>{idx.state}</Badge>
                        </td>
                        <td>
                          <Button variant="outline-danger" size="sm" onClick={() => handleDropIndex(idx.name)}>
                            Drop
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}

              {/* Create Index Form */}
              <div className="border rounded p-2">
                <small className="text-muted fw-semibold d-block mb-2">Create Vector Index</small>
                <Row className="g-2 mb-2">
                  <Col xs={6}>
                    <Form.Control size="sm" placeholder="Index name" value={newName} onChange={e => setNewName(e.target.value)} />
                  </Col>
                  <Col xs={6}>
                    <Form.Control size="sm" placeholder="Label" value={newLabel} onChange={e => setNewLabel(e.target.value)} />
                  </Col>
                </Row>
                <Row className="g-2 mb-2">
                  <Col>
                    <Form.Control size="sm" placeholder="Property" value={newProperty} onChange={e => setNewProperty(e.target.value)} />
                  </Col>
                  <Col xs={3}>
                    <Form.Control size="sm" type="number" placeholder="Dims" value={newDimensions} onChange={e => setNewDimensions(Number(e.target.value))} />
                  </Col>
                  <Col xs={3}>
                    <Form.Select size="sm" value={newSimilarity} onChange={e => setNewSimilarity(e.target.value as 'cos' | 'l2sq' | 'ip')}>
                      <option value="cos">Cosine</option>
                      <option value="l2sq">Euclidean (L2)</option>
                      <option value="ip">Inner Product</option>
                    </Form.Select>
                  </Col>
                </Row>
                <Button size="sm" variant="primary" onClick={handleCreateIndex} disabled={creating || !newName || !newLabel || !newProperty}>
                  {creating ? <Spinner size="sm" /> : 'Create Index'}
                </Button>
              </div>
            </Card.Body>
          </Card>
        </Col>

        {/* Vector Search */}
        <Col lg={6}>
          <Card className="h-100">
            <Card.Header>
              <h6 className="mb-0">Similarity Search</h6>
            </Card.Header>
            <Card.Body>
              <Form.Group className="mb-2">
                <Form.Label className="small">Index</Form.Label>
                <Form.Select size="sm" value={searchIndex} onChange={e => setSearchIndex(e.target.value)}>
                  <option value="">Select index...</option>
                  {indexes.map(idx => (
                    <option key={idx.name} value={idx.name}>
                      {idx.name} ({idx.dimensions}d, {idx.similarity})
                    </option>
                  ))}
                </Form.Select>
              </Form.Group>

              <Form.Group className="mb-2">
                <Form.Label className="small">Query Vector (JSON array)</Form.Label>
                <Form.Control
                  as="textarea"
                  rows={3}
                  size="sm"
                  className="font-monospace"
                  placeholder="[0.1, 0.2, 0.3, ...]"
                  value={vectorInput}
                  onChange={e => setVectorInput(e.target.value)}
                />
              </Form.Group>

              <Row className="g-2 mb-3">
                <Col>
                  <Form.Group>
                    <Form.Label className="small">Top K</Form.Label>
                    <Form.Control size="sm" type="number" value={topK} onChange={e => setTopK(Number(e.target.value))} min={1} max={1000} />
                  </Form.Group>
                </Col>
                <Col>
                  <Form.Group>
                    <Form.Label className="small">Min Score</Form.Label>
                    <Form.Control size="sm" type="number" step={0.01} value={minScore} onChange={e => setMinScore(Number(e.target.value))} min={0} max={1} />
                  </Form.Group>
                </Col>
              </Row>

              <Button
                variant="primary"
                size="sm"
                onClick={handleSearch}
                disabled={searching || !searchIndex || !vectorInput}
              >
                {searching ? <><Spinner size="sm" className="me-1" /> Searching...</> : 'Search'}
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

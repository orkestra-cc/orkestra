import { Row, Col, Card, Table, Badge, Spinner, Alert } from 'react-bootstrap';
import { useListDatabasesQuery, useGraphHealthQuery, useGetSchemaQuery } from '../../../store/api/graphApi';

const GraphDatabases: React.FC = () => {
  const { data: healthData, isLoading: healthLoading, error: healthError } = useGraphHealthQuery();
  const { data: dbData, isLoading: dbLoading } = useListDatabasesQuery();

  return (
    <>
      <Row className="g-3 mb-3">
        <Col>
          <h5 className="mb-0">Graph Databases</h5>
        </Col>
      </Row>

      {/* Connection Status */}
      <Row className="g-3 mb-3">
        <Col md={6} lg={4}>
          <Card>
            <Card.Body>
              <h6 className="text-muted mb-2">Connection Status</h6>
              {healthLoading ? (
                <Spinner size="sm" />
              ) : healthError ? (
                <Badge bg="danger">Disconnected</Badge>
              ) : (
                <>
                  <Badge bg="success" className="me-2">Connected</Badge>
                  <small className="text-muted">{healthData?.uri}</small>
                </>
              )}
            </Card.Body>
          </Card>
        </Col>
        <Col md={6} lg={4}>
          <Card>
            <Card.Body>
              <h6 className="text-muted mb-2">Databases</h6>
              {dbLoading ? (
                <Spinner size="sm" />
              ) : (
                <span className="fs-4 fw-bold">
                  {dbData?.databases?.filter(d => d.currentStatus === 'online').length ?? 0}
                </span>
              )}
              <small className="text-muted ms-2">online</small>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      {/* Database List */}
      <Row className="g-3 mb-3">
        <Col>
          <Card>
            <Card.Header>
              <h6 className="mb-0">Available Databases</h6>
            </Card.Header>
            <Card.Body className="p-0">
              {dbLoading ? (
                <div className="text-center p-3"><Spinner size="sm" /></div>
              ) : !dbData?.databases?.length ? (
                <Alert variant="info" className="m-3">No databases found</Alert>
              ) : (
                <Table striped hover responsive className="mb-0">
                  <thead>
                    <tr>
                      <th>Name</th>
                      <th>Status</th>
                      <th>Address</th>
                      <th>Default</th>
                      <th>Home</th>
                    </tr>
                  </thead>
                  <tbody>
                    {dbData.databases.map(db => (
                      <tr key={db.name}>
                        <td className="fw-semibold">{db.name}</td>
                        <td>
                          <Badge bg={db.currentStatus === 'online' ? 'success' : 'danger'}>
                            {db.currentStatus}
                          </Badge>
                        </td>
                        <td><small className="text-muted">{db.address || '-'}</small></td>
                        <td>{db.default ? <Badge bg="primary">Default</Badge> : '-'}</td>
                        <td>{db.home ? <Badge bg="info">Home</Badge> : '-'}</td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}
            </Card.Body>
          </Card>
        </Col>
      </Row>

      {/* Schema Overview for each online database */}
      {dbData?.databases?.filter(d => d.currentStatus === 'online').map(db => (
        <Row key={db.name} className="g-3 mb-3">
          <Col>
            <DatabaseSchemaCard database={db.name} />
          </Col>
        </Row>
      ))}
    </>
  );
};

const DatabaseSchemaCard: React.FC<{ database: string }> = ({ database }) => {
  const { data: schema, isLoading } = useGetSchemaQuery({ database });

  return (
    <Card>
      <Card.Header>
        <div className="d-flex align-items-center justify-content-between">
          <h6 className="mb-0">Schema: {database}</h6>
          {schema && (
            <div>
              <Badge bg="primary" className="me-2">{schema.nodeCount} nodes</Badge>
              <Badge bg="warning" text="dark">{schema.relationshipCount} relationships</Badge>
            </div>
          )}
        </div>
      </Card.Header>
      <Card.Body>
        {isLoading ? (
          <div className="text-center"><Spinner size="sm" /></div>
        ) : !schema ? (
          <small className="text-muted">Unable to fetch schema</small>
        ) : (
          <Row>
            <Col md={4}>
              <h6 className="text-muted small">Labels ({schema.labels?.length ?? 0})</h6>
              {schema.labels?.map(l => (
                <div key={l.name} className="mb-1">
                  <Badge bg="soft-primary" text="dark" className="me-1">:{l.name}</Badge>
                  <small className="text-muted">{l.count}</small>
                </div>
              ))}
            </Col>
            <Col md={4}>
              <h6 className="text-muted small">Relationship Types ({schema.relationshipTypes?.length ?? 0})</h6>
              {schema.relationshipTypes?.map(r => (
                <div key={r.name} className="mb-1">
                  <Badge bg="soft-warning" text="dark" className="me-1">{r.name}</Badge>
                  <small className="text-muted">{r.count}</small>
                </div>
              ))}
            </Col>
            <Col md={4}>
              <h6 className="text-muted small">Indexes ({schema.indexes?.length ?? 0})</h6>
              {schema.indexes?.map(i => (
                <div key={i.name} className="mb-1">
                  <small>
                    <Badge bg={i.state === 'ONLINE' ? 'soft-success' : 'soft-secondary'} text="dark" className="me-1">
                      {i.type}
                    </Badge>
                    {i.name}
                  </small>
                </div>
              ))}
            </Col>
          </Row>
        )}
      </Card.Body>
    </Card>
  );
};

export default GraphDatabases;

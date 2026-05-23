import { Row, Col, Card, Table, Badge, Spinner, Alert } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useListDatabasesQuery,
  useGraphHealthQuery,
  useGetSchemaQuery
} from '../../../store/api/graphApi';

const GraphDatabases: React.FC = () => {
  const { t } = useTranslation();
  const {
    data: healthData,
    isLoading: healthLoading,
    error: healthError
  } = useGraphHealthQuery();
  const { data: dbData, isLoading: dbLoading } = useListDatabasesQuery();

  return (
    <>
      <Row className="g-3 mb-3">
        <Col>
          <h5 className="mb-0">{t('graph.databases.pageTitle')}</h5>
        </Col>
      </Row>

      {/* Connection Status */}
      <Row className="g-3 mb-3">
        <Col md={6} lg={4}>
          <Card>
            <Card.Body>
              <h6 className="text-muted mb-2">
                {t('graph.databases.connection.title')}
              </h6>
              {healthLoading ? (
                <Spinner size="sm" />
              ) : healthError ? (
                <Badge bg="danger">
                  {t('graph.databases.connection.disconnected')}
                </Badge>
              ) : (
                <>
                  <Badge bg="success" className="me-2">
                    {t('graph.databases.connection.connected')}
                  </Badge>
                  <small className="text-muted">{healthData?.uri}</small>
                </>
              )}
            </Card.Body>
          </Card>
        </Col>
        <Col md={6} lg={4}>
          <Card>
            <Card.Body>
              <h6 className="text-muted mb-2">
                {t('graph.databases.online.title')}
              </h6>
              {dbLoading ? (
                <Spinner size="sm" />
              ) : (
                <span className="fs-4 fw-bold">
                  {dbData?.databases?.filter(d => d.currentStatus === 'online')
                    .length ?? 0}
                </span>
              )}
              <small className="text-muted ms-2">
                {t('graph.databases.online.suffix')}
              </small>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      {/* Database List */}
      <Row className="g-3 mb-3">
        <Col>
          <Card>
            <Card.Header>
              <h6 className="mb-0">{t('graph.databases.list.title')}</h6>
            </Card.Header>
            <Card.Body className="p-0">
              {dbLoading ? (
                <div className="text-center p-3">
                  <Spinner size="sm" />
                </div>
              ) : !dbData?.databases?.length ? (
                <Alert variant="info" className="m-3">
                  {t('graph.databases.list.empty')}
                </Alert>
              ) : (
                <Table striped hover responsive className="mb-0">
                  <thead>
                    <tr>
                      <th>{t('graph.databases.list.cols.name')}</th>
                      <th>{t('graph.databases.list.cols.status')}</th>
                      <th>{t('graph.databases.list.cols.address')}</th>
                      <th>{t('graph.databases.list.cols.default')}</th>
                      <th>{t('graph.databases.list.cols.home')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {dbData.databases.map(db => (
                      <tr key={db.name}>
                        <td className="fw-semibold">{db.name}</td>
                        <td>
                          <Badge
                            bg={
                              db.currentStatus === 'online'
                                ? 'success'
                                : 'danger'
                            }
                          >
                            {db.currentStatus}
                          </Badge>
                        </td>
                        <td>
                          <small className="text-muted">
                            {db.address || '-'}
                          </small>
                        </td>
                        <td>
                          {db.default ? (
                            <Badge bg="primary">
                              {t('graph.databases.list.defaultBadge')}
                            </Badge>
                          ) : (
                            '-'
                          )}
                        </td>
                        <td>
                          {db.home ? (
                            <Badge bg="info">
                              {t('graph.databases.list.homeBadge')}
                            </Badge>
                          ) : (
                            '-'
                          )}
                        </td>
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
      {dbData?.databases
        ?.filter(d => d.currentStatus === 'online')
        .map(db => (
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
  const { t } = useTranslation();
  const { data: schema, isLoading } = useGetSchemaQuery({ database });

  return (
    <Card>
      <Card.Header>
        <div className="d-flex align-items-center justify-content-between">
          <h6 className="mb-0">
            {t('graph.databases.schema.title', { database })}
          </h6>
          {schema && (
            <div>
              <Badge bg="primary" className="me-2">
                {t('graph.databases.schema.nodes', { count: schema.nodeCount })}
              </Badge>
              <Badge bg="warning" text="dark">
                {t('graph.databases.schema.relationships', {
                  count: schema.relationshipCount
                })}
              </Badge>
            </div>
          )}
        </div>
      </Card.Header>
      <Card.Body>
        {isLoading ? (
          <div className="text-center">
            <Spinner size="sm" />
          </div>
        ) : !schema ? (
          <small className="text-muted">
            {t('graph.databases.schema.unavailable')}
          </small>
        ) : (
          <Row>
            <Col md={4}>
              <h6 className="text-muted small">
                {t('graph.databases.schema.labelsHeading', {
                  count: schema.labels?.length ?? 0
                })}
              </h6>
              {schema.labels?.map(l => (
                <div key={l.name} className="mb-1">
                  <Badge bg="soft-primary" text="dark" className="me-1">
                    :{l.name}
                  </Badge>
                  <small className="text-muted">{l.count}</small>
                </div>
              ))}
            </Col>
            <Col md={4}>
              <h6 className="text-muted small">
                {t('graph.databases.schema.relTypesHeading', {
                  count: schema.relationshipTypes?.length ?? 0
                })}
              </h6>
              {schema.relationshipTypes?.map(r => (
                <div key={r.name} className="mb-1">
                  <Badge bg="soft-warning" text="dark" className="me-1">
                    {r.name}
                  </Badge>
                  <small className="text-muted">{r.count}</small>
                </div>
              ))}
            </Col>
            <Col md={4}>
              <h6 className="text-muted small">
                {t('graph.databases.schema.indexesHeading', {
                  count: schema.indexes?.length ?? 0
                })}
              </h6>
              {schema.indexes?.map(i => (
                <div key={i.name} className="mb-1">
                  <small>
                    <Badge
                      bg={
                        i.state === 'ONLINE' ? 'soft-success' : 'soft-secondary'
                      }
                      text="dark"
                      className="me-1"
                    >
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

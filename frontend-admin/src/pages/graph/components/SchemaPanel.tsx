import {
  Accordion,
  Badge,
  Form,
  ListGroup,
  Spinner,
  Alert
} from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useGetSchemaQuery,
  useListDatabasesQuery
} from '../../../store/api/graphApi';
import { useListDocumentsQuery } from '../../../store/api/ragApi';
import type {
  LabelInfo,
  RelTypeInfo,
  IndexInfo,
  ConstraintInfo
} from '../../../types/graph';

interface SchemaPanelProps {
  database?: string;
  selectedDocumentUuid?: string;
  onLabelClick?: (label: string) => void;
  onRelTypeClick?: (type: string) => void;
  onDatabaseChange?: (database: string) => void;
  onDocumentChange?: (documentUuid: string) => void;
}

function LabelItem({
  label,
  onClick
}: {
  label: LabelInfo;
  onClick?: (name: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <Accordion.Item eventKey={`label-${label.name}`}>
      <Accordion.Header>
        <div className="d-flex align-items-center justify-content-between w-100 me-2">
          <span
            role="button"
            className="fw-semibold text-primary"
            style={{ cursor: 'pointer' }}
            onClick={e => {
              e.stopPropagation();
              onClick?.(label.name);
            }}
            title={t('graph.schema.browseLabelTitle', { name: label.name })}
          >
            :{label.name}
          </span>
          <Badge bg="primary" pill className="ms-2">
            {label.count.toLocaleString()}
          </Badge>
        </div>
      </Accordion.Header>
      <Accordion.Body className="p-0">
        {(label.properties?.length ?? 0) > 0 ? (
          <ListGroup variant="flush">
            {label.properties.map(prop => (
              <ListGroup.Item key={prop} className="py-1 px-3 fs-10">
                <i className="fas fa-circle fa-xs text-400 me-2" />
                <span className="font-monospace">{prop}</span>
              </ListGroup.Item>
            ))}
          </ListGroup>
        ) : (
          <div className="px-3 py-2 text-muted fs-10">
            {t('graph.schema.noProperties')}
          </div>
        )}
      </Accordion.Body>
    </Accordion.Item>
  );
}

function RelTypeItem({
  rel,
  onClick
}: {
  rel: RelTypeInfo;
  onClick?: (type: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <Accordion.Item eventKey={`rel-${rel.name}`}>
      <Accordion.Header>
        <div className="d-flex align-items-center justify-content-between w-100 me-2">
          <span
            role="button"
            className="fw-semibold text-warning"
            style={{ cursor: 'pointer' }}
            onClick={e => {
              e.stopPropagation();
              onClick?.(rel.name);
            }}
            title={t('graph.schema.browseRelTitle', { name: rel.name })}
          >
            :{rel.name}
          </span>
          <Badge bg="warning" text="dark" pill className="ms-2">
            {rel.count.toLocaleString()}
          </Badge>
        </div>
      </Accordion.Header>
      <Accordion.Body className="p-0">
        {(rel.properties?.length ?? 0) > 0 ? (
          <ListGroup variant="flush">
            {rel.properties.map(prop => (
              <ListGroup.Item key={prop} className="py-1 px-3 fs-10">
                <i className="fas fa-circle fa-xs text-400 me-2" />
                <span className="font-monospace">{prop}</span>
              </ListGroup.Item>
            ))}
          </ListGroup>
        ) : (
          <div className="px-3 py-2 text-muted fs-10">
            {t('graph.schema.noProperties')}
          </div>
        )}
      </Accordion.Body>
    </Accordion.Item>
  );
}

function IndexItem({ index }: { index: IndexInfo }) {
  const { t } = useTranslation();
  return (
    <ListGroup.Item className="py-2 px-3">
      <div className="d-flex align-items-center justify-content-between">
        <span className="fw-semibold fs-10">{index.name}</span>
        <Badge
          bg={index.state === 'ONLINE' ? 'success' : 'secondary'}
          className="ms-2 fs-11"
        >
          {index.state}
        </Badge>
      </div>
      <div className="fs-10 text-muted mt-1">
        <span className="me-2">
          <strong>{t('graph.schema.indexTypePrefix')}</strong> {index.type}
        </span>
        {(index.labels?.length ?? 0) > 0 && (
          <span className="me-2">
            <strong>{t('graph.schema.indexLabelsPrefix')}</strong>{' '}
            {index.labels.join(', ')}
          </span>
        )}
        {(index.properties?.length ?? 0) > 0 && (
          <span>
            <strong>{t('graph.schema.indexPropsPrefix')}</strong>{' '}
            <span className="font-monospace">
              {index.properties.join(', ')}
            </span>
          </span>
        )}
      </div>
    </ListGroup.Item>
  );
}

function ConstraintItem({ constraint }: { constraint: ConstraintInfo }) {
  const { t } = useTranslation();
  return (
    <ListGroup.Item className="py-2 px-3">
      <div className="fw-semibold fs-10">{constraint.name}</div>
      <div className="fs-10 text-muted mt-1">
        <span className="me-2">
          <strong>{t('graph.schema.indexTypePrefix')}</strong> {constraint.type}
        </span>
        {(constraint.labels?.length ?? 0) > 0 && (
          <span className="me-2">
            <strong>{t('graph.schema.indexLabelsPrefix')}</strong>{' '}
            {constraint.labels.join(', ')}
          </span>
        )}
        {(constraint.properties?.length ?? 0) > 0 && (
          <span>
            <strong>{t('graph.schema.indexPropsPrefix')}</strong>{' '}
            <span className="font-monospace">
              {constraint.properties.join(', ')}
            </span>
          </span>
        )}
      </div>
    </ListGroup.Item>
  );
}

const SchemaPanel = ({
  database,
  selectedDocumentUuid,
  onLabelClick,
  onRelTypeClick,
  onDatabaseChange,
  onDocumentChange
}: SchemaPanelProps) => {
  const { t } = useTranslation();
  const {
    data: schema,
    isLoading,
    error
  } = useGetSchemaQuery(database ? { database } : {}, {
    pollingInterval: 15000
  });
  const { data: dbData } = useListDatabasesQuery();
  const { data: docsData } = useListDocumentsQuery({ status: 'completed' });
  const completedDocs = docsData?.documents ?? [];

  if (isLoading) {
    return (
      <div className="d-flex justify-content-center align-items-center py-5">
        <Spinner
          animation="border"
          variant="primary"
          size="sm"
          className="me-2"
        />
        <span className="text-muted fs-10">{t('graph.schema.loading')}</span>
      </div>
    );
  }

  if (error) {
    return (
      <Alert variant="danger" className="m-2 fs-10">
        {t('graph.schema.error')}
      </Alert>
    );
  }

  if (!schema) {
    return (
      <div className="d-flex justify-content-center align-items-center py-5">
        <span className="text-muted fs-10">{t('graph.schema.empty')}</span>
      </div>
    );
  }

  const databases = dbData?.databases ?? [];

  return (
    <div className="d-flex flex-column h-100">
      {/* Database selector */}
      {databases.length > 0 && (
        <div className="px-3 py-2 border-bottom">
          <Form.Select
            size="sm"
            value={database ?? ''}
            onChange={e => onDatabaseChange?.(e.target.value)}
            className="fs-10"
          >
            <option value="">{t('graph.schema.databaseDefaultOption')}</option>
            {databases.map(db => (
              <option key={db.name} value={db.name}>
                {db.name}
                {db.default ? t('graph.schema.databaseDefaultSuffix') : ''}
                {db.home ? t('graph.schema.databaseHomeSuffix') : ''}
              </option>
            ))}
          </Form.Select>
        </div>
      )}

      {/* Document filter */}
      <div className="px-3 py-2 border-bottom">
        <Form.Label className="fs-10 mb-1 text-muted">
          {t('graph.schema.documentScopeLabel')}
        </Form.Label>
        <Form.Select
          size="sm"
          value={selectedDocumentUuid ?? ''}
          onChange={e => onDocumentChange?.(e.target.value)}
          className="fs-10"
        >
          <option value="">{t('graph.schema.documentScopeAll')}</option>
          {completedDocs.map(doc => (
            <option key={doc.uuid} value={doc.uuid}>
              {doc.title}
              {doc.isoStandard ? ` (${doc.isoStandard})` : ''}
              {doc.llmModelName ? ` [${doc.llmModelName}]` : ''}
            </option>
          ))}
        </Form.Select>
      </div>

      {/* Summary badges */}
      <div className="d-flex gap-2 px-3 py-2 border-bottom">
        <Badge bg="info">
          <i className="fas fa-circle-nodes me-1" />
          {t('graph.schema.nodesBadge', {
            count: schema.nodeCount
          })}
        </Badge>
        <Badge bg="warning" text="dark">
          <i className="fas fa-arrows-left-right me-1" />
          {t('graph.schema.relsBadge', {
            count: schema.relationshipCount
          })}
        </Badge>
      </div>

      {/* Schema accordion */}
      <div className="flex-grow-1 overflow-auto">
        <Accordion flush defaultActiveKey="labels">
          {/* Labels section */}
          <Accordion.Item eventKey="labels">
            <Accordion.Header>
              <span className="fw-bold me-2">
                {t('graph.schema.sections.labels')}
              </span>
              <Badge bg="secondary" pill>
                {schema.labels?.length ?? 0}
              </Badge>
            </Accordion.Header>
            <Accordion.Body className="p-0">
              {(schema.labels?.length ?? 0) > 0 ? (
                <Accordion flush>
                  {schema.labels.map(label => (
                    <LabelItem
                      key={label.name}
                      label={label}
                      onClick={onLabelClick}
                    />
                  ))}
                </Accordion>
              ) : (
                <div className="px-3 py-2 text-muted fs-10">
                  {t('graph.schema.emptyLabels')}
                </div>
              )}
            </Accordion.Body>
          </Accordion.Item>

          {/* Relationship Types section */}
          <Accordion.Item eventKey="relationships">
            <Accordion.Header>
              <span className="fw-bold me-2">
                {t('graph.schema.sections.relationshipTypes')}
              </span>
              <Badge bg="secondary" pill>
                {schema.relationshipTypes?.length ?? 0}
              </Badge>
            </Accordion.Header>
            <Accordion.Body className="p-0">
              {(schema.relationshipTypes?.length ?? 0) > 0 ? (
                <Accordion flush>
                  {schema.relationshipTypes.map(rel => (
                    <RelTypeItem
                      key={rel.name}
                      rel={rel}
                      onClick={onRelTypeClick}
                    />
                  ))}
                </Accordion>
              ) : (
                <div className="px-3 py-2 text-muted fs-10">
                  {t('graph.schema.emptyRelTypes')}
                </div>
              )}
            </Accordion.Body>
          </Accordion.Item>

          {/* Indexes section */}
          <Accordion.Item eventKey="indexes">
            <Accordion.Header>
              <span className="fw-bold me-2">
                {t('graph.schema.sections.indexes')}
              </span>
              <Badge bg="secondary" pill>
                {schema.indexes?.length ?? 0}
              </Badge>
            </Accordion.Header>
            <Accordion.Body className="p-0">
              {(schema.indexes?.length ?? 0) > 0 ? (
                <ListGroup variant="flush">
                  {schema.indexes.map((index, i) => (
                    <IndexItem key={`${index.name}-${i}`} index={index} />
                  ))}
                </ListGroup>
              ) : (
                <div className="px-3 py-2 text-muted fs-10">
                  {t('graph.schema.emptyIndexes')}
                </div>
              )}
            </Accordion.Body>
          </Accordion.Item>

          {/* Constraints section */}
          <Accordion.Item eventKey="constraints">
            <Accordion.Header>
              <span className="fw-bold me-2">
                {t('graph.schema.sections.constraints')}
              </span>
              <Badge bg="secondary" pill>
                {schema.constraints?.length ?? 0}
              </Badge>
            </Accordion.Header>
            <Accordion.Body className="p-0">
              {(schema.constraints?.length ?? 0) > 0 ? (
                <ListGroup variant="flush">
                  {schema.constraints.map((constraint, i) => (
                    <ConstraintItem
                      key={`${constraint.name}-${i}`}
                      constraint={constraint}
                    />
                  ))}
                </ListGroup>
              ) : (
                <div className="px-3 py-2 text-muted fs-10">
                  {t('graph.schema.emptyConstraints')}
                </div>
              )}
            </Accordion.Body>
          </Accordion.Item>
        </Accordion>
      </div>
    </div>
  );
};

export default SchemaPanel;

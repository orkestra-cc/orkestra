import { useMemo } from 'react';
import { Table, Badge, Spinner, Alert } from 'react-bootstrap';
import type { QueryResult, QueryMetadata } from '../../../types/graph';

interface ResultsTableProps {
  result: QueryResult | null;
  isLoading?: boolean;
}

const MAX_STRING_LENGTH = 120;

function truncate(value: string): string {
  return value.length > MAX_STRING_LENGTH
    ? value.slice(0, MAX_STRING_LENGTH) + '...'
    : value;
}

function isNode(value: unknown): value is { labels: string[]; properties?: Record<string, unknown> } {
  return (
    typeof value === 'object' &&
    value !== null &&
    'labels' in value &&
    Array.isArray((value as Record<string, unknown>).labels)
  );
}

function isRelationship(value: unknown): value is { type: string; startNodeId: number } {
  return (
    typeof value === 'object' &&
    value !== null &&
    'type' in value &&
    'startNodeId' in value
  );
}

function renderCell(value: unknown): React.ReactNode {
  if (value === null || value === undefined) {
    return <span className="text-muted fst-italic">null</span>;
  }

  if (isNode(value)) {
    return (
      <span>
        {value.labels.map((label) => (
          <Badge key={label} bg="info" className="me-1">
            :{label}
          </Badge>
        ))}
      </span>
    );
  }

  if (isRelationship(value)) {
    return (
      <Badge bg="warning" text="dark">
        -[:{value.type}]-&gt;
      </Badge>
    );
  }

  if (Array.isArray(value)) {
    return (
      <span className="font-monospace fs-10">
        [{value.map((item, i) => (
          <span key={i}>
            {i > 0 && ', '}
            {renderCell(item)}
          </span>
        ))}]
      </span>
    );
  }

  if (typeof value === 'object') {
    const json = JSON.stringify(value, null, 0);
    return (
      <code className="fs-10 text-break">{truncate(json)}</code>
    );
  }

  if (typeof value === 'boolean') {
    return (
      <Badge bg={value ? 'success' : 'secondary'}>
        {String(value)}
      </Badge>
    );
  }

  if (typeof value === 'number') {
    return <span className="font-monospace">{value}</span>;
  }

  return <span>{truncate(String(value))}</span>;
}

function formatDuration(ms: number): string {
  if (ms < 1) return `${(ms * 1000).toFixed(0)} us`;
  if (ms < 1000) return `${ms.toFixed(0)} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
}

function MetadataBar({ metadata, hasGraph }: { metadata: QueryMetadata; hasGraph: boolean }) {
  const stats = useMemo(() => {
    const items: { label: string; value: number }[] = [];
    if (metadata.nodesCreated) items.push({ label: 'Nodes created', value: metadata.nodesCreated });
    if (metadata.nodesDeleted) items.push({ label: 'Nodes deleted', value: metadata.nodesDeleted });
    if (metadata.relationshipsCreated) items.push({ label: 'Rels created', value: metadata.relationshipsCreated });
    if (metadata.relationshipsDeleted) items.push({ label: 'Rels deleted', value: metadata.relationshipsDeleted });
    if (metadata.propertiesSet) items.push({ label: 'Props set', value: metadata.propertiesSet });
    if (metadata.labelsAdded) items.push({ label: 'Labels added', value: metadata.labelsAdded });
    if (metadata.labelsRemoved) items.push({ label: 'Labels removed', value: metadata.labelsRemoved });
    return items;
  }, [metadata]);

  return (
    <div className="d-flex flex-wrap align-items-center gap-2 px-3 py-2 bg-body-tertiary border-top fs-10">
      <Badge bg="secondary">
        {formatDuration(metadata.executionTimeMs)}
      </Badge>
      <Badge bg="primary">
        {metadata.resultCount} {metadata.resultCount === 1 ? 'row' : 'rows'}
      </Badge>
      {stats.map(({ label, value }) => (
        <Badge key={label} bg="success">
          {label}: {value}
        </Badge>
      ))}
      {hasGraph && (
        <Badge bg="info">
          <i className="fas fa-project-diagram me-1" />
          Graph data available
        </Badge>
      )}
    </div>
  );
}

const ResultsTable = ({ result, isLoading }: ResultsTableProps) => {
  if (isLoading) {
    return (
      <div className="d-flex justify-content-center align-items-center py-5">
        <Spinner animation="border" variant="primary" size="sm" className="me-2" />
        <span className="text-muted">Running query...</span>
      </div>
    );
  }

  if (!result) {
    return null;
  }

  if (!result.rows || result.rows.length === 0) {
    return (
      <div>
        <Alert variant="info" className="mb-0 rounded-0 border-start-0 border-end-0">
          <span className="fw-semibold">No results.</span>{' '}
          {result.metadata.containsUpdates
            ? 'Query executed successfully with updates.'
            : 'The query returned an empty result set.'}
        </Alert>
        <MetadataBar metadata={result.metadata} hasGraph={!!result.graph} />
      </div>
    );
  }

  return (
    <div>
      <div className="table-responsive">
        <Table striped hover size="sm" className="fs-10 mb-0">
          <thead className="bg-body-tertiary">
            <tr>
              {(result.columns ?? []).map((col) => (
                <th key={col} className="text-900 text-nowrap px-3 py-2">
                  {col}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {(result.rows ?? []).map((row, rowIndex) => (
              <tr key={rowIndex}>
                {(result.columns ?? []).map((col) => (
                  <td key={col} className="px-3 py-2 align-middle">
                    {renderCell(row[col])}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </Table>
      </div>
      <MetadataBar metadata={result.metadata} hasGraph={!!result.graph} />
    </div>
  );
};

export default ResultsTable;

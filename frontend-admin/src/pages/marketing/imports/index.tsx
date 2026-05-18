// Imports audit list — read-only summary of every CSV import that
// ran in this tenant. The wizard for kicking new imports lives at
// /marketing/imports/new.

import { Card, Table, Badge, Button } from 'react-bootstrap';
import { Link } from 'react-router';
import { useListMarketingImportsQuery } from 'store/api/marketingApi';
import type { ImportJobStatus } from 'types/marketing';

const statusVariant: Record<ImportJobStatus, string> = {
  queued: 'secondary',
  running: 'info',
  done: 'success',
  failed: 'danger'
};

const ImportsPage: React.FC = () => {
  const { data, isLoading, refetch } = useListMarketingImportsQuery(undefined);

  return (
    <>
      <div className="mb-3 d-flex justify-content-between align-items-center">
        <div>
          <h3 className="fw-normal mb-1">Imports</h3>
          <p className="fs-10 text-muted mb-0">
            Audit log of every contact-base import. Phase 1 ships CSV; Excel +
            Odoo adapters arrive in Phase 3.
          </p>
        </div>
        <div className="d-flex gap-2">
          <Button
            variant="outline-secondary"
            size="sm"
            onClick={() => refetch()}
          >
            Refresh
          </Button>
          <Link to="/marketing/imports/new" className="btn btn-primary btn-sm">
            New import
          </Link>
        </div>
      </div>

      <Card>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="p-3 text-muted">Loading…</div>
          ) : !data?.items?.length ? (
            <div className="p-3 text-muted">
              No imports yet. Click <strong>New import</strong> to upload a CSV.
            </div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>Source</th>
                  <th>Adapter</th>
                  <th>Status</th>
                  <th>Rows</th>
                  <th>Created</th>
                  <th>Created · Merged</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map(j => (
                  <tr key={j.uuid}>
                    <td className="fw-medium">
                      {j.sourceName || (
                        <span className="text-muted">(unnamed)</span>
                      )}
                      <div className="text-muted fs-10">
                        <code>{j.uuid.slice(0, 8)}</code>
                      </div>
                    </td>
                    <td>
                      <Badge bg="light" text="dark">
                        {j.importer}
                      </Badge>
                    </td>
                    <td>
                      <Badge bg={statusVariant[j.status]}>{j.status}</Badge>
                      {j.error && (
                        <div className="text-danger fs-10 mt-1">{j.error}</div>
                      )}
                    </td>
                    <td>
                      {j.stats.rowsRead}
                      {j.stats.rowsFailed ? (
                        <span className="text-danger fs-10">
                          {' '}
                          ({j.stats.rowsFailed} failed)
                        </span>
                      ) : null}
                      {j.stats.conflictsSkipped ? (
                        <span className="text-warning fs-10">
                          {' '}
                          ({j.stats.conflictsSkipped} conflicts)
                        </span>
                      ) : null}
                    </td>
                    <td>
                      <small className="text-muted">
                        {new Date(j.createdAt).toLocaleString()}
                      </small>
                    </td>
                    <td>
                      <small>
                        Orgs: {j.stats.orgsCreated ?? 0} ·{' '}
                        {j.stats.orgsMerged ?? 0}
                      </small>
                      <br />
                      <small>
                        Persons: {j.stats.personsCreated ?? 0} ·{' '}
                        {j.stats.personsMerged ?? 0}
                      </small>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card.Body>
      </Card>
    </>
  );
};

export default ImportsPage;

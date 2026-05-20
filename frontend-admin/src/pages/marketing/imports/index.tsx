// Imports audit list — read-only summary of every CSV import that
// ran in this tenant. The wizard for kicking new imports lives at
// /marketing/imports/new.

import { Card, Table, Badge, Button } from 'react-bootstrap';
import { Link } from 'react-router';
import { Trans, useTranslation } from 'react-i18next';
import { useListMarketingImportsQuery } from 'store/api/marketingApi';
import type { ImportJobStatus } from 'types/marketing';

const statusVariant: Record<ImportJobStatus, string> = {
  queued: 'secondary',
  running: 'info',
  done: 'success',
  failed: 'danger'
};

const ImportsPage: React.FC = () => {
  const { t } = useTranslation();
  const { data, isLoading, refetch } = useListMarketingImportsQuery(undefined);

  return (
    <>
      <div className="mb-3 d-flex justify-content-between align-items-center">
        <div>
          <h3 className="fw-normal mb-1">{t('marketing.imports.title')}</h3>
          <p className="fs-10 text-muted mb-0">
            {t('marketing.imports.list.subtitle')}
          </p>
        </div>
        <div className="d-flex gap-2">
          <Button
            variant="outline-secondary"
            size="sm"
            onClick={() => refetch()}
          >
            {t('marketing.imports.list.refresh')}
          </Button>
          <Link to="/marketing/imports/new" className="btn btn-primary btn-sm">
            {t('marketing.imports.list.newImport')}
          </Link>
        </div>
      </div>

      <Card>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="p-3 text-muted">
              {t('marketing.imports.list.loading')}
            </div>
          ) : !data?.items?.length ? (
            <div className="p-3 text-muted">
              <Trans
                i18nKey="marketing.imports.list.empty"
                components={{ strong: <strong /> }}
              />
            </div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>{t('marketing.imports.list.colSource')}</th>
                  <th>{t('marketing.imports.list.colAdapter')}</th>
                  <th>{t('marketing.imports.list.colStatus')}</th>
                  <th>{t('marketing.imports.list.colRows')}</th>
                  <th>{t('marketing.imports.list.colCreated')}</th>
                  <th>{t('marketing.imports.list.colCreatedMerged')}</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map(j => (
                  <tr key={j.uuid}>
                    <td className="fw-medium">
                      {j.sourceName || (
                        <span className="text-muted">
                          {t('marketing.imports.list.unnamed')}
                        </span>
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
                          {t('marketing.imports.list.rowsFailedSuffix', {
                            count: j.stats.rowsFailed
                          })}
                        </span>
                      ) : null}
                      {j.stats.conflictsSkipped ? (
                        <span className="text-warning fs-10">
                          {t('marketing.imports.list.conflictsSuffix', {
                            count: j.stats.conflictsSkipped
                          })}
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
                        {t('marketing.imports.list.orgsLine', {
                          created: j.stats.orgsCreated ?? 0,
                          merged: j.stats.orgsMerged ?? 0
                        })}
                      </small>
                      <br />
                      <small>
                        {t('marketing.imports.list.personsLine', {
                          created: j.stats.personsCreated ?? 0,
                          merged: j.stats.personsMerged ?? 0
                        })}
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

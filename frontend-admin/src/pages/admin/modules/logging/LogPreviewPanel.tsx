import { useMemo, useState } from 'react';
import { Alert, Button, Col, Form, Row, Spinner } from 'react-bootstrap';
import {
  faArrowUpRightFromSquare,
  faFileLines
} from '@fortawesome/free-solid-svg-icons';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import type { ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import SectionCard from 'components/common/SectionCard';
import SubtleBadge, { type BadgeColor } from 'components/common/SubtleBadge';
import { formatDateTime } from 'helpers/dateFormat';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import { useGetLogPreviewQuery } from 'store/api/observabilityApi';
import {
  LOG_LEVELS,
  type LogEvent,
  type LogLevel,
  type LogLevelsView,
  type LogPreviewWindowMinutes
} from 'types/observability';

interface LogPreviewPanelProps {
  snapshot: LogLevelsView;
}

const levelVariant: Record<LogLevel, BadgeColor> = {
  debug: 'secondary',
  info: 'primary',
  warn: 'warning',
  error: 'danger'
};

const displayAttribute = (value: unknown): string =>
  typeof value === 'string' ? value : JSON.stringify(value);

const LogPreviewPanel = ({ snapshot }: LogPreviewPanelProps) => {
  const { t } = useTranslation();
  const [moduleName, setModuleName] = useState(snapshot.modules[0]?.name ?? '');
  const [windowMinutes, setWindowMinutes] =
    useState<LogPreviewWindowMinutes>(15);
  const [level, setLevel] = useState<LogLevel | ''>('');
  const [search, setSearch] = useState('');
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());

  const preview = useGetLogPreviewQuery(
    snapshot.logProvider.available
      ? {
          module: moduleName,
          windowMinutes,
          level: level || undefined,
          q: search.trim() || undefined,
          limit: 100
        }
      : undefined,
    {
      // RTK Query owns the polling lifecycle. Zero keeps auto-refresh off by
      // default; enabling the checkbox creates one five-second subscription
      // and unmounting the panel tears it down without a component timer.
      pollingInterval: autoRefresh ? 5000 : 0
    }
  );

  const toggleAttributes = (rowKey: string) => {
    setExpandedRows(current => {
      const next = new Set(current);
      if (next.has(rowKey)) next.delete(rowKey);
      else next.add(rowKey);
      return next;
    });
  };

  const columns = useMemo<ColumnDef<LogEvent>[]>(
    () => [
      {
        accessorKey: 'timestamp',
        header: t('adminObservability.loggingWorkspace.logs.columns.timestamp'),
        meta: {
          headerProps: { className: 'text-900 text-nowrap' },
          cellProps: { className: 'text-nowrap' }
        },
        cell: ({ row: { original } }) => formatDateTime(original.timestamp)
      },
      {
        accessorKey: 'level',
        header: t('adminObservability.loggingWorkspace.logs.columns.level'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({ row: { original } }) => (
          <SubtleBadge bg={levelVariant[original.level]}>
            {t(
              `adminObservability.loggingWorkspace.levelNames.${original.level}`
            )}
          </SubtleBadge>
        )
      },
      {
        accessorKey: 'module',
        header: t('adminObservability.loggingWorkspace.logs.columns.module'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({ row: { original } }) => <code>{original.module}</code>
      },
      {
        accessorKey: 'message',
        header: t('adminObservability.loggingWorkspace.logs.columns.message'),
        meta: {
          headerProps: { className: 'text-900' },
          cellProps: { className: 'text-wrap' }
        }
      },
      {
        id: 'attributes',
        header: t(
          'adminObservability.loggingWorkspace.logs.columns.attributes'
        ),
        enableSorting: false,
        meta: { headerProps: { className: 'text-900 text-nowrap' } },
        cell: ({ row: { original } }) => {
          const rowKey = `${original.timestamp}-${original.module}`;
          const expanded = expandedRows.has(rowKey);
          const attributes = Object.entries(original.attributes);
          if (attributes.length === 0) {
            return (
              <span className="text-muted">
                {t('adminObservability.loggingWorkspace.logs.noAttributes')}
              </span>
            );
          }
          return (
            <div>
              <Button
                variant="link"
                size="sm"
                className="p-0"
                aria-expanded={expanded}
                aria-label={t(
                  expanded
                    ? 'adminObservability.loggingWorkspace.logs.hideAttributesAria'
                    : 'adminObservability.loggingWorkspace.logs.showAttributesAria',
                  {
                    module: original.module,
                    date: formatDateTime(original.timestamp)
                  }
                )}
                onClick={() => toggleAttributes(rowKey)}
              >
                {t(
                  expanded
                    ? 'adminObservability.loggingWorkspace.logs.hideAttributes'
                    : 'adminObservability.loggingWorkspace.logs.showAttributes'
                )}
              </Button>
              {expanded && (
                <dl className="mb-0 mt-2">
                  {attributes.map(([name, value]) => (
                    <div key={name} className="d-flex gap-2">
                      <dt className="fw-semibold">{name}</dt>
                      <dd className="mb-0 text-break">
                        {displayAttribute(value)}
                      </dd>
                    </div>
                  ))}
                </dl>
              )}
            </div>
          );
        }
      }
    ],
    [expandedRows, t]
  );

  const table = useAdvanceTable({
    data: preview.data?.events ?? [],
    columns,
    sortable: true,
    pagination: false
  });

  const headerActions = snapshot.logProvider.grafanaUrl ? (
    <a
      href={snapshot.logProvider.grafanaUrl}
      target="_blank"
      rel="noreferrer"
      className="btn btn-orkestra-primary btn-sm"
    >
      <FontAwesomeIcon icon={faArrowUpRightFromSquare} className="me-2" />
      {t('adminObservability.loggingWorkspace.logs.openGrafana')}
    </a>
  ) : undefined;

  return (
    <SectionCard
      icon={faFileLines}
      title={t('adminObservability.loggingWorkspace.logs.title')}
      headerEnd={headerActions}
    >
      <p className="text-muted fs-10">
        {t('adminObservability.loggingWorkspace.logs.description')}
      </p>
      <Alert variant="warning" className="fs-10 py-2">
        {t('adminObservability.loggingWorkspace.logs.privacyNotice')}
      </Alert>
      {!snapshot.logProvider.available ? (
        <Alert variant="secondary" className="fs-10 mb-0">
          {t('adminObservability.loggingWorkspace.logs.providerUnavailable')}
        </Alert>
      ) : (
        <>
          <Row className="g-3 align-items-end mb-3">
            <Col md={4} xl={3}>
              <Form.Group controlId="logging-preview-module">
                <Form.Label>
                  {t('adminObservability.loggingWorkspace.logs.moduleLabel')}
                </Form.Label>
                <Form.Select
                  value={moduleName}
                  aria-label={t(
                    'adminObservability.loggingWorkspace.logs.moduleAria'
                  )}
                  onChange={event => setModuleName(event.target.value)}
                >
                  {snapshot.modules.map(module => (
                    <option key={module.name} value={module.name}>
                      {module.name}
                    </option>
                  ))}
                </Form.Select>
              </Form.Group>
            </Col>
            <Col md={4} xl={2}>
              <Form.Group controlId="logging-preview-window">
                <Form.Label>
                  {t('adminObservability.loggingWorkspace.logs.windowLabel')}
                </Form.Label>
                <Form.Select
                  value={windowMinutes}
                  aria-label={t(
                    'adminObservability.loggingWorkspace.logs.windowAria'
                  )}
                  onChange={event =>
                    setWindowMinutes(
                      Number(event.target.value) as LogPreviewWindowMinutes
                    )
                  }
                >
                  {[5, 15, 60].map(minutes => (
                    <option key={minutes} value={minutes}>
                      {t(
                        'adminObservability.loggingWorkspace.logs.windowMinutes',
                        { count: minutes }
                      )}
                    </option>
                  ))}
                </Form.Select>
              </Form.Group>
            </Col>
            <Col md={4} xl={2}>
              <Form.Group controlId="logging-preview-level">
                <Form.Label>
                  {t('adminObservability.loggingWorkspace.logs.levelLabel')}
                </Form.Label>
                <Form.Select
                  value={level}
                  aria-label={t(
                    'adminObservability.loggingWorkspace.logs.levelAria'
                  )}
                  onChange={event =>
                    setLevel(event.target.value as LogLevel | '')
                  }
                >
                  <option value="">
                    {t('adminObservability.loggingWorkspace.logs.allLevels')}
                  </option>
                  {LOG_LEVELS.map(candidate => (
                    <option key={candidate} value={candidate}>
                      {t(
                        `adminObservability.loggingWorkspace.levelNames.${candidate}`
                      )}
                    </option>
                  ))}
                </Form.Select>
              </Form.Group>
            </Col>
            <Col md={8} xl={3}>
              <Form.Group controlId="logging-preview-search">
                <Form.Label>
                  {t('adminObservability.loggingWorkspace.logs.searchLabel')}
                </Form.Label>
                <Form.Control
                  value={search}
                  maxLength={200}
                  aria-label={t(
                    'adminObservability.loggingWorkspace.logs.searchAria'
                  )}
                  placeholder={t(
                    'adminObservability.loggingWorkspace.logs.searchPlaceholder'
                  )}
                  onChange={event => setSearch(event.target.value)}
                />
              </Form.Group>
            </Col>
            <Col md={4} xl={2}>
              <Button
                variant="orkestra-primary"
                className="w-100"
                disabled={preview.isFetching}
                aria-label={t(
                  'adminObservability.loggingWorkspace.logs.refreshAria'
                )}
                onClick={() => preview.refetch()}
              >
                {preview.isFetching && (
                  <Spinner animation="border" size="sm" className="me-2" />
                )}
                {t('adminObservability.loggingWorkspace.logs.refresh')}
              </Button>
            </Col>
          </Row>

          <div className="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-3">
            <Form.Check
              type="switch"
              id="logging-preview-auto-refresh"
              checked={autoRefresh}
              label={t('adminObservability.loggingWorkspace.logs.autoRefresh')}
              aria-label={t(
                'adminObservability.loggingWorkspace.logs.autoRefreshAria'
              )}
              onChange={event => setAutoRefresh(event.target.checked)}
            />
            <span className="text-muted fs-10">
              {t('adminObservability.loggingWorkspace.logs.limitHint')}
            </span>
          </div>

          {preview.isLoading ? (
            <div className="text-center py-4">
              <Spinner animation="border" size="sm" className="me-2" />
              <span className="fs-10">
                {t('adminObservability.loggingWorkspace.logs.loading')}
              </span>
            </div>
          ) : preview.error ? (
            <Alert variant="danger" className="fs-10 mb-0">
              {t('adminObservability.loggingWorkspace.logs.loadFailed')}
            </Alert>
          ) : (preview.data?.events.length ?? 0) === 0 ? (
            <p
              className="text-muted fs-10 mb-0"
              role="status"
              aria-live="polite"
              aria-label={t(
                'adminObservability.loggingWorkspace.logs.statusAria'
              )}
            >
              {t('adminObservability.loggingWorkspace.logs.empty')}
            </p>
          ) : (
            <>
              <div
                role="status"
                aria-live="polite"
                aria-label={t(
                  'adminObservability.loggingWorkspace.logs.statusAria'
                )}
                className="visually-hidden"
              >
                {t('adminObservability.loggingWorkspace.logs.resultsCount', {
                  count: preview.data?.events.length ?? 0
                })}
              </div>
              <AdvanceTableProvider {...table}>
                <AdvanceTable
                  headerClassName="text-nowrap align-middle"
                  rowClassName="align-middle"
                  tableProps={{
                    striped: true,
                    className: 'fs-10 mb-0 overflow-hidden'
                  }}
                />
              </AdvanceTableProvider>
            </>
          )}
        </>
      )}
    </SectionCard>
  );
};

export default LogPreviewPanel;

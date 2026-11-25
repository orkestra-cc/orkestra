import { useMemo } from 'react';
import { Col, Row, Spinner } from 'react-bootstrap';
import {
  faLayerGroup,
  faServer,
  faUserSlash
} from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import EmptyState from 'components/common/EmptyState';
import StatCard from 'components/common/StatCard';
import SubtleBadge, { type BadgeColor } from 'components/common/SubtleBadge';
import SearchablePagedTable from 'components/common/advance-table/SearchablePagedTable';
import {
  useGetRetrievalTenantIndexingQuery,
  type RetrievalTenantCorpus
} from 'store/api/retrievalApi';
import RetrievalLoadError from './RetrievalLoadError';
import {
  BacklogCell,
  CORPUS_STATE_COLOR,
  OwnerCell,
  engineAccent
} from './RetrievalCells';

const BUILD_COLOR: Record<RetrievalTenantCorpus['initialBuild'], BadgeColor> = {
  pending: 'secondary',
  building: 'info',
  done: 'success'
};

// IndexingTab is the active organization's view of retrieval
// (retrieval.tenant.read): for each registered corpus, its state, whether
// its initial build for this organization is done, how many points it holds, and the
// organization's own pending updates, plus its erasures still pending.
const IndexingTab = () => {
  const { t, i18n } = useTranslation();
  const { data, isLoading, isError, refetch } =
    useGetRetrievalTenantIndexingQuery();

  const columns = useMemo<ColumnDef<RetrievalTenantCorpus>[]>(
    () => [
      {
        accessorKey: 'corpus',
        header: t('adminRetrieval.indexing.columns.corpus'),
        meta: {
          headerProps: { className: 'text-900' },
          cellProps: { className: 'fw-semibold text-900' }
        }
      },
      {
        accessorKey: 'owner',
        header: t('adminRetrieval.indexing.columns.owner'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalTenantCorpus, unknown>) => (
          <OwnerCell owner={original.owner} running={original.ownerRunning} />
        )
      },
      {
        // A broken corpus reads as broken here, not as a build that is
        // merely pending; the reason stays on the operator's Corpora tab.
        id: 'state',
        accessorFn: c => t(`adminRetrieval.corpora.states.${c.state}`),
        header: t('adminRetrieval.indexing.columns.state'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalTenantCorpus, unknown>) => (
          <SubtleBadge pill bg={CORPUS_STATE_COLOR[original.state] ?? 'danger'}>
            {t(`adminRetrieval.corpora.states.${original.state}`, {
              defaultValue: original.state
            })}
          </SubtleBadge>
        )
      },
      {
        id: 'initialBuild',
        accessorFn: c =>
          t(`adminRetrieval.indexing.initialBuild.${c.initialBuild}`),
        header: t('adminRetrieval.indexing.columns.initialBuild'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalTenantCorpus, unknown>) => (
          <SubtleBadge
            pill
            bg={BUILD_COLOR[original.initialBuild] ?? 'secondary'}
          >
            {t(`adminRetrieval.indexing.initialBuild.${original.initialBuild}`)}
          </SubtleBadge>
        )
      },
      {
        id: 'points',
        accessorFn: c => c.points ?? -1,
        header: t('adminRetrieval.indexing.columns.points'),
        enableGlobalFilter: false,
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalTenantCorpus, unknown>) =>
          original.points == null ? (
            <span className="text-600">—</span>
          ) : (
            original.points.toLocaleString(i18n.language)
          )
      },
      {
        id: 'outbox',
        accessorFn: c => c.outbox.pending,
        header: t('adminRetrieval.indexing.columns.outbox'),
        enableGlobalFilter: false,
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalTenantCorpus, unknown>) => (
          <BacklogCell backlog={original.outbox} />
        )
      }
    ],
    [t, i18n.language]
  );

  if (isLoading) return <Spinner animation="border" size="sm" />;
  if (isError || !data) return <RetrievalLoadError onRetry={() => refetch()} />;

  return (
    <>
      <Row className="g-3 mb-3">
        <Col md={6} xl={3}>
          <StatCard
            title={t('adminRetrieval.engine.title')}
            value={t(`adminRetrieval.engine.${data.engine.status}`)}
            icon={faServer}
            color="primary"
            accent={engineAccent(data.engine.status)}
          />
        </Col>
        <Col md={6} xl={3}>
          <StatCard
            title={t('adminRetrieval.indexing.erasuresPending')}
            value={data.erasuresPending.toLocaleString(i18n.language)}
            icon={faUserSlash}
            color="warning"
            accent={data.erasuresPending > 0 ? 'warning' : undefined}
          />
        </Col>
      </Row>
      {data.corpora.length === 0 ? (
        <EmptyState
          icon={faLayerGroup}
          message={t('adminRetrieval.indexing.empty')}
        />
      ) : (
        <SearchablePagedTable
          data={data.corpora}
          columns={columns}
          searchPlaceholder={t('adminRetrieval.indexing.search')}
        />
      )}
    </>
  );
};

export default IndexingTab;

import { useMemo, useState } from 'react';
import { Alert, Col, Row, Spinner, Table } from 'react-bootstrap';
import {
  faDatabase,
  faMicrochip,
  faServer,
  faStethoscope,
  faTrash,
  faUserSlash,
  faHourglassHalf
} from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import EmptyState from 'components/common/EmptyState';
import IconButton from 'components/common/IconButton';
import SectionCard from 'components/common/SectionCard';
import StatCard from 'components/common/StatCard';
import SubtleBadge, { type BadgeColor } from 'components/common/SubtleBadge';
import SearchablePagedTable from 'components/common/advance-table/SearchablePagedTable';
import {
  useDropRetrievalCorpusMutation,
  useGetRetrievalCorporaQuery,
  type RetrievalComponentStatus,
  type RetrievalCorpusStatus
} from 'store/api/retrievalApi';
import ConfirmDeleteModal from 'pages/admin/llm/ConfirmDeleteModal';
import DiagnosticsModal from './DiagnosticsModal';
import RetrievalLoadError from './RetrievalLoadError';
import {
  BacklogCell,
  CORPUS_STATE_COLOR,
  OwnerCell,
  engineAccent
} from './RetrievalCells';
import { isReauthCancelled, retrievalErrorMessage } from './retrievalErrors';
import { useRetrievalPermissions } from './retrievalPermissions';

const INFERENCE_COLOR: Record<RetrievalComponentStatus, BadgeColor> = {
  ok: 'success',
  not_configured: 'secondary',
  unavailable: 'danger',
  mismatch: 'warning',
  wrong_kind: 'danger'
};

// The generations an orphan drop deletes: 1..max(1, active), the same range
// the backend walks (a failed bootstrap may have created g1 before writing
// any state).
const dropGenerations = (c: RetrievalCorpusStatus) =>
  Array.from({ length: Math.max(1, c.activeGeneration) }, (_, i) => i + 1);

// CorporaTab is the installation-wide corpus report (retrieval.corpus.read):
// engine and inference health, erasure backlog, and one row per corpus,
// registered or orphaned. With retrieval.corpus.admin it adds a diagnostic
// search on registered corpora (an error corpus still serves its active
// generation, so it stays diagnosable) and a drop on orphans — corpora no
// module registers any more. Both are step-up-gated and audited on the
// backend; a dismissed step-up says nothing more.
const CorporaTab = () => {
  const { t, i18n } = useTranslation();
  const { canAdminCorpora } = useRetrievalPermissions();
  const { data, isLoading, isError, refetch } = useGetRetrievalCorporaQuery();
  const [drop, { isLoading: dropping }] = useDropRetrievalCorpusMutation();
  const [diagnose, setDiagnose] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<{
    open: boolean;
    corpus?: RetrievalCorpusStatus;
    error?: string | null;
  }>({ open: false });

  const onConfirmDrop = async () => {
    const c = confirm.corpus;
    if (!c) return;
    setConfirm(prev => ({ ...prev, error: null }));
    try {
      await drop({ corpus: c.name }).unwrap();
      toast.success(t('adminRetrieval.corpora.dropSuccess', { name: c.name }));
      setConfirm(prev => ({ ...prev, open: false }));
    } catch (e) {
      if (isReauthCancelled(e)) return;
      setConfirm(prev => ({ ...prev, error: retrievalErrorMessage(t, e) }));
    }
  };

  // Memoised: AdvanceTable renders each `cell` as a component, so a fresh
  // columns array per render would remount every row's actions (and drop
  // the focus a modal hands back to its trigger).
  const columns = useMemo<ColumnDef<RetrievalCorpusStatus>[]>(() => {
    const cols: ColumnDef<RetrievalCorpusStatus>[] = [
      {
        accessorKey: 'name',
        header: t('adminRetrieval.corpora.columns.name'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalCorpusStatus, unknown>) => (
          <span className="d-inline-flex align-items-center gap-2">
            <span className="fw-semibold text-900">{original.name}</span>
            {!original.registered && (
              <SubtleBadge pill bg="warning">
                {t('adminRetrieval.corpora.notRegistered')}
              </SubtleBadge>
            )}
          </span>
        )
      },
      {
        id: 'owner',
        accessorFn: c => c.owner ?? '',
        header: t('adminRetrieval.corpora.columns.owner'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalCorpusStatus, unknown>) => (
          <OwnerCell owner={original.owner} running={original.ownerRunning} />
        )
      },
      {
        id: 'state',
        // Translated accessor so the search box matches what is on screen.
        accessorFn: c => t(`adminRetrieval.corpora.states.${c.state}`),
        header: t('adminRetrieval.corpora.columns.state'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalCorpusStatus, unknown>) => (
          <div>
            <SubtleBadge
              pill
              bg={CORPUS_STATE_COLOR[original.state] ?? 'danger'}
            >
              {t(`adminRetrieval.corpora.states.${original.state}`)}
            </SubtleBadge>
            {original.reason && (
              <div className="fs-11 text-danger mt-1">
                {t(`adminRetrieval.corpora.reasons.${original.reason}`)}
              </div>
            )}
          </div>
        )
      },
      {
        accessorKey: 'activeGeneration',
        header: t('adminRetrieval.corpora.columns.generation'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalCorpusStatus, unknown>) =>
          original.activeGeneration > 0 ? (
            original.activeGeneration
          ) : (
            <span className="text-600">—</span>
          )
      },
      {
        id: 'outbox',
        accessorFn: c => c.outbox.pending,
        header: t('adminRetrieval.corpora.columns.outbox'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalCorpusStatus, unknown>) => (
          <BacklogCell backlog={original.outbox} />
        )
      },
      {
        id: 'tenants',
        accessorFn: c => c.tenantsDone,
        header: t('adminRetrieval.corpora.columns.tenants'),
        enableGlobalFilter: false,
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalCorpusStatus, unknown>) =>
          t('adminRetrieval.corpora.tenants', {
            done: original.tenantsDone.toLocaleString(i18n.language),
            building: original.tenantsBuilding.toLocaleString(i18n.language)
          })
      }
    ];
    if (!canAdminCorpora) return cols;
    cols.push({
      id: 'actions',
      header: t('adminRetrieval.corpora.columns.actions'),
      enableSorting: false,
      enableGlobalFilter: false,
      meta: {
        headerProps: { className: 'text-end text-900' },
        cellProps: { className: 'text-end' }
      },
      cell: ({
        row: { original }
      }: CellContext<RetrievalCorpusStatus, unknown>) =>
        original.registered ? (
          <IconButton
            variant="orkestra-default"
            size="sm"
            icon={faStethoscope}
            title={t('adminRetrieval.actions.diagnose')}
            aria-label={t('adminRetrieval.actions.diagnoseAria', {
              name: original.name
            })}
            onClick={() => setDiagnose(original.name)}
          />
        ) : (
          <IconButton
            variant="orkestra-danger"
            size="sm"
            icon={faTrash}
            title={t('adminRetrieval.actions.drop')}
            aria-label={t('adminRetrieval.actions.dropAria', {
              name: original.name
            })}
            onClick={() =>
              setConfirm({ open: true, corpus: original, error: null })
            }
          />
        )
    });
    return cols;
  }, [t, i18n.language, canAdminCorpora]);

  if (isLoading) return <Spinner animation="border" size="sm" />;
  if (isError || !data) return <RetrievalLoadError onRetry={() => refetch()} />;

  const { engine, inference, corpora, erasures } = data;
  const healthy = inference.filter(i => i.status === 'ok').length;
  const num = (n: number) => n.toLocaleString(i18n.language);

  return (
    <>
      <Row className="g-3 mb-3">
        <Col md={6} xl={3}>
          <StatCard
            title={t('adminRetrieval.engine.title')}
            value={t(`adminRetrieval.engine.${engine.status}`)}
            icon={faServer}
            color="primary"
            accent={engineAccent(engine.status)}
          />
        </Col>
        <Col md={6} xl={3}>
          <StatCard
            title={t('adminRetrieval.inference.title')}
            value={
              inference.length > 0 ? `${healthy}/${inference.length}` : '—'
            }
            subtitle={
              inference.length > 0
                ? t('adminRetrieval.inference.healthy', {
                    ok: healthy,
                    total: inference.length
                  })
                : t('adminRetrieval.inference.none')
            }
            icon={faMicrochip}
            color="info"
            accent={healthy < inference.length ? 'warning' : undefined}
          />
        </Col>
        <Col md={6} xl={3}>
          <StatCard
            title={t('adminRetrieval.erasures.pending')}
            value={num(erasures.pending)}
            icon={faUserSlash}
            color="warning"
            accent={erasures.pending > 0 ? 'warning' : undefined}
          />
        </Col>
        <Col md={6} xl={3}>
          <StatCard
            title={t('adminRetrieval.erasures.fencing')}
            value={num(erasures.fencing)}
            icon={faHourglassHalf}
            color="secondary"
          />
        </Col>
      </Row>

      {inference.length > 0 && (
        <div className="mb-3">
          <SectionCard
            icon={faMicrochip}
            title={t('adminRetrieval.inference.title')}
          >
            <Table size="sm" responsive className="fs-10 mb-0">
              <thead>
                <tr>
                  <th scope="col">
                    {t('adminRetrieval.inference.columns.endpoint')}
                  </th>
                  <th scope="col">
                    {t('adminRetrieval.inference.columns.kind')}
                  </th>
                  <th scope="col">
                    {t('adminRetrieval.inference.columns.status')}
                  </th>
                  <th scope="col">
                    {t('adminRetrieval.inference.columns.profile')}
                  </th>
                  <th scope="col">
                    {t('adminRetrieval.inference.columns.boundary')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {inference.map(row => (
                  <tr key={`${row.endpoint}:${row.kind}`}>
                    <td className="font-monospace">{row.endpoint}</td>
                    <td>{t(`adminRetrieval.inference.kinds.${row.kind}`)}</td>
                    <td>
                      <SubtleBadge
                        pill
                        bg={INFERENCE_COLOR[row.status] ?? 'danger'}
                      >
                        {t(`adminRetrieval.inference.status.${row.status}`)}
                      </SubtleBadge>
                    </td>
                    <td className="font-monospace">{row.profile ?? '—'}</td>
                    <td>
                      {row.boundary
                        ? t(
                            `adminRetrieval.inference.boundaries.${row.boundary}`
                          )
                        : '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </SectionCard>
        </div>
      )}

      {engine.status === 'not_configured' && (
        <Alert variant="info" className="fs-10">
          {t('adminRetrieval.corpora.notConfigured')}
        </Alert>
      )}

      {corpora.length === 0 ? (
        <EmptyState
          icon={faDatabase}
          message={t('adminRetrieval.corpora.empty')}
        />
      ) : (
        <SearchablePagedTable
          data={corpora}
          columns={columns}
          searchPlaceholder={t('adminRetrieval.corpora.search')}
        />
      )}

      {diagnose && (
        <DiagnosticsModal corpus={diagnose} onClose={() => setDiagnose(null)} />
      )}
      <ConfirmDeleteModal
        show={confirm.open}
        title={t('adminRetrieval.corpora.dropTitle')}
        body={t('adminRetrieval.corpora.dropConfirm', {
          name: confirm.corpus?.name ?? '',
          generations: confirm.corpus
            ? dropGenerations(confirm.corpus).join(', ')
            : ''
        })}
        error={confirm.error}
        busy={dropping}
        confirmLabel={t('adminRetrieval.actions.drop')}
        onCancel={() => setConfirm(prev => ({ ...prev, open: false }))}
        onConfirm={onConfirmDrop}
      />
    </>
  );
};

export default CorporaTab;

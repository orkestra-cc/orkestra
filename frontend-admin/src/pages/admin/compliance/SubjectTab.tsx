import { useEffect, useState, type FormEvent } from 'react';
import { Alert, Button, Card, Col, Form, Row, Spinner } from 'react-bootstrap';
import { faFileExport, faUserSlash } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import IconButton from 'components/common/IconButton';
import SubtleBadge from 'components/common/SubtleBadge';
import EmptyState from 'components/common/EmptyState';
import SearchablePagedTable from 'components/common/advance-table/SearchablePagedTable';
import {
  useCreateDsrExportMutation,
  useDownloadDsrExportMutation,
  useDsrPreviewQuery,
  useListDsrExportsQuery,
  useOpenErasureRequestMutation,
  type DSRExportFormat,
  type DSRExportRecord,
  type DSRPreviewProducer
} from 'store/api/complianceApi';
import { formatDate, formatDateTime } from './complianceFormat';
import { erasureErrorMessage } from './erasureErrors';
import OpenErasureRequestModal from './OpenErasureRequestModal';
import ReasonModal from './policies/ReasonModal';

// SubjectTab is the per-subject DSR workbench: what an erasure would do (dry
// run), an export on behalf of the subject, and opening an erasure request.
// The subject is carried in `?userUuid=` so a view can be shared and the user
// page can deep-link here. Producer row counts are per producer and can
// overlap (auth instances share rows), so they are never summed.
const SubjectTab = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const userUuid = (searchParams.get('userUuid') ?? '').trim();
  const [draft, setDraft] = useState(userUuid);
  useEffect(() => setDraft(userUuid), [userUuid]);

  const preview = useDsrPreviewQuery(userUuid, { skip: !userUuid });
  const exportsQ = useListDsrExportsQuery(userUuid, { skip: !userUuid });
  const [createExport, { isLoading: exporting }] = useCreateDsrExportMutation();
  const [download] = useDownloadDsrExportMutation();
  const [openRequest, { isLoading: opening }] = useOpenErasureRequestMutation();
  const [exportFormat, setExportFormat] = useState<DSRExportFormat | null>(
    null
  );
  const [showOpen, setShowOpen] = useState(false);

  const onLoad = (e: FormEvent) => {
    e.preventDefault();
    const v = draft.trim();
    setSearchParams(
      prev => {
        if (v) prev.set('userUuid', v);
        else prev.delete('userUuid');
        return prev;
      },
      { replace: true }
    );
  };

  const onExport = async (reason: string) => {
    if (!exportFormat) return;
    try {
      await createExport({ userUuid, format: exportFormat, reason }).unwrap();
      toast.success(t('adminCompliance.subject.exportSuccess'));
      setExportFormat(null);
    } catch (err) {
      toast.error(
        erasureErrorMessage(err, t, t('adminCompliance.subject.exportError'))
      );
    }
  };

  // The signed URL is opened and dropped: never stored, never logged.
  const onDownload = async (id: string) => {
    try {
      const res = await download(id).unwrap();
      window.open(res.url, '_blank', 'noopener');
    } catch (err) {
      toast.error(
        erasureErrorMessage(err, t, t('adminCompliance.subject.downloadError'))
      );
    }
  };

  const onOpenRequest = async (v: {
    userUuid: string;
    mode: string;
    reason: string;
  }) => {
    try {
      await openRequest(v).unwrap();
      toast.success(t('adminCompliance.erasure.open.success'));
      setShowOpen(false);
    } catch (err) {
      toast.error(
        erasureErrorMessage(err, t, t('adminCompliance.erasure.open.error'))
      );
    }
  };

  const producerColumns: ColumnDef<DSRPreviewProducer>[] = [
    {
      accessorKey: 'key',
      header: t('adminCompliance.subject.producers.key'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      id: 'rows',
      accessorFn: p => (p.countable ? p.rows : -1),
      header: t('adminCompliance.subject.producers.rows'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({
        row: { original }
      }: CellContext<DSRPreviewProducer, unknown>) =>
        original.countable ? (
          original.rows
        ) : (
          <SubtleBadge pill bg="secondary">
            {t('adminCompliance.subject.producers.notCountable')}
          </SubtleBadge>
        )
    },
    {
      id: 'collections',
      header: t('adminCompliance.subject.producers.collections'),
      enableSorting: false,
      meta: { headerProps: { className: 'text-900' } },
      cell: ({
        row: { original }
      }: CellContext<DSRPreviewProducer, unknown>) => (
        <>
          <span className="font-monospace small">
            {(original.collections ?? []).join(', ') || '—'}
          </span>
          {original.error && (
            <SubtleBadge pill bg="danger" className="ms-2">
              {original.error}
            </SubtleBadge>
          )}
        </>
      )
    }
  ];

  const exportColumns: ColumnDef<DSRExportRecord>[] = [
    {
      accessorKey: 'format',
      header: t('adminCompliance.subject.exports.format'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<DSRExportRecord, unknown>) =>
        original.format.toUpperCase()
    },
    {
      accessorKey: 'reason',
      header: t('adminCompliance.subject.exports.reason'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      id: 'createdAt',
      accessorFn: e => e.createdAt,
      header: t('adminCompliance.subject.exports.created'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<DSRExportRecord, unknown>) =>
        formatDateTime(original.createdAt)
    },
    {
      id: 'expiresAt',
      accessorFn: e => e.expiresAt,
      header: t('adminCompliance.subject.exports.expires'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<DSRExportRecord, unknown>) =>
        Date.parse(original.expiresAt) < Date.now() ? (
          <SubtleBadge pill bg="secondary">
            {t('adminCompliance.subject.exports.expired')}
          </SubtleBadge>
        ) : (
          formatDateTime(original.expiresAt)
        )
    },
    {
      id: 'actions',
      header: t('adminCompliance.holds.columns.actions'),
      enableSorting: false,
      meta: {
        headerProps: { className: 'text-end text-900' },
        cellProps: { className: 'text-end' }
      },
      cell: ({ row: { original } }: CellContext<DSRExportRecord, unknown>) => (
        <IconButton
          size="sm"
          variant="outline-secondary"
          icon="download"
          onClick={() => onDownload(original.uuid)}
        >
          {t('adminCompliance.subject.exports.download')}
        </IconButton>
      )
    }
  ];

  const data = preview.data;
  const holds = data?.legalHolds ?? [];
  const producers = data?.producers ?? [];
  const exportRows = exportsQ.data?.items ?? [];

  return (
    <>
      <Form onSubmit={onLoad} className="mb-3">
        <Row className="g-2 align-items-end">
          <Col md={6}>
            <Form.Label
              htmlFor="subject-user-uuid"
              className="fs-11 text-700 mb-1"
            >
              {t('adminCompliance.subject.userUuid')}
            </Form.Label>
            <Form.Control
              id="subject-user-uuid"
              size="sm"
              value={draft}
              placeholder={t('adminCompliance.holds.form.subjectPlaceholder')}
              onChange={e => setDraft(e.target.value)}
            />
          </Col>
          <Col xs="auto">
            <Button type="submit" size="sm" variant="orkestra-primary">
              {t('adminCompliance.subject.load')}
            </Button>
          </Col>
        </Row>
      </Form>

      {!userUuid ? (
        <EmptyState
          icon={faUserSlash}
          message={t('adminCompliance.subject.emptyMessage')}
          hint={t('adminCompliance.subject.emptyHint')}
        />
      ) : preview.isLoading ? (
        <Spinner animation="border" size="sm" />
      ) : preview.isError || !data ? (
        <Alert variant="danger">{t('adminCompliance.subject.loadError')}</Alert>
      ) : (
        <>
          <Card className="shadow-none border mb-3">
            <Card.Body>
              <h6>{t('adminCompliance.subject.previewTitle')}</h6>
              <div className="mb-2">
                <SubtleBadge
                  pill
                  bg={data.hardDeleteAllowed ? 'success' : 'danger'}
                  className="me-2"
                >
                  {data.hardDeleteAllowed
                    ? t('adminCompliance.subject.hardDeleteAllowed')
                    : t('adminCompliance.subject.hardDeleteBlocked')}
                </SubtleBadge>
                <SubtleBadge
                  pill
                  bg={data.anonymizeAllowed ? 'success' : 'danger'}
                >
                  {data.anonymizeAllowed
                    ? t('adminCompliance.subject.anonymizeAllowed')
                    : t('adminCompliance.subject.anonymizeBlocked')}
                </SubtleBadge>
              </div>
              <p className="fs-10 mb-1">
                <strong>{t('adminCompliance.subject.holds')}:</strong>{' '}
                {holds.length === 0
                  ? t('adminCompliance.subject.noHolds')
                  : holds.map(h => h.reason).join('; ')}
              </p>
              <p className="fs-10 mb-1">
                <strong>{t('adminCompliance.subject.retention')}:</strong>{' '}
                {data.retentionError
                  ? t('adminCompliance.subject.retentionUnavailable')
                  : data.retention
                    ? t('adminCompliance.subject.retainUntil', {
                        date: formatDate(data.retention.retainUntil),
                        reasons: (data.retention.reasons ?? [])
                          .map(r =>
                            t(`adminCompliance.erasure.retentionReason.${r}`, {
                              defaultValue: r
                            })
                          )
                          .join(', ')
                      })
                    : t('adminCompliance.subject.noRetention')}
              </p>
              <p className="fs-10 mb-3">
                <strong>{t('adminCompliance.subject.openRequests')}:</strong>{' '}
                {(data.openRequests ?? []).length}
              </p>

              <h6>{t('adminCompliance.subject.producers.title')}</h6>
              <p className="fs-11 text-muted">
                {t('adminCompliance.subject.producers.note')}
              </p>
              {producers.length === 0 ? (
                <p className="fs-10 text-muted">
                  {t('adminCompliance.subject.producers.empty')}
                </p>
              ) : (
                <SearchablePagedTable
                  data={producers}
                  columns={producerColumns}
                  searchPlaceholder={t(
                    'adminCompliance.subject.producers.search'
                  )}
                />
              )}
            </Card.Body>
          </Card>

          <div className="d-flex flex-wrap gap-2 mb-3">
            <IconButton
              size="sm"
              variant="outline-secondary"
              icon={faFileExport}
              onClick={() => setExportFormat('json')}
            >
              {t('adminCompliance.subject.exportJson')}
            </IconButton>
            <IconButton
              size="sm"
              variant="outline-secondary"
              icon={faFileExport}
              onClick={() => setExportFormat('csv')}
            >
              {t('adminCompliance.subject.exportCsv')}
            </IconButton>
            <IconButton
              size="sm"
              variant="outline-danger"
              icon={faUserSlash}
              onClick={() => setShowOpen(true)}
            >
              {t('adminCompliance.subject.openErasure')}
            </IconButton>
          </div>

          <h6>{t('adminCompliance.subject.exports.title')}</h6>
          {exportRows.length === 0 ? (
            <p className="fs-10 text-muted">
              {t('adminCompliance.subject.exports.empty')}
            </p>
          ) : (
            <SearchablePagedTable
              data={exportRows}
              columns={exportColumns}
              searchPlaceholder={t('adminCompliance.subject.exports.search')}
            />
          )}
        </>
      )}

      <ReasonModal
        show={!!exportFormat}
        title={t('adminCompliance.subject.exportModal.title', {
          format: (exportFormat ?? '').toUpperCase()
        })}
        body={t('adminCompliance.subject.exportModal.body')}
        confirmLabel={t('adminCompliance.subject.exportModal.submit')}
        busy={exporting}
        onCancel={() => setExportFormat(null)}
        onConfirm={onExport}
      />
      <OpenErasureRequestModal
        show={showOpen}
        busy={opening}
        defaultUserUuid={userUuid}
        onCancel={() => setShowOpen(false)}
        onSubmit={onOpenRequest}
      />
    </>
  );
};

export default SubjectTab;

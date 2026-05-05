import { useMemo, useState } from 'react';
import { Button, Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useListAuditEventsQuery } from 'store/api/complianceApi';
import type { AuditEvent, ListAuditEventsParams } from 'types/compliance';
import AuditEventsFilters from './AuditEventsFilters';
import AuditEventsTable from './AuditEventsTable';
import AuditEventDetailModal from './AuditEventDetailModal';

const DEFAULT_LIMIT = 50;

const AuditEventsPage: React.FC = () => {
  // Filter + pagination state. Filter changes always reset offset=0 (see
  // AuditEventsFilters.onApply); paging-only changes mutate offset alone.
  const [params, setParams] = useState<ListAuditEventsParams>({ limit: DEFAULT_LIMIT });
  const [selected, setSelected] = useState<AuditEvent | null>(null);
  const [showDetail, setShowDetail] = useState(false);

  const { data, isFetching, error } = useListAuditEventsQuery(params);

  const total = data?.total ?? 0;
  const limit = data?.limit ?? params.limit ?? DEFAULT_LIMIT;
  const offset = data?.offset ?? params.offset ?? 0;
  const showingFrom = total === 0 ? 0 : offset + 1;
  const showingTo = Math.min(offset + (data?.items.length ?? 0), total);
  const canPrev = offset > 0;
  const canNext = offset + limit < total;

  const activeFilterCount = useMemo(() => {
    let n = 0;
    if (params.actionPrefix) n += 1;
    if (params.action) n += 1;
    if (params.outcome) n += 1;
    if (params.tenantId) n += 1;
    if (params.actorUserId) n += 1;
    if (params.since) n += 1;
    if (params.until) n += 1;
    return n;
  }, [params]);

  const openDetail = (ev: AuditEvent) => {
    setSelected(ev);
    setShowDetail(true);
  };

  return (
    <>
      <Row className="g-3 mb-3">
        <Col>
          <Card className="shadow-none border">
            <Card.Body className="d-flex align-items-center justify-content-between gap-3 flex-wrap">
              <div>
                <h5 className="mb-1">
                  <FontAwesomeIcon icon="clipboard-list" className="me-2 text-primary" />
                  Audit Events
                </h5>
                <p className="fs-10 mb-0 text-body-secondary">
                  Append-only trail of security-relevant actions across every tenant.
                  Retention: 2 years (TTL-enforced on MongoDB).
                </p>
              </div>
              <div className="text-end">
                <div className="fs-10 text-body-tertiary">Total matches</div>
                <div className="fs-5">{total.toLocaleString()}</div>
                {activeFilterCount > 0 && (
                  <div className="fs-11 text-body-tertiary">
                    {activeFilterCount} filter{activeFilterCount === 1 ? '' : 's'} active
                  </div>
                )}
              </div>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      <Card className="mb-3 shadow-none border">
        <Card.Body>
          <AuditEventsFilters
            value={params}
            onApply={(next) => setParams(next)}
            onReset={() => setParams({ limit: DEFAULT_LIMIT })}
          />
        </Card.Body>
      </Card>

      <AuditEventsTable
        events={data?.items ?? []}
        isLoading={isFetching}
        error={!!error}
        onRowClick={openDetail}
      />

      <div className="d-flex justify-content-between align-items-center mt-2 fs-10">
        <span className="text-body-tertiary">
          {total === 0
            ? 'No events'
            : `Showing ${showingFrom.toLocaleString()}–${showingTo.toLocaleString()} of ${total.toLocaleString()}`}
        </span>
        <div className="d-flex gap-2">
          <Button
            size="sm"
            variant="outline-secondary"
            disabled={!canPrev || isFetching}
            onClick={() =>
              setParams((prev) => ({
                ...prev,
                offset: Math.max(0, (prev.offset ?? 0) - (prev.limit ?? DEFAULT_LIMIT)),
              }))
            }
          >
            Previous
          </Button>
          <Button
            size="sm"
            variant="outline-secondary"
            disabled={!canNext || isFetching}
            onClick={() =>
              setParams((prev) => ({
                ...prev,
                offset: (prev.offset ?? 0) + (prev.limit ?? DEFAULT_LIMIT),
              }))
            }
          >
            Next
          </Button>
        </div>
      </div>

      <AuditEventDetailModal
        event={selected}
        show={showDetail}
        onHide={() => setShowDetail(false)}
      />
    </>
  );
};

export default AuditEventsPage;

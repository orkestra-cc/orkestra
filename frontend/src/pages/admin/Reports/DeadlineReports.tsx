import { useState } from 'react';
import { Card, Spinner, Alert } from 'react-bootstrap';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import DeadlineReportsHeader from './DeadlineReportsHeader';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import useDeadlineTable from 'hooks/ui/useDeadlineTable';
import { useGetDeadlineReportQuery, EntityType, DeadlineStatus } from 'store/api/reportsApi';

const DeadlineReports = () => {
  const [page, setPage] = useState(1);
  const [pageSize] = useState(20);
  const [entityTypeFilter, setEntityTypeFilter] = useState<EntityType | ''>('');
  const [statusFilter, setStatusFilter] = useState<DeadlineStatus | ''>('');

  // Fetch deadline report data
  const { data, isLoading, isError, error } = useGetDeadlineReportQuery({
    page,
    pageSize,
    entityType: entityTypeFilter || undefined,
    status: statusFilter || undefined,
  });

  const table = useDeadlineTable({
    data: data?.deadlines || [],
    selection: true,
    sortable: true,
    pagination: true,
    perPage: pageSize,
    selectionColumnWidth: 52
  });

  const handleFilterChange = (filterType: string, value: string) => {
    if (filterType === 'entityType') {
      setEntityTypeFilter(value as EntityType | '');
    } else if (filterType === 'status') {
      setStatusFilter(value as DeadlineStatus | '');
    }
    setPage(1); // Reset to first page when filter changes
  };

  const handlePageChange = (newPage: number) => {
    setPage(newPage);
  };

  return (
    <AdvanceTableProvider {...table}>
      <Card>
        <Card.Header className="border-bottom border-200 px-0">
          <DeadlineReportsHeader
            onFilterChange={handleFilterChange}
            entityTypeFilter={entityTypeFilter}
            statusFilter={statusFilter}
          />
        </Card.Header>
        <Card.Body className="p-0">
          {isLoading && (
            <div className="text-center py-5">
              <Spinner animation="border" role="status">
                <span className="visually-hidden">Loading...</span>
              </Spinner>
            </div>
          )}
          {isError && (
            <Alert variant="danger" className="m-3">
              <Alert.Heading>Error loading data</Alert.Heading>
              <p>
                {error && 'status' in error
                  ? `Error ${error.status}: ${JSON.stringify(error.data)}`
                  : 'An unexpected error occurred'}
              </p>
            </Alert>
          )}
          {!isLoading && !isError && data && (
            <>
              {(data.deadlines?.length ?? 0) === 0 ? (
                <div className="text-center py-5">
                  <p className="text-muted">No deadlines found</p>
                </div>
              ) : (
                <AdvanceTable
                  headerClassName="bg-body-tertiary align-middle"
                  rowClassName="btn-reveal-trigger align-middle"
                  tableProps={{
                    size: 'sm',
                    className: 'fs-10 mb-0 overflow-hidden'
                  }}
                />
              )}
            </>
          )}
        </Card.Body>
        <Card.Footer>
          {data && (data.totalPages ?? 0) > 1 && (
            <div className="d-flex justify-content-between align-items-center">
              <div className="fs-10 text-muted">
                Page {data.page} of {data.totalPages} ({data.total} total)
              </div>
              <div className="d-flex gap-2">
                <button
                  className="btn btn-sm btn-falcon-default"
                  onClick={() => handlePageChange(page - 1)}
                  disabled={page === 1}
                >
                  Previous
                </button>
                <button
                  className="btn btn-sm btn-falcon-default"
                  onClick={() => handlePageChange(page + 1)}
                  disabled={page >= (data.totalPages ?? 1)}
                >
                  Next
                </button>
              </div>
            </div>
          )}
        </Card.Footer>
      </Card>
    </AdvanceTableProvider>
  );
};

export default DeadlineReports;

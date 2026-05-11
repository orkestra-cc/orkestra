import classNames from 'classnames';
import { Link } from 'react-router';
import Flex from 'components/common/Flex';
import Avatar from 'components/common/Avatar';
import { surveyResults } from 'data/support-desk/reportsData';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import { Button, Card, Col, Form, ProgressBar, Row } from 'react-bootstrap';
import AdvanceTablePagination from 'components/common/advance-table/AdvanceTablePagination';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider, {
  useAdvanceTableContext
} from 'providers/AdvanceTableProvider';

const columns = [
  {
    accessorKey: 'name',
    header: '	Agent Name',
    meta: {
      headerProps: { style: { height: '46px' }, className: 'text-900' },
      cellProps: {
        className: 'white-space-nowrap pe-4 w-lg-25 py-3'
      }
    },
    cell: ({ row: { original } }: any) => {
      const { name, avatar } = original;
      return (
        <Flex alignItems="center" className="gap-2 position-relative py-2">
          {avatar.img ? (
            <Avatar src={avatar.img} size="xl" />
          ) : (
            <Avatar size="xl" name={avatar.name} />
          )}
          <h6 className="mb-0">
            <Link to="#!" className="stretched-link text-800">
              {name}
            </Link>
          </h6>
        </Flex>
      );
    }
  },
  {
    accessorKey: 'satisfied',
    header: 'Extremely Satisfied',
    meta: {
      headerProps: { className: 'text-900' },
      cellProps: {
        className: 'white-space-nowrap pe-5 pe-xxl-8'
      }
    },
    cell: ({ row: { original } }: any) => {
      const { satisfied } = original;
      return (
        <Flex alignItems="center">
          <div>
            <Flex alignItems="baseline" className="gap-1 mb-1">
              <h5 className="mb-0">{satisfied.total}</h5>
              <h6
                className={classNames('mb-0 fw-semibold', {
                  'text-primary': satisfied.countUp,
                  'text-danger': !satisfied.countUp
                })}
              >
                <FontAwesomeIcon
                  icon={`caret-${satisfied.countUp ? 'up' : 'down'}`}
                  className="fs-11 me-1 ms-2"
                />
                <span className="fs-10">{satisfied.count}</span>
              </h6>
            </Flex>
            <h6 className="mb-0 text-700">{satisfied.date}</h6>
          </div>
          <div className="ms-4">
            <ProgressBar
              variant="primary"
              now={satisfied.progressBarWidth[0]}
              style={{ width: '174px', height: '5px' }}
              className="mb-1"
            />
            <ProgressBar
              variant="info"
              now={satisfied.progressBarWidth[1]}
              style={{ width: '174px', height: '5px' }}
            />
          </div>
        </Flex>
      );
    }
  },
  {
    accessorKey: 'dissatisfied',
    header: 'Extremely Dissatisfied',
    meta: {
      headerProps: { className: 'text-900' },
      cellProps: {
        className: 'white-space-nowrap'
      }
    },
    cell: ({ row: { original } }: any) => {
      const { dissatisfied } = original;
      return (
        <Flex alignItems="center">
          <div>
            <Flex alignItems="baseline" className="gap-1 mb-1">
              <h5 className="mb-0">{dissatisfied.total}</h5>
              <h6
                className={classNames('mb-0 fw-semibold', {
                  'text-primary': !dissatisfied.countUp,
                  'text-danger': dissatisfied.countUp
                })}
              >
                <FontAwesomeIcon
                  icon={`caret-${dissatisfied.countUp ? 'up' : 'down'}`}
                  className="fs-11 me-1 ms-2"
                />
                <span className="fs-10">{dissatisfied.count}</span>
              </h6>
            </Flex>
            <h6 className="mb-0 text-700">{dissatisfied.date}</h6>
          </div>
          <div className="ms-4">
            <ProgressBar
              variant="primary"
              now={dissatisfied.progressBarWidth[0]}
              style={{ width: '174px', height: '5px' }}
              className="mb-1"
            />
            <ProgressBar
              variant="info"
              now={dissatisfied.progressBarWidth[1]}
              style={{ width: '174px', height: '5px' }}
            />
          </div>
        </Flex>
      );
    }
  }
];

const BulkAction = () => {
  const { getSelectedRowModel } = useAdvanceTableContext();

  return (
    <>
      {getSelectedRowModel().rows.length > 0 ? (
        <div className="d-flex">
          <Form.Select size="sm" aria-label="Bulk actions">
            <option>Bulk Actions</option>
            <option value="refund">Refund</option>
            <option value="delete">Delete</option>
            <option value="archive">Archive</option>
          </Form.Select>
          <Button
            type="button"
            variant="falcon-default"
            size="sm"
            className="ms-2"
          >
            Apply
          </Button>
        </div>
      ) : (
        <Form.Select size="sm" defaultValue="Monthly">
          <option>Daily</option>
          <option>Weekly</option>
          <option>Monthly</option>
          <option>Yearly</option>
        </Form.Select>
      )}
    </>
  );
};

const SurveyResult = () => {
  const table = useAdvanceTable({
    data: surveyResults,
    columns,
    selection: true,
    sortable: true,
    pagination: true,
    perPage: 5,
    selectionColumnWidth: 30
  });

  return (
    <AdvanceTableProvider {...table}>
      <Card className="mt-3">
        <Card.Header className="border-bottom border-200">
          <Row className="flex-between-center gy-2">
            <Col xs="auto">
              <h6 className="mb-0 me-x1">Agent Survey Result</h6>
            </Col>
            <Col xs="auto">
              <BulkAction />
            </Col>
          </Row>
        </Card.Header>
        <Card.Body className="p-0">
          <AdvanceTable
            headerClassName="bg-body-tertiary align-middle"
            rowClassName="btn-reveal-trigger align-middle"
            tableProps={{
              size: 'sm',
              className: 'fs-10 mb-0 overflow-hidden'
            }}
          />
        </Card.Body>
        <Card.Footer>
          <AdvanceTablePagination />
        </Card.Footer>
      </Card>
    </AdvanceTableProvider>
  );
};

export default SurveyResult;

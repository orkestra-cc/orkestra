import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';
import { useState } from 'react';
import { Button, Form } from 'react-bootstrap';
import Flex from '../Flex';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';

interface AdvanceTableFooterProps {
  viewAllBtn?: boolean;
  navButtons?: boolean;
  className?: string;
  rowInfo?: boolean;
  rowsPerPageSelection?: boolean;
  rowsPerPageOptions?: number[];
}

export const AdvanceTableFooter = ({
  viewAllBtn,
  navButtons,
  className,
  rowInfo,
  rowsPerPageSelection,
  rowsPerPageOptions = [5, 10, 15]
}: AdvanceTableFooterProps) => {
  const {
    setPageSize,
    previousPage,
    nextPage,
    getCanNextPage,
    getCanPreviousPage,
    getState,
    getPrePaginationRowModel,
    getPaginationRowModel
  } = useAdvanceTableContext();

  const {
    pagination: { pageSize, pageIndex }
  } = getState();
  const [perPage] = useState(pageSize);
  const [isAllVisible, setIsAllVisible] = useState(false);

  return (
    <Flex
      className={classNames(
        className,
        'align-items-center justify-content-between'
      )}
    >
      <Flex alignItems="center" className="fs-10">
        {rowInfo && (
          <p className="mb-0">
            <span className="d-none d-sm-inline-block me-2">
              {pageSize * pageIndex + 1} to{' '}
              {pageSize * pageIndex + getPaginationRowModel().rows.length} of{' '}
              {getPrePaginationRowModel().rows.length}
            </span>
            {viewAllBtn && (
              <>
                <span className="d-none d-sm-inline-block me-2">&mdash;</span>
                <Button
                  variant="link"
                  size="sm"
                  className="py-2 px-0 fw-semibold"
                  onClick={() => {
                    setIsAllVisible(!isAllVisible);
                    setPageSize(
                      isAllVisible
                        ? perPage
                        : getPrePaginationRowModel().rows.length
                    );
                  }}
                >
                  View {isAllVisible ? 'less' : 'all'}
                  <FontAwesomeIcon
                    icon="chevron-right"
                    className="ms-1 fs-11"
                  />
                </Button>
              </>
            )}
          </p>
        )}
        {rowsPerPageSelection && (
          <>
            <p className="mb-0 mx-2">Rows per page:</p>
            <Form.Select
              size="sm"
              className="w-auto"
              onChange={e => setPageSize(Number(e.target.value))}
              defaultValue={pageSize}
            >
              {rowsPerPageOptions.map(value => (
                <option value={value} key={value}>
                  {value}
                </option>
              ))}
            </Form.Select>
          </>
        )}
      </Flex>
      {navButtons && (
        <Flex>
          <Button
            size="sm"
            variant={getCanPreviousPage() ? 'primary' : 'tertiary'}
            onClick={() => previousPage()}
            className={classNames({ disabled: !getCanPreviousPage() })}
          >
            Previous
          </Button>
          <Button
            size="sm"
            variant={getCanNextPage() ? 'primary' : 'tertiary'}
            className={classNames('px-4 ms-2', {
              disabled: !getCanNextPage()
            })}
            onClick={() => nextPage()}
          >
            Next
          </Button>
        </Flex>
      )}
    </Flex>
  );
};

export default AdvanceTableFooter;

import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';
import { Button } from 'react-bootstrap';
import Flex from '../Flex';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';

export const AdvanceTablePagination = () => {
  const {
    previousPage,
    nextPage,
    getCanNextPage,
    getCanPreviousPage,
    getState,
    getPageCount,
    setPageIndex
  } = useAdvanceTableContext();

  const {
    pagination: { pageIndex }
  } = getState();

  return (
    <Flex alignItems="center" justifyContent="center">
      <Button
        size="sm"
        variant="orkestra-default"
        onClick={() => previousPage()}
        className={classNames({ disabled: !getCanPreviousPage() })}
      >
        <FontAwesomeIcon icon="chevron-left" />
      </Button>
      <ul className="pagination mb-0 mx-2">
        {Array.from(Array(getPageCount()).keys()).map((page, index) => (
          <li key={page} className={classNames({ active: pageIndex === page })}>
            <Button
              size="sm"
              variant="orkestra-default"
              className={classNames('page', {
                'me-2': index + 1 !== getPageCount()
              })}
              onClick={() => setPageIndex(page)}
            >
              {page + 1}
            </Button>
          </li>
        ))}
      </ul>
      <Button
        size="sm"
        variant="orkestra-default"
        onClick={() => nextPage()}
        className={classNames({ disabled: !getCanNextPage() })}
      >
        <FontAwesomeIcon icon="chevron-right" />
      </Button>
    </Flex>
  );
};

export default AdvanceTablePagination;

import { Card } from 'react-bootstrap';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import VehicleTableHeader from './VehicleTableHeader';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import useVehicleTable from 'hooks/ui/useVehicleTable';

const VehicleTable = () => {
  const { ActivationModal, ...table } = useVehicleTable({
    selection: true,
    sortable: true,
    pagination: true,
    perPage: 10,
    selectionColumnWidth: 52
  });

  return (
    <>
      <AdvanceTableProvider {...table}>
        <Card>
          <Card.Header className="border-bottom border-200 px-0">
            <VehicleTableHeader />
          </Card.Header>
          <Card.Body className="p-0">
            <AdvanceTable
              headerClassName="bg-body-tertiary align-middle"
              bodyClassName=""
              rowClassName="btn-reveal-trigger align-middle"
              tableProps={{
                size: 'sm',
                className: 'fs-10 mb-0 overflow-hidden'
              }}
            />
          </Card.Body>
          <Card.Footer>
            <AdvanceTableFooter
              viewAllBtn={false}
              navButtons={true}
              className=""
              rowInfo={true}
              rowsPerPageSelection={true}
            />
          </Card.Footer>
        </Card>
      </AdvanceTableProvider>
      <ActivationModal />
    </>
  );
};

export default VehicleTable;
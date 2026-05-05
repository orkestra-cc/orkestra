import { useState } from 'react';
import { Card } from 'react-bootstrap';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import UnsolvedTicketsHeader from './UnsolvedTicketsHeader';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import useSupportDeskTable from 'hooks/ui/useSupportDeskTable';
import CardLayout from 'reference/app-examples/support-desk/tickets-layout/CardLayout';

const UnsolvedTickets = () => {
  const [layout, setLayout] = useState('tableView');

  const table = useSupportDeskTable({
    selection: true,
    sortable: true,
    pagination: true,
    perPage: 6,
    selectionColumnWidth: 52
  });

  return (
    <AdvanceTableProvider {...table}>
      <Card>
        <Card.Header className="border-bottom border-200 px-0">
          <UnsolvedTicketsHeader setLayout={setLayout} layout={layout} />
        </Card.Header>
        {layout === 'tableView' ? (
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
        ) : (
          <Card.Body className="bg-body-tertiary">
            <CardLayout />
          </Card.Body>
        )}
        <Card.Footer>
          <AdvanceTableFooter rowInfo navButtons />
        </Card.Footer>
      </Card>
    </AdvanceTableProvider>
  );
};

export default UnsolvedTickets;

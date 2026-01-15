import { useState } from 'react';
import { Card, Col, Offcanvas, Row } from 'react-bootstrap';
import AllTicketsHeader from './AllTicketsHeader';
import TicketFilteringForm from './TicketFilteringForm';
import { useBreakpoints } from 'hooks/ui/useBreakpoints';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import AdvanceTablePagination from 'components/common/advance-table/AdvanceTablePagination';
import useSupportDeskTable from 'hooks/ui/useSupportDeskTable';
import CardLayout from './CardLayout';

const CardView = () => {
  const [show, setShow] = useState(false);
  const { breakpoints } = useBreakpoints();
  const handleClose = () => setShow(false);
  const handleShow = () => setShow(true);

  const table = useSupportDeskTable({
    perPage: 7,
    pagination: true
  });

  return (
    <Row className="gx-3">
      <Col xxl={10} xl={9}>
        <AdvanceTableProvider {...table}>
          <Card>
            <Card.Header className="border-bottom border-200 px-0">
              <AllTicketsHeader layout="card-view" handleShow={handleShow} />
            </Card.Header>
            <Card.Body className="bg-body-tertiary">
              <CardLayout />
            </Card.Body>
            <Card.Footer className="d-flex justify-content-center">
              <AdvanceTablePagination />
            </Card.Footer>
          </Card>
        </AdvanceTableProvider>
      </Col>
      <Col xxl={2} xl={3}>
        {breakpoints.down('xl') ? (
          <Offcanvas
            show={show}
            onHide={handleClose}
            placement="end"
            className="dark__bg-card-dark"
          >
            <Offcanvas.Header closeButton className="bg-body-tertiary">
              <h6 className="fs-9 mb-0 fw-semibold">Filter</h6>
            </Offcanvas.Header>
            <TicketFilteringForm />
          </Offcanvas>
        ) : (
          <TicketFilteringForm />
        )}
      </Col>
    </Row>
  );
};

export default CardView;

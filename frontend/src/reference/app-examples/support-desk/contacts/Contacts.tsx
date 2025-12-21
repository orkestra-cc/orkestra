import { useState } from 'react';
import { Link } from 'react-router';
import { Card, Col, Offcanvas, Row } from 'react-bootstrap';
import paths from 'routes/paths';
import Flex from 'components/common/Flex';
import Avatar from 'components/common/Avatar';
import SubtleBadge from 'components/common/SubtleBadge';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTablePagination from 'components/common/advance-table/AdvanceTablePagination';
import { contacts } from 'data/support-desk/contactsData';
import ContactsHeader from './ContactsHeader';
import ContactsFilteringForm from './ContactsFilteringForm';
import { useBreakpoints } from 'hooks/ui/useBreakpoints';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';

const columns = [
  {
    accessorKey: 'name',
    header: 'Name',
    meta: {
      headerProps: { className: 'ps-2 text-900', style: { height: '46px' } },
      cellProps: {
        className: 'white-space-nowrap pe-5 ps-2 py-2'
      }
    },
    cell: ({ row: { original } }: any) => {
      const { name, avatar } = original;
      return (
        <Flex alignItems="center" className="position-relative py-1">
          {avatar.img ? (
            <Avatar src={avatar.img} size="xl" className="me-2" />
          ) : (
            <Avatar size="xl" name={avatar.name} className="me-2" />
          )}
          <h6 className="mb-0">
            <Link to={paths.contactDetails} className="stretched-link text-900">
              {name}
            </Link>
          </h6>
        </Flex>
      );
    }
  },
  {
    accessorKey: 'phone',
    header: 'Phone Number',
    meta: {
      headerProps: { className: 'pe-5 white-space-nowrap text-900' },
      cellProps: { className: 'font-sans-serif white-space-nowrap' }
    },
    cell: ({ row: { original } }: any) => {
      const { phone } = original;
      return (
        <a className="text-700" href={`tel:${phone.replace(/\s+/g, '')}`}>
          {phone}
        </a>
      );
    }
  },
  {
    accessorKey: 'report',
    header: 'Name of the report',
    meta: {
      headerProps: { className: 'text-900' },
      cellProps: {
        className: 'white-space-nowrap pe-5'
      }
    },
    cell: ({ row: { original } }: any) => {
      const { report } = original;
      return <Link to="#!">{report}</Link>;
    }
  },
  {
    accessorKey: 'subscription',
    header: 'Subscription',
    meta: {
      headerProps: { className: 'text-end white-space-nowrap text-900' },
      cellProps: {
        className: 'font-sans-serif fs-9 text-end'
      }
    },
    cell: ({ row: { original } }: any) => {
      const { subscription } = original;
      return subscription ? (
        <SubtleBadge bg={subscription.type}>{subscription.content}</SubtleBadge>
      ) : (
        <p className="mb-0 text-500 font-sans-serif fs-10">N/A</p>
      );
    }
  },
  {
    accessorKey: 'social',
    header: 'Social',
    meta: {
      headerProps: { className: 'text-end text-900' },
      cellProps: { className: 'ps-4 text-end' }
    },
    cell: ({ row: { original } }: any) => {
      const { social } = original;
      return social ? (
        <a href="#!">{social}</a>
      ) : (
        <p className="mb-0 text-500">N/A</p>
      );
    }
  }
];

const Contacts = () => {
  const [show, setShow] = useState(false);
  const { breakpoints } = useBreakpoints();
  const handleClose = () => setShow(false);
  const handleShow = () => setShow(true);

  const table = useAdvanceTable({
    data: contacts,
    columns,
    selection: true,
    sortable: true,
    pagination: true,
    perPage: 11,
    selectionColumnWidth: 52
  });

  return (
    <Row className="gx-3">
      <Col xxl={10} xl={9}>
        <AdvanceTableProvider {...table}>
          <Card>
            <Card.Header className="px-0">
              <ContactsHeader handleShow={handleShow} />
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
            <ContactsFilteringForm />
          </Offcanvas>
        ) : (
          <ContactsFilteringForm />
        )}
      </Col>
    </Row>
  );
};

export default Contacts;

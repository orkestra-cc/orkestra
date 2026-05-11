
import { Card } from 'react-bootstrap';
import { Link } from 'react-router';
import type { CellContext } from '@tanstack/react-table';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import Avatar from 'components/common/Avatar';
import CardDropdown from 'components/common/CardDropdown';
import FalconCardFooterLink from 'components/common/FalconCardFooterLink';
import FalconCardHeader from 'components/common/FalconCardHeader';
import Flex from 'components/common/Flex';
import IconItem from 'components/common/icon/IconItem';
import SubtleBadge, { BadgeColor } from 'components/common/SubtleBadge';
import { recentLeadsTableData } from 'data/dashboard/crm';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';

interface LeadData {
  id: string | number;
  name: string;
  img: string;
  email: string;
  status: string;
  variant: BadgeColor;
}

const columns = [
  {
    accessorKey: 'name',
    header: 'Name',
    meta: {
      headerProps: {
        className: 'py-0 ps-0 text-900'
      },
      cellProps: {
        className: 'ps-0'
      }
    },
    cell: ({ row: { original } }: CellContext<LeadData, unknown>) => {
      const { name, img } = original;
      return (
        <Link to="#!" className="text-800">
          <Flex alignItems="center">
            <Avatar size="xl" src={img} />
            <h6 className="mb-0 ps-2">{name}</h6>
          </Flex>
        </Link>
      );
    }
  },
  {
    accessorKey: 'email',
    header: 'Email and Phone',
    meta: {
      headerProps: { className: 'text-900' }
    },
    cell: ({ row: { original } }: CellContext<LeadData, unknown>) => {
      const { email } = original;
      return (
        <a href={`mailto:${email}`} className="mb-0">
          {email}
        </a>
      );
    }
  },
  {
    accessorKey: 'status',
    header: 'Status',
    meta: {
      headerProps: { className: 'text-900' }
    },
    cell: ({ row: { original } }: CellContext<LeadData, unknown>) => {
      const { status, variant } = original;
      return (
        <SubtleBadge pill bg={variant} className="me-2">
          {status}
        </SubtleBadge>
      );
    }
  },
  {
    accessorKey: 'Action',
    header: 'Action',
    meta: {
      headerProps: {
        className: 'text-end text-900'
      },
      cellProps: {
        className: 'text-end'
      }
    },
    enableSorting: false,
    cell: () => (
      <div>
        <div className="hover-actions bg-100">
          <IconItem
            tag="button"
            icon={['far', 'edit']}
            size="sm"
            className="btn rounded-3 me-2 fs-11"
          />
          <IconItem
            tag="button"
            icon={['far', 'comment']}
            size="sm"
            className="btn rounded-3 me-2 fs-11"
          />
        </div>
        <CardDropdown btnRevealClass="btn-reveal-sm" drop="start" />
      </div>
    )
  }
];

const RecentLeads = () => {
  const table = useAdvanceTable({
    data: recentLeadsTableData,
    columns,
    selection: true,
    sortable: true,
    pagination: true,
    perPage: 10,
    selectionColumnWidth: 28,
    selectionHeaderClassname: 'py-0'
  });

  return (
    <AdvanceTableProvider {...table}>
      <Card>
        <FalconCardHeader
          title="Recent Leads"
          titleTag="h6"
          className="py-2"
          endEl={<CardDropdown />}
        />
        <Card.Body className="p-0">
          <AdvanceTable
            headerClassName="bg-200 text-nowrap align-middle"
            rowClassName="align-middle white-space-nowrap hover-actions-trigger btn-reveal-trigger hover-bg-100"
            tableProps={{
              className: 'fs-10 mb-0 overflow-hidden'
            }}
          />
        </Card.Body>
        <Card.Footer className="p-0">
          <FalconCardFooterLink title="Show full list" size="sm" />
        </Card.Footer>
      </Card>
    </AdvanceTableProvider>
  );
};

export default RecentLeads;

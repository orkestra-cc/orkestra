
import paths from 'routes/paths';
import { Link } from 'react-router';
import useAdvanceTable from './useAdvanceTable';
import Avatar from 'components/common/Avatar';
import Flex from 'components/common/Flex';
import SubtleBadge from 'components/common/SubtleBadge';
import { tickets } from 'data/dashboard/support-desk';
import Priority from 'reference/app-examples/support-desk/tickets-layout/Priority';
import AgentSelect from 'reference/app-examples/support-desk/tickets-layout/AgentSelect';

const columns = [
  {
    accessorKey: 'name',
    header: 'Client',
    meta: {
      headerProps: { className: 'ps-2 text-900', style: { height: '46px' } },
      cellProps: {
        className: 'py-2 white-space-nowrap pe-3 pe-xxl-4 ps-2'
      }
    },
    cell: ({ row: { original } }: { row: { original: any } }) => {
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
    accessorKey: 'subject',
    header: 'Subject',
    meta: {
      headerProps: {
        style: { minWidth: '14.625rem' },
        className: 'text-900'
      },
      cellProps: {
        className: 'py-2 pe-4'
      }
    },
    cell: ({ row: { original } }: { row: { original: any } }) => {
      const { subject } = original;
      return (
        <Link to={paths.ticketsPreview} className="fw-semibold">
          {subject}
        </Link>
      );
    }
  },
  {
    accessorKey: 'status',
    header: 'Status',
    meta: {
      headerProps: { className: 'text-900' },
      cellProps: {
        className: 'fs-9 pe-4'
      }
    },
    cell: ({ row: { original } }: { row: { original: any } }) => {
      const { status } = original;
      return (
        <SubtleBadge bg={status.type} className="me-2">
          {status.content}
        </SubtleBadge>
      );
    }
  },
  {
    accessorKey: 'priority',
    header: 'Priority',
    meta: {
      headerProps: { className: 'text-900' },
      cellProps: {
        className: 'pe-4'
      }
    },
    cell: ({ row: { original } }: { row: { original: any } }) => {
      const { priority } = original;
      return (
        <Priority
          title={priority.title}
          color={priority.color}
          data={priority.data}
        />
      );
    }
  },
  {
    accessorKey: 'agent',
    header: 'Agent',
    meta: {
      headerProps: { className: 'text-end text-900' }
    },
    cell: ({ row: { original } }: { row: { original: any } }) => {
      const { agent } = original;
      return <AgentSelect agent={agent} className="w-auto ms-auto" />;
    }
  }
];

const useSupportDeskTable = (options?: any) => {
  const table = useAdvanceTable({
    columns,
    data: tickets,
    ...options
  });

  return table;
};

export default useSupportDeskTable;

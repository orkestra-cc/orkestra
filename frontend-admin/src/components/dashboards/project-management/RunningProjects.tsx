
import { Card, ProgressBar } from 'react-bootstrap';
import type { CellContext } from '@tanstack/react-table';
import Flex from 'components/common/Flex';
import Avatar, { AvatarGroup } from 'components/common/Avatar';
import { Link } from 'react-router';
import FalconCardFooterLink from 'components/common/FalconCardFooterLink';
import FalconCardHeader from 'components/common/FalconCardHeader';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';

interface MemberData {
  id: string | number;
  img?: string;
  name?: string;
}

interface ProjectData {
  id: string | number;
  avatar: { name: string };
  color: string;
  title: string;
  projectName: string;
  progress: number;
  duration: string;
  date: string;
  members: MemberData[];
}

const columns = [
  {
    accessorKey: 'title',
    header: 'Projects',
    meta: {
      headerProps: { className: 'text-800' }
    },
    cell: ({ row: { original } }: CellContext<ProjectData, unknown>) => {
      const { avatar, color, title, projectName } = original;
      return (
        <Flex alignItems="center" className="position-relative">
          <Avatar
            name={avatar.name}
            mediaClass={`text-${color}-emphasis bg-${color}-subtle fs-9`}
          />
          <div className="flex-1 ms-3">
            <h6 className="mb-0 fw-semibold">
              <Link className="text-1100 stretched-link" to="#!">
                {title}
              </Link>
            </h6>
            <p className="fs-11 mb-0 text-500">{projectName}</p>
          </div>
        </Flex>
      );
    }
  },
  {
    accessorKey: 'progress',
    header: 'Worked',
    meta: {
      headerProps: {
        className: 'text-center text-800'
      },
      cellProps: {
        className: 'text-center'
      }
    },
    cell: ({ row: { original } }: CellContext<ProjectData, unknown>) => {
      const { progress } = original;
      return (
        <ProgressBar
          now={progress}
          style={{ height: 5 }}
          className="rounded-pill align-middle"
          variant="progress-gradient"
        />
      );
    }
  },
  {
    accessorKey: 'duration',
    header: 'Progress',
    meta: {
      cellProps: {
        className: 'text-center fw-semibold fs-10'
      },
      headerProps: {
        className: 'text-center text-800'
      }
    }
  },
  {
    accessorKey: 'date',
    header: 'Due Date',
    meta: {
      cellProps: {
        className: 'text-center fw-semibold fs-10'
      },
      headerProps: {
        className: 'text-center text-800'
      }
    }
  },
  {
    accessorKey: 'members',
    header: 'Members',
    enableSorting: false,
    meta: {
      headerProps: {
        className: 'text-end text-800'
      }
    },
    cell: ({ row: { original } }: CellContext<ProjectData, unknown>) => {
      const { members } = original;
      return (
        <AvatarGroup className="justify-content-end">
          {members.map(({ img, name, id }: MemberData) => {
            return (
              <Avatar
                src={img && img}
                key={id}
                name={name && name}
                isExact
                className="border border-3 rounded-circle border-200"
              />
            );
          })}
        </AvatarGroup>
      );
    }
  }
];

interface RunningProjectsProps {
  data: ProjectData[];
}

const RunningProjects = ({ data }: RunningProjectsProps) => {
  const table = useAdvanceTable({
    data,
    columns,
    sortable: true,
    pagination: true,
    perPage: 10
  });

  return (
    <AdvanceTableProvider {...table}>
      <Card className="h-100">
        <FalconCardHeader title="Running Projects" titleTag="h6" />
        <Card.Body className="p-0">
          <AdvanceTable
            headerClassName="bg-body-tertiary text-nowrap align-middle"
            rowClassName="align-middle white-space-nowrap"
            tableProps={{
              borderless: true,
              className: 'fs-11 mb-0 overflow-hidden'
            }}
          />
        </Card.Body>
        <FalconCardFooterLink title="Show all projects" size="sm" />
      </Card>
    </AdvanceTableProvider>
  );
};

export default RunningProjects;

import CardDropdown from 'components/common/CardDropdown';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';

import { Card } from 'react-bootstrap';
import Avatar, { AvatarGroup } from 'components/common/Avatar';
import Statistics from './Statistics';
import ProjectTable from './ProjectTable';

interface ProjectUser {
  id: string | number;
  img?: string;
  name?: string;
}

interface StatisticsItem {
  id: string | number;
  variant: string;
  amount: number;
}

interface ProjectItem {
  id: string | number;
  project: string;
  team: string;
  iconColor: string;
}

interface ProjectStatisticsProps {
  progressBar: StatisticsItem[];
  projectsTable: ProjectItem[];
  projectUsers: ProjectUser[];
}

const ProjectStatistics = ({
  progressBar,
  projectsTable,
  projectUsers
}: ProjectStatisticsProps) => {
  return (
    <Card className="h-100">
      <OrkestraCardHeader
        title="Project Statistics"
        titleTag="h6"
        endEl={<CardDropdown />}
      />
      <Card.Body className="pt-0">
        <Statistics data={progressBar} />

        <p className="fs-10 mb-2 mt-3">Assignees in Sprint</p>
        <AvatarGroup dense>
          {projectUsers.map(({ img, name, id }: ProjectUser) => {
            return (
              <Avatar
                src={img && img}
                key={id}
                name={name && name}
                isExact
                size="2xl"
                className="border border-3 rounded-circle border-200"
              />
            );
          })}
        </AvatarGroup>

        <ProjectTable data={projectsTable} />
      </Card.Body>
    </Card>
  );
};

export default ProjectStatistics;

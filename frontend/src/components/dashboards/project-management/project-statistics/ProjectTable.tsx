
import Flex from 'components/common/Flex';
import classNames from 'classnames';
import { Link } from 'react-router';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

interface ProjectItem {
  id: string | number;
  project: string;
  team: string;
  iconColor: string;
}

interface ProjectTableProps {
  data: ProjectItem[];
}

const ProjectTable = ({ data }: ProjectTableProps) => {
  return (
    <>
      <Flex justifyContent="between" className="mt-3">
        <h6>Project</h6>
        <h6>Team</h6>
      </Flex>
      {data.map((project: ProjectItem, index: number) => {
        return (
          <Flex
            key={project.id}
            alignItems="center"
            justifyContent="between"
            className={classNames('rounded-3 bg-body-tertiary p-3 ', {
              'mb-2': index !== data.length - 1
            })}
          >
            <>
              <Link to="#!">
                <h6 className="mb-0">
                  <FontAwesomeIcon
                    icon="circle"
                    className={`fs-10 me-3 ${project.iconColor}`}
                  />
                  {project.project}
                </h6>
              </Link>
              <Link className="fs-11 text-600 mb-0" to="#!">
                {project.team}
              </Link>
            </>
          </Flex>
        );
      })}
    </>
  );
};

export default ProjectTable;

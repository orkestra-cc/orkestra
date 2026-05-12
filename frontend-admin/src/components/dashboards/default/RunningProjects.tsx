import classNames from 'classnames';
import OrkestraCardFooterLink from 'components/common/OrkestraCardFooterLink';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import Flex from 'components/common/Flex';

import { Badge, Card, Col, Form, ProgressBar, Row } from 'react-bootstrap';

interface ProjectData {
  id: string | number;
  color: string;
  progress: number;
  duration: string;
  title: string;
}

interface ProjectProps {
  project: ProjectData;
  isLast: boolean;
}

const Project = ({ project, isLast }: ProjectProps) => {
  const { color, progress, duration, title } = project;
  return (
    <Row
      className={classNames('position-relative align-items-center py-2 g-0', {
        'border-bottom border-200': !isLast
      })}
    >
      <Col className="py-1 ps-x1">
        <Flex className="align-items-center">
          <div className="avatar avatar-xl me-3">
            <div className={`avatar-name rounded-circle bg-${color}-subtle`}>
              <span className={`fs-9 text-${color.split('-')[1] || color}`}>
                {title[0]}
              </span>
            </div>
          </div>
          <Flex tag="h6" className="mb-0 align-items-center">
            <a className="text-800 stretched-link" href="#!">
              {title}
            </a>
            <Badge pill bg="200" className="ms-2 text-primary">
              {progress}%
            </Badge>
          </Flex>
        </Flex>
      </Col>
      <Col>
        <Row className="justify-content-end align-items-center g-0">
          <Col xs="auto" className="pe-2">
            <div className="fs-10 fw-semibold">{duration}</div>
          </Col>
          <Col xs="5" className="ps-2 pe-x1">
            <ProgressBar now={progress} style={{ height: 5 }} />
          </Col>
        </Row>
      </Col>
    </Row>
  );
};

interface RunningProjectsProps {
  data: ProjectData[];
}

const RunningProjects = ({ data }: RunningProjectsProps) => {
  return (
    <Card>
      <OrkestraCardHeader
        title="Running Projects"
        light
        titleTag="h6"
        endEl={
          <Form.Select size="sm" className="me-2">
            <option>Working Time</option>
            <option>Estimated Time</option>
            <option>Billable Time</option>
          </Form.Select>
        }
      />

      <Card.Body className="p-0">
        {data.map((project: ProjectData, index: number) => (
          <Project
            project={project}
            isLast={index === data.length - 1}
            key={project.id}
          />
        ))}
      </Card.Body>

      <OrkestraCardFooterLink title="Show all projects" size="sm" />
    </Card>
  );
};

export default RunningProjects;

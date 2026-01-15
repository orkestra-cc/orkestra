import { Card, Col, Row, Tab, Nav, Form } from 'react-bootstrap';
import Flex from 'components/common/Flex';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import AudienceChart from './AudienceChart';
import FalconLink from 'components/common/FalconLink';
import SimpleBar from 'simplebar-react';
import classNames from 'classnames';

interface TabTitleProps {
  title: string;
  value: string;
  percentage: string;
  progress: boolean;
}

interface AudienceChartData {
  users: number[][];
  sessions: number[][];
  rate: number[][];
  duration: number[][];
}

interface AudienceProps {
  chartData: AudienceChartData;
  className?: string;
}

const TabTitle = ({ title, value, percentage, progress }: TabTitleProps) => (
  <div className="p-2 pe-4 text-start cursor-pointer">
    <h6 className="text-800 fs-11 text-nowrap">{title}</h6>
    <h5 className="text-800">{value}</h5>
    <Flex alignItems="center">
      <FontAwesomeIcon
        icon={progress ? 'caret-up' : 'caret-down'}
        className={progress ? 'text-success' : 'text-warning'}
      />
      <h6
        className={`fs-11 mb-0 ms-2 ${
          progress ? 'text-success' : 'text-warning'
        }`}
      >
        {percentage}
      </h6>
    </Flex>
  </div>
);

const Audience = ({ chartData, className }: AudienceProps) => {
  return (
    <Card className={classNames(className, 'overflow-hidden')}>
      <Tab.Container id="audience-tab" defaultActiveKey="users">
        <SimpleBar>
          <Card.Header className="p-0 bg-body-tertiary">
            <Nav className="nav-tabs border-0 flex-nowrap chart-tab">
              <Nav.Item>
                <Nav.Link className="mb-0" eventKey="users">
                  <TabTitle
                    title="Users"
                    value="3.9K"
                    progress={true}
                    percentage="62.0%"
                  />
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link className="mb-0" eventKey="sessions">
                  <TabTitle
                    title="Sessions"
                    value="6.3K"
                    progress={true}
                    percentage="46.2%"
                  />
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link className="mb-0" eventKey="bounceRate">
                  <TabTitle
                    title="Bounce Rate"
                    value="9.49%"
                    progress={false}
                    percentage="56.1%"
                  />
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link className="mb-0" eventKey="sessionDuration">
                  <TabTitle
                    title="Session Duration"
                    value="4m 03s"
                    progress={false}
                    percentage="32.2%"
                  />
                </Nav.Link>
              </Nav.Item>
            </Nav>
          </Card.Header>
        </SimpleBar>

        <Card.Body>
          <Tab.Content>
            <Tab.Pane unmountOnExit eventKey="users">
              <AudienceChart data={chartData.users} />
            </Tab.Pane>
            <Tab.Pane unmountOnExit eventKey="sessions">
              <AudienceChart data={chartData.sessions} />
            </Tab.Pane>
            <Tab.Pane unmountOnExit eventKey="bounceRate">
              <AudienceChart data={chartData.rate} />
            </Tab.Pane>
            <Tab.Pane unmountOnExit eventKey="sessionDuration">
              <AudienceChart data={chartData.duration} />
            </Tab.Pane>
          </Tab.Content>
        </Card.Body>
      </Tab.Container>

      <Card.Footer className="bg-body-tertiary py-2">
        <Row className="g-0 flex-between-center">
          <Col xs="auto">
            <Form.Select size="sm" className="me-2">
              <option>Last 7 days</option>
              <option>Last Month</option>
              <option>Last Year</option>
            </Form.Select>
          </Col>
          <Col xs="auto">
            <FalconLink title="Visitors overview" className="px-0 fw-medium" />
          </Col>
        </Row>
      </Card.Footer>
    </Card>
  );
};

export default Audience;

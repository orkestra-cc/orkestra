
import { Col, Row } from 'react-bootstrap';
import Greetings from 'components/dashboards/project-management/Greetings';
import TeamProgress from 'components/dashboards/project-management/TeamProgress';
import Discussion from 'components/dashboards/project-management/Discussion';
import CalendarManagement from 'components/dashboards/project-management/calendar/CalendarManagement';
import ProjectStatistics from 'components/dashboards/project-management/project-statistics/ProjectStatistics';
import Statistics from 'components/dashboards/project-management/statistics/Statistics';
import ToDoList from 'components/dashboards/project-management/ToDoList';
import ReportForThisWeek from 'components/dashboards/project-management/report-for-this-week/ReportForThisWeek';
import MemberInfo from 'components/dashboards/project-management/MemberInfo';
import RunningProjects from 'components/dashboards/project-management/RunningProjects';
import ProjectLocation from 'components/dashboards/project-management/project-location/ProjectLocation';
import {
  recentActivities,
  membersActivities,
  markers,
  greetingItems,
  discussionMembers,
  weeklyReport,
  progressBar,
  projectsTable,
  projectUsers,
  membersInfo,
  runningProjects,
  managementEvents
} from 'data/dashboard/projectManagement';
import RecentActivity from 'components/dashboards/project-management/RecentActivity';
import MembersActivity from 'components/dashboards/project-management/MembersActivity';

const ProjectManagement: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={6} lg={12}>
          <Greetings data={greetingItems} />
        </Col>
        <Col xxl={3} md={6}>
          <TeamProgress />
        </Col>
        <Col xxl={3} md={6}>
          <Discussion data={discussionMembers} />
        </Col>
      </Row>
      <Row className="g-3 mb-3">
        <Col xxl={6} lg={12}>
          <Row>
            <Col lg={12}>
              <Statistics />
            </Col>
            <Col lg={12}>
              <ReportForThisWeek data={weeklyReport} />
            </Col>
          </Row>
        </Col>
        <Col xxl={{ span: 6, order: 1 }} lg={6}>
          <ProjectLocation data={markers} />
        </Col>
        <Col xxl={6} lg={6}>
          <ProjectStatistics
            progressBar={progressBar}
            projectsTable={projectsTable}
            projectUsers={projectUsers}
          />
        </Col>
        <Col xxl={{ span: 6, order: 1 }} lg={6}>
          <RecentActivity data={recentActivities} />
        </Col>
        <Col xxl={{ span: 4, order: 3 }} lg={6}>
          <MembersActivity data={membersActivities} />
        </Col>
        <Col xxl={{ span: 8, order: 2 }}>
          <MemberInfo data={membersInfo} />
        </Col>
        <Col xxl={{ span: 12, order: 3 }}>
          <RunningProjects data={runningProjects} />
        </Col>
      </Row>

      <Row className="g-3">
        <Col xxl={8}>
          <CalendarManagement data={managementEvents} />
        </Col>
        <Col xxl={4}>
          <ToDoList />
        </Col>
      </Row>
    </>
  );
};

export default ProjectManagement;

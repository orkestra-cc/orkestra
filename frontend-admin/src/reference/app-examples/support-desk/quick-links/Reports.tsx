import { reports } from 'data/support-desk/quickLinksData';
import { Card, Col, Row } from 'react-bootstrap';
import Flex from 'components/common/Flex';

interface ReportsItemProps {
  report: any;
}

const ReportsItem = ({ report }: ReportsItemProps) => {
  return (
    <Col lg={6} xxl={4}>
      <Flex
        alignItems="center"
        className="px-4 py-x1 bg-body-tertiary rounded-3 border position-relative"
      >
        <img src={report.img} alt="" width="39" />
        <div className="ms-3 my-x1">
          <h5 className="fs-9 fw-semibold mb-2">
            <a href="#!" className="text-900 hover-primary stretched-link">
              {report.title}
            </a>
          </h5>
          <h6 className="mb-0 text-600">{report.description}</h6>
        </div>
      </Flex>
    </Col>
  );
};

const Reports = () => {
  return (
    <Card>
      <Card.Header className="bg-body-tertiary">
        <h6 className="mb-0">Reports</h6>
      </Card.Header>
      <Card.Body>
        <h5 className="fs-9 mb-2">Analysis of the Helpdesk</h5>
        <Row className="g-3">
          {reports.analysis.map((report: any, index: number) => (
            <ReportsItem key={index} report={report} />
          ))}
        </Row>
        <h5 className="fs-9 mb-2 mt-5">Customer Satisfaction</h5>
        <Row className="g-3">
          {reports.customerSatisfaction.map((report: any, index: number) => (
            <ReportsItem key={index} report={report} />
          ))}
        </Row>
        <h5 className="fs-9 mb-2 mt-5">Productivity</h5>
        <Row className="g-3">
          {reports.productivity.map((report: any, index: number) => (
            <ReportsItem key={index} report={report} />
          ))}
        </Row>
      </Card.Body>
    </Card>
  );
};

export default Reports;

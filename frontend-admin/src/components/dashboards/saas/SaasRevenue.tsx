
import { Card, Col, Row } from 'react-bootstrap';
import SubtleBadge from 'components/common/SubtleBadge';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import CountUp from 'react-countup';

const SaasRevenue: React.FC = () => {
  return (
    <Card className="h-100">
      <Card.Body>
        <Row className="flex-between-center">
          <Col className="d-md-flex d-lg-block flex-between-center">
            <h6 className="mb-md-0 mb-lg-2">Revenue</h6>
            <SubtleBadge bg="success" pill>
              <FontAwesomeIcon icon="caret-up" /> 61.8%
            </SubtleBadge>
          </Col>
          <Col xs="auto">
            <h4 className="fs-6 fw-normal text-700">
              <CountUp
                start={0}
                end={82.18}
                duration={2.75}
                suffix={'M'}
                prefix={'$'}
                decimals={2}
                decimal="."
              />
            </h4>
          </Col>
        </Row>
      </Card.Body>
    </Card>
  );
};

export default SaasRevenue;

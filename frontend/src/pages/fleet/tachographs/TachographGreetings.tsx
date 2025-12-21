
import { Card } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import Background from 'components/common/Background';
import greetingsBg from 'assets/img/illustrations/ticket-greetings-bg.png';

function TachographGreetings() {
  return (
    <Card className="bg-body-tertiary dark__bg-opacity-50 shadow-none h-100">
      <Background image={greetingsBg} className="bg-card d-none d-sm-block" />
      <Card.Header className="d-flex align-items-center z-1 p-0">
        <div className="bg-info rounded-circle p-3 ms-3">
          <FontAwesomeIcon icon="gauge-high" className="text-white fs-5" />
        </div>
        <div className="ms-3">
          <h6 className="mb-1 text-info">Welcome to</h6>
          <h4 className="mb-0 text-info fw-bold">
            Tachograph
            <span className="text-primary fw-medium"> Management</span>
          </h4>
        </div>
      </Card.Header>
    </Card>
  );
}

export default TachographGreetings;

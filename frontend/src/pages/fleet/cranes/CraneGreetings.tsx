
import { Card } from 'react-bootstrap';
import { GiCrane } from 'react-icons/gi';
import Background from 'components/common/Background';
import greetingsBg from 'assets/img/illustrations/ticket-greetings-bg.png';

function CraneGreetings() {
  return (
    <Card className="bg-body-tertiary dark__bg-opacity-50 shadow-none h-100">
      <Background image={greetingsBg} className="bg-card d-none d-sm-block" />
      <Card.Header className="d-flex align-items-center z-1 p-0">
        <div className="bg-warning rounded-circle p-3 ms-3">
          <GiCrane className="text-dark" size={32} />
        </div>
        <div className="ms-3">
          <h6 className="mb-1 text-warning">Welcome to</h6>
          <h4 className="mb-0 text-warning fw-bold">
            Crane
            <span className="text-info fw-medium"> Management</span>
          </h4>
        </div>
      </Card.Header>
    </Card>
  );
}

export default CraneGreetings;

import { Card } from 'react-bootstrap';
import greetingImage from '../../../assets/img/illustrations/ticket-greetings.png';
import greetingsBg from '../../../assets/img/illustrations/ticket-greetings-bg.png';
import Background from 'components/common/Background';

function Greetings() {
  return (
    <Card className="bg-body-tertiary dark__bg-opacity-50 shadow-none h-100">
      <Background image={greetingsBg} className="bg-card d-none d-sm-block" />
      <Card.Header className="d-flex align-items-center z-1 p-0">
        <img src={greetingImage} alt="" width="96" />
        <div className="ms-n3">
          <h6 className="mb-1 text-primary">Welcome to</h6>
          <h4 className="mb-0 text-primary fw-bold">
            Falcon
            <span className="text-info fw-medium"> Support Desk</span>
          </h4>
        </div>
      </Card.Header>
    </Card>
  );
}

export default Greetings;

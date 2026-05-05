import { Card } from 'react-bootstrap';
import { FaRobot } from 'react-icons/fa';
import Background from 'components/common/Background';
import greetingsBg from 'assets/img/illustrations/ticket-greetings-bg.png';

function ModelsGreetings() {
  return (
    <Card className="bg-body-tertiary dark__bg-opacity-50 shadow-none h-100">
      <Background image={greetingsBg} className="bg-card d-none d-sm-block" />
      <Card.Header className="d-flex align-items-center z-1 p-0">
        <div className="bg-primary rounded-circle p-3 ms-3">
          <FaRobot className="text-white" size={32} />
        </div>
        <div className="ms-3">
          <h6 className="mb-1 text-primary">Management</h6>
          <h4 className="mb-0 text-primary fw-bold">
            AI
            <span className="text-info fw-medium"> Models</span>
          </h4>
          <p className="mb-0 mt-1 text-muted small">
            Manage AI model configurations for embedding and language model providers
          </p>
        </div>
      </Card.Header>
    </Card>
  );
}

export default ModelsGreetings;

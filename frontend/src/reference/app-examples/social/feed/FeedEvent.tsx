import { Card, Col, Row, Button } from 'react-bootstrap';
import { Link } from 'react-router';
import Calendar from 'components/common/Calendar';
import Flex from 'components/common/Flex';
import paths from 'routes/paths';

interface FeedEventProps {
  title: string;
  calender: any;
  author: string;
  regFee: string;
  eventImg?: string;
}

const FeedEvent = ({ title, calender, author, regFee, eventImg }: FeedEventProps) => (
  <Card className="p-0 shadow-none">
    {!!eventImg && <img className="card-img-top" src={eventImg} alt="" />}
    <Card.Body className="overflow-hidden">
      <Row className="flex-center">
        <Col>
          <Flex>
            <Calendar {...calender} />
            <div className="fs-10 ms-2">
              <h5 className="fs-9 text-capitalize">
                <Link to={paths.eventDetail}>{title}</Link>
              </h5>
              <p className="mb-0 text-capitalize">
                by <a href="#!">{author}</a>
              </p>
              <span className="fs-9 text-warning fw-semibold">{regFee}</span>
            </div>
          </Flex>
        </Col>
        <Col md="auto" className="d-none d-md-block">
          <Button variant="falcon-default" size="sm" className="px-4">
            Register
          </Button>
        </Col>
      </Row>
    </Card.Body>
  </Card>
);

export default FeedEvent;

import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import type { IconProp } from '@fortawesome/fontawesome-svg-core';
import Background from 'components/common/Background';
import Flex from 'components/common/Flex';

import { Card, Col, Row } from 'react-bootstrap';
import { Link } from 'react-router';
import corner1 from 'assets/img/icons/spot-illustrations/corner-3.png';

interface GreetingItemData {
  icon: IconProp;
  color: string;
  title: string;
  text: string;
}

interface GreetingsProps {
  data: GreetingItemData[];
}

const Greetings = ({ data }: GreetingsProps) => {
  return (
    <Card className="h-100">
      <Background image={corner1} className="rounded-soft bg-card" />
      <Card.Header className="z-1">
        <h5 className="text-primary">Welcome to Orkestra!</h5>
        <h6 className="text-600">Here are some quick links for you to start</h6>
      </Card.Header>
      <Card.Body className="z-1">
        <Row className="g-2 h-100 align-items-end">
          {data.map(({ icon, color, title, text }: GreetingItemData) => {
            return (
              <Col sm={6} md={5} key={title}>
                <Flex className="position-relative">
                  <div className="icon-item icon-item-sm border rounded-3 shadow-none me-2">
                    <FontAwesomeIcon icon={icon} className={`text-${color}`} />
                  </div>
                  <div className="flex-1">
                    <Link to="#!" className="stretched-link text-800">
                      <h6 className="mb-0">{title}</h6>
                    </Link>
                    <p className="mb-0 fs-11 text-500 ">{text}</p>
                  </div>
                </Flex>
              </Col>
            );
          })}
        </Row>
      </Card.Body>
    </Card>
  );
};

export default Greetings;

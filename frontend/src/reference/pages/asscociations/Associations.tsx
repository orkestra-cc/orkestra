import Association from 'reference/pages/asscociations/Association';

import { Card, Col, Row } from 'react-bootstrap';
import associationsData from 'data/associations';

interface AssociationType {
  image: string;
  title: string;
  description: string;
  to?: string;
}

interface AssociationsProps {
  associations?: AssociationType[];
  colBreakpoints?: { sm: number; md: number };
  [key: string]: any;
}

const Associations: React.FC<AssociationsProps> = ({
  associations = associationsData,
  colBreakpoints = { sm: 6, md: 4 },
  ...rest
}) => {
  return (
    <Card {...rest}>
      <Card.Header className="bg-body-tertiary">
        <h5 className="mb-0">Associations</h5>
      </Card.Header>
      <Card.Body className="fs-10">
        <Row className="g-3">
          {associations.map((association: AssociationType) => (
            <Col key={association.title} {...colBreakpoints}>
              <Association
                image={association.image}
                title={association.title}
                description={association.description}
              />
            </Col>
          ))}
        </Row>
      </Card.Body>
    </Card>
  );
};

export default Associations;

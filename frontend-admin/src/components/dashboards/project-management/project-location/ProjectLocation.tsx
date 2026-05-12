import { Card, Col, Form, Row } from 'react-bootstrap';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import CardDropdown from 'components/common/CardDropdown';
import IconButton from 'components/common/IconButton';
import LeafletMap from './LeafletMap';
import 'leaflet/dist/leaflet.css';

interface MarkerData {
  id: string | number;
  lat: number;
  long: number;
  name: string;
  street: string;
  location: string;
}

interface ProjectLocationProps {
  data: MarkerData[];
}

const ProjectLocation = ({ data }: ProjectLocationProps) => {
  return (
    <Card className="h-100">
      <OrkestraCardHeader
        title="Project Locations"
        titleTag="h5"
        endEl={<CardDropdown />}
        light={true}
      />
      <Card.Body className="h-100 p-0" dir="ltr">
        <LeafletMap
          data={data}
          className="h-100 bg-body-tertiary"
          style={{ minHeight: '300px' }}
        />
      </Card.Body>
      <Card.Footer className="bg-body-tertiary">
        <Row className="justify-content-between">
          <Col xs="auto">
            <Form.Select size="sm" defaultValue="week">
              <option value="week">Last 7 days</option>
              <option value="month">Last month</option>
              <option value="year">Last Year</option>
            </Form.Select>
          </Col>
          <Col xs="auto">
            <IconButton
              variant="orkestra-default"
              size="sm"
              icon="chevron-right"
              iconClassName="ms-1 fs-10"
              iconAlign="right"
            >
              <span className="d-none d-sm-inline-block">
                Location overview
              </span>
            </IconButton>
          </Col>
        </Row>
      </Card.Footer>
    </Card>
  );
};

export default ProjectLocation;

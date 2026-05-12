import { Card, CardProps, Col, Row } from 'react-bootstrap';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import CardDropdown from 'components/common/CardDropdown';
import weatherIcon from 'assets/img/icons/weather-icon.png';
import Flex from 'components/common/Flex';

interface WeatherData {
  city: string;
  condition: string;
  precipitation: string;
  temperature: number;
  highestTemperature: number;
  lowestTemperature: number;
}

interface WeatherProps extends CardProps {
  data: WeatherData;
}

const Weather = ({
  data: {
    city,
    condition,
    precipitation,
    temperature,
    highestTemperature,
    lowestTemperature
  },
  ...rest
}: WeatherProps) => (
  <Card {...rest} className="h-100">
    <OrkestraCardHeader
      title="Weather"
      light={false}
      titleTag="h6"
      className="pb-0"
      endEl={<CardDropdown />}
    />
    <Card.Body className="pt-2">
      <Row className="g-0 h-100 align-items-center">
        <Col as={Flex} alignItems="center">
          <img className="me-3" src={weatherIcon} alt="" height="60" />
          <div>
            <h6 className="mb-2">{city}</h6>
            <div className="fs-11 fw-semibold">
              <div className="text-warning">{condition}</div>
              Precipitation: {precipitation}
            </div>
          </div>
        </Col>
        <Col xs="auto" className="text-center ps-2">
          <div className="fs-5 fw-normal font-sans-serif text-primary mb-1 lh-1">
            {temperature}°
          </div>
          <div className="fs-10 text-800">
            {highestTemperature}° / {lowestTemperature}°
          </div>
        </Col>
      </Row>
    </Card.Body>
  </Card>
);

export default Weather;

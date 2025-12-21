
import { Row, Col } from 'react-bootstrap';
import className from 'classnames';
import serviceList from 'data/feature/serviceList';
import Section from 'components/common/Section';
import CardService from './CardService';
import SectionHeader from './SectionHeader';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

interface Service {
  icon: IconProp;
  color: string;
  title: string;
  description: string;
}

const Services: React.FC = () => (
  <Section className="text-center bg-body-tertiary dark__bg-opacity-50">
    <SectionHeader
      title="Here's what's in it for you"
      subtitle="Things you will get right out of the box with Falcon."
    />
    <Row className="mt-6">
      {serviceList.map((service: Service, index: number) => (
        <Col
          lg={4}
          className={className({ 'mt-6 mt-lg-0': index > 0 })}
          key={index}
        >
          <CardService {...service} />
        </Col>
      ))}
    </Row>
  </Section>
);

export default Services;

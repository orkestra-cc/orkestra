
import partnerList from 'data/partner/partnerList';
import Section from 'components/common/Section';
import { Row, Col, Image, ImageProps } from 'react-bootstrap';

const Partner: React.FC<ImageProps> = props => (
  <Col xs={3} sm="auto" className="my-1 my-sm-3 px-x1">
    <Image className="landing-cta-img" {...props} alt="Partner" />
  </Col>
);

interface PartnerData {
  src: string;
  [key: string]: any;
}

const Partners: React.FC = () => {
  return (
    <Section className="py-3 shadow-sm bg-body-tertiary">
      <Row className="flex-center">
        {partnerList.map((partner: PartnerData, index: number) => (
          <Partner key={index} {...partner} />
        ))}
      </Row>
    </Section>
  );
};

export default Partners;

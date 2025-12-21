
import { Card, Col, Row, Spinner } from 'react-bootstrap';
import PricingAltHeader from './PricingAltHeader';
import { Link } from 'react-router';
import useFakeFetch from 'hooks/ui/useFakeFetch';
import PricingAltCard from './PricingAltCard';
import FaqAlt from 'reference/pages/faq/faq-alt/FaqAlt';
import { pricingAltData as pricingData } from 'data/pricing';

const PricingAlt: React.FC = () => {
  const { loading: pricingLoading, data: pricing } = useFakeFetch(
    pricingData,
    1000
  );
  return (
    <>
      <Card className="mb-3">
        <Card.Body>
          <Row className="justify-content-center">
            <PricingAltHeader />
            <Col xs={12} lg={9} xl={10} xxl={8}>
              <Row>
                {pricingLoading ? (
                  <Col xs={12}>
                    <Spinner
                      className="position-absolute start-50"
                      animation="grow"
                    />
                  </Col>
                ) : (
                  (pricing as any[]).map((item: any) => (
                    <PricingAltCard key={item.id} pricing={item} />
                  ))
                )}
              </Row>
            </Col>
            <Col xs={12} className="text-center">
              <h5 className="mt-5">
                Looking for personal or small team task management?
              </h5>
              <p className="fs-8">
                Try the <Link to="#!">basic version</Link> of Falcon
              </p>
            </Col>
          </Row>
        </Card.Body>
      </Card>
      <FaqAlt />
    </>
  );
};

export default PricingAlt;

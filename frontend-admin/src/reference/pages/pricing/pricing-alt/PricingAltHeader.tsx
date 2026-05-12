
import { Col, Form, FormCheck } from 'react-bootstrap';
import Flex from 'components/common/Flex';

const PricingAltHeader: React.FC = () => {
  return (
    <Col xs={12} className="text-center mb-4">
      <div className="fs-8">Orkestra Pricing</div>
      <h3 className="fs-7 fs-md-6">
        Free plan with all the basic features.{' '}
        <br className="d-none d-md-block" /> Pro plan for advanced users.
      </h3>
      <Flex justifyContent="center">
        <FormCheck.Label htmlFor="custom-switch" className="me-2">
          Monthly
        </FormCheck.Label>
        <Form.Check type="switch">
          <Form.Check.Input
            type="checkbox"
            className="orkestra-form-check-input"
            defaultChecked
          />
        </Form.Check>
        <Form.Check.Label className="ms-2 align-top">Yearly</Form.Check.Label>
      </Flex>
    </Col>
  );
};

export default PricingAltHeader;

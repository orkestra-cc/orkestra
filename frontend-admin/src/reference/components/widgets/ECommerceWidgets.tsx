
// E-commerce components are not available
// import BillingCard from 'features/e-commerce/billing/BillingCard';
// import ShoppingCart from 'features/e-commerce/cart/ShoppingCart';
// import OrderSummary from 'features/e-commerce/checkout/OrderSummary';
// import BestSellingProducts from 'components/dashboards/e-commerce/BestSellingProducts';
import { Card, Col, Row } from 'react-bootstrap';
import WidgetSectionTitle from './WidgetSectionTitle';

const ECommerceWidgets = () => {
  return (
    <>
      <WidgetSectionTitle
        icon="cart-plus"
        title="E-commerce"
        subtitle="Find more cards which are dedicatedly made for E-commerce."
        transform="shrink-4"
        className="mb-4 mt-6"
      />

      <Row className="g-3 mb-3">
        <Col xs={12}>
          <Card>
            <Card.Body>
              <p className="text-muted">E-commerce components are not available in this version.</p>
            </Card.Body>
          </Card>
        </Col>
      </Row>
    </>
  );
};

export default ECommerceWidgets;

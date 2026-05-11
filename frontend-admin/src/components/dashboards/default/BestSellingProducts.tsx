import classNames from 'classnames';
import Flex from 'components/common/Flex';

import {
  Button,
  Card,
  Col,
  Form,
  ProgressBar,
  Row,
  Table
} from 'react-bootstrap';
import { Link } from 'react-router';

interface Product {
  id: string | number;
  img: string;
  title: string;
  type: string;
  unit: number;
  price: number;
}

interface BestSellingProductProps {
  product: Product;
  totalPrice: number;
  isLast: boolean;
}

interface BestSellingProductsProps {
  products: Product[];
}

const getTotalPrice = (items: Product[]) =>
  items
    .map(({ unit, price }) => unit * price)
    .reduce((total, currentValue) => total + currentValue, 0);

const getProductItemCalculatedData = (unit: number, price: number, totalPrice: number) => {
  const productTotalPrice = unit * price;
  const percentage = ((productTotalPrice * 100) / totalPrice).toFixed(0);
  return { productTotalPrice, percentage };
};

const BestSellingProduct = ({ product, totalPrice, isLast }: BestSellingProductProps) => {
  const { img, title, type, unit, price } = product;
  const { productTotalPrice, percentage } = getProductItemCalculatedData(
    unit,
    price,
    totalPrice
  );

  return (
    <tr className={classNames({ 'border-bottom border-200': !isLast })}>
      <td>
        <Flex alignItems="center" className="position-relative">
          <img
            className="rounded-1 border border-200"
            src={img}
            width="60"
            alt={title}
          />
          <div className="ms-3">
            <h6 className="mb-1 fw-semibold">
              <Link className="text-1100 stretched-link" to="#!">
                {title}
              </Link>
            </h6>
            <p className="fw-semibold mb-0 text-500">{type}</p>
          </div>
        </Flex>
      </td>
      <td className="align-middle text-end fw-semibold">
        ${productTotalPrice}
      </td>
      <td className="align-middle pe-x1">
        <Flex alignItems="center">
          <ProgressBar now={Number(percentage)} style={{ width: '80px', height: 5 }} />
          <div className="fw-semibold ms-3">{percentage}%</div>
        </Flex>
      </td>
    </tr>
  );
};

const BestSellingProducts = ({ products }: BestSellingProductsProps) => {
  const totalPrice = getTotalPrice(products);
  const noOfProducts = products.length;

  return (
    <Card className="h-lg-100 overflow-hidden">
      <Card.Body className="p-0 ">
        <Table borderless responsive className="mb-0 fs-10">
          <thead className="bg-body-tertiary">
            <tr>
              <th className="text-900">Best Selling Products</th>
              <th className="text-900 text-end">Revenue ( ${totalPrice})</th>
              <th className="pe-x1 text-900 text-end" style={{ width: '8rem' }}>
                Revenue (%)
              </th>
            </tr>
          </thead>
          <tbody>
            {products.map((product, index) => (
              <BestSellingProduct
                product={product}
                totalPrice={totalPrice}
                isLast={index === noOfProducts - 1}
                key={product.id}
              />
            ))}
          </tbody>
        </Table>
      </Card.Body>
      <Card.Footer className="bg-body-tertiary py-2">
        <Row className="flex-between-center">
          <Col xs="auto">
            <Form.Select size="sm" className="me-2">
              <option>Last 7 days</option>
              <option>Last Month</option>
              <option>Last Year</option>
            </Form.Select>
          </Col>
          <Col xs="auto">
            <Button variant="falcon-default" size="sm" as="a" href="#!">
              View All
            </Button>
          </Col>
        </Row>
      </Card.Footer>
    </Card>
  );
};

export default BestSellingProducts;

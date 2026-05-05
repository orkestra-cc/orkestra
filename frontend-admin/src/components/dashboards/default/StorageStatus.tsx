
import classNames from 'classnames';
import Flex from 'components/common/Flex';
import { Card, Col, ProgressBar, Row } from 'react-bootstrap';

interface StorageItem {
  name: string;
  size: number;
  color: string;
}

interface StorageStatusProps {
  data: StorageItem[];
  className?: string;
}

const StorageStatus = ({ data, className }: StorageStatusProps) => {
  const totalStorage = data
    .map((d: StorageItem) => d.size)
    .reduce((total: number, currentValue: number) => total + currentValue, 0);
  const freeStorage = data.find((d: StorageItem) => d.name === 'Free')?.size || 0;

  return (
    <Card className={className}>
      <Card.Body as={Flex} alignItems="center">
        <div className="w-100">
          <h6 className="mb-3 text-800">
            Using Storage{' '}
            <strong className="text-1100">
              {totalStorage - freeStorage} MB{' '}
            </strong>
            of {Math.round(totalStorage / 1024)} GB
          </h6>
          <ProgressBar
            style={{ height: 10 }}
            className="shadow-none rounded-4 mb-3"
          >
            {data.map((status: StorageItem, index: number) => (
              <ProgressBar
                // variant={status.color}
                variant={`${status.color.split('-')[1] || status.color}`}
                now={(status.size * 100) / totalStorage}
                key={status.name}
                className={classNames('overflow-visible position-relative', {
                  'rounded-end rounded-pill': index === 0,
                  'rounded-start rounded-pill': index === data.length - 1,
                  'border-end border-100 border-2': index !== data.length - 1,
                  'rounded-0': index !== data.length - 1 && index !== 0
                })}
              />
            ))}
          </ProgressBar>
          <Row className="fs-10 fw-semibold text-500">
            {data.map((status: StorageItem) => (
              <Col
                xs={6}
                sm="auto"
                as={Flex}
                alignItems="center"
                className="pe-2"
                key={status.name}
              >
                <span
                  className={`dot bg-${
                    status.color === 'gradient' ? 'primary' : status.color
                  }`}
                ></span>

                <span>{status.name}</span>
                <span className="d-none d-md-inline-block d-lg-none d-xxl-inline-block ms-1">
                  ({status.size}MB)
                </span>
              </Col>
            ))}
          </Row>
        </div>
      </Card.Body>
    </Card>
  );
};

export default StorageStatus;

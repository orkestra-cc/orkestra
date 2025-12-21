
import { Col, Button } from 'react-bootstrap';
import classNames from 'classnames';
import { Link } from 'react-router';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import SubtleBadge from 'components/common/SubtleBadge';

// Types for Pricing Default Card
interface PricingFeature {
  id: number;
  title: string;
  tag?: {
    label: string;
    type: string;
  };
}

interface PricingPlan {
  title: string;
  subTitle?: string;
  price: number;
  url: string;
  buttonText: string;
  isFeatured: boolean;
  featureTitle: string;
  features: PricingFeature[];
}

interface PricingDefaultCardProps {
  pricing: PricingPlan;
}

const PricingDefaultCard: React.FC<PricingDefaultCardProps> = ({
  pricing: {
    title,
    subTitle,
    price,
    url,
    buttonText,
    isFeatured,
    featureTitle,
    features
  }
}) => {
  return (
    <Col
      lg={4}
      className={classNames('border-top border-bottom', {
        'dark__bg-1000 px-4 px-lg-0': isFeatured
      })}
      style={{ backgroundColor: isFeatured ? 'rgba(115, 255, 236, 0.18)' : undefined }}
    >
      <div className="h100">
        <div className="text-center p-4">
          <h3 className="fw-normal my-0">{title}</h3>
          <p className="mt-3">{subTitle}</p>
          <h2 className="fw-medium my-4">
            <sup className="fw-normal fs-7 me-1">$</sup>
            {price}
            <small className="fs-10 text-700">/ year</small>
          </h2>
          <Button
            as={Link as any}
            variant={isFeatured ? 'primary' : 'outline-primary'}
            to={url}
          >
            {buttonText}
          </Button>
        </div>
        <hr className="border-bottom-0 m-0" />
        <div className="text-start px-sm-4 py-4">
          <h5 className="fw-medium fs-9">{featureTitle}</h5>
          <ul className="list-unstyled mt-3">
            {features.map((feature: PricingFeature) => (
              <li className="py-1" key={feature.id}>
                <FontAwesomeIcon icon="check" className="me-2 text-success" />{' '}
                {feature.title}{' '}
                {feature.tag && (
                  <SubtleBadge pill bg={feature.tag.type as any}>
                    {feature.tag.label}
                  </SubtleBadge>
                )}
              </li>
            ))}
          </ul>
          <Link to="#!" className="btn btn-link">
            More about {title}
          </Link>
        </div>
      </div>
    </Col>
  );
};

export default PricingDefaultCard;

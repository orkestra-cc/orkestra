
import { Alert, Card } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import IconButton from 'components/common/IconButton';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

// TypeScript interfaces
interface MoreItem {
  id: number;
  label: string;
  icon: IconProp;
}

interface DoMoreCardProps extends React.ComponentProps<typeof Card> {
  // Additional props if needed
}

const moreItems: MoreItem[] = [
  {
    id: 1,
    label: 'Unlimited downloads',
    icon: 'infinity'
  },
  {
    id: 2,
    label: 'Commercial use',
    icon: 'check'
  },
  { id: 3, label: '100% moneyback guarantee', icon: 'check' },
  {
    id: 4,
    label: 'Lifetime free updates',
    icon: 'check'
  }
];

const DoMoreCard: React.FC<DoMoreCardProps> = ({ ...rest }) => {
  return (
    <Card {...rest}>
      <Card.Body className="p-4">
        <h4 className="text-800">More with Falcon</h4>
        <h5 className="text-600 fs-9">
          Get <span className="text-primary fw-semibold">70% OFF </span> with
          Falcon+
        </h5>
        <Alert variant="success" className="mt-3">
          <h3 className="mb-0 text-800">
            $29
            <span className="fs-9 fw-medium font-sans-serif text-600">
              /month
            </span>
          </h3>
        </Alert>
        <ul className="fa-ul ms-2 ps-x1 mb-2">
          {moreItems.map(item => (
            <li key={item.id} className="py-1">
              <h6 className="text-700">
                <FontAwesomeIcon
                  icon={item.icon}
                  className="fa-li text-success"
                />
                {item.label}
              </h6>
            </li>
          ))}
        </ul>
        <IconButton
          icon="crown"
          variant="success"
          iconClassName="me-2"
          className="w-100"
        >
          Upgrade to Falcon+
        </IconButton>
      </Card.Body>
    </Card>
  );
};

export default DoMoreCard;

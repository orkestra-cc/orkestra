
import className from 'classnames';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Card } from 'react-bootstrap';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

// Types for Card Service component
interface MediaProps {
  icon: IconProp;
  color?: string;
  className?: string;
}

interface CardServiceProps {
  media?: MediaProps;
  icon?: IconProp;
  color?: string;
  title: string;
  description?: string;
  children?: React.ReactNode;
}

const CardService: React.FC<CardServiceProps> = ({ media, icon, color, title, description, children }) => {
  const iconProp = media?.icon || icon;
  const iconColor = media?.color || color;
  const iconClassName = media?.className;

  return (
    <Card className="card-span h-100">
      <div className="card-span-img">
        {iconProp && (
          <FontAwesomeIcon
            icon={iconProp}
            className={className(
              { [`text-${iconColor}`]: iconColor },
              iconClassName
            )}
          />
        )}
      </div>
      <Card.Body className="pt-6 pb-4">
        <h5 className="mb-2">{title}</h5>
        {description && <p>{description}</p>}
        {children}
      </Card.Body>
    </Card>
  );
};

export default CardService;

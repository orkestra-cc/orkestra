import { Link } from 'react-router-dom';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import classNames from 'classnames';

interface FalconLinkProps {
  to?: string;
  icon?: IconProp;
  title: string;
  className?: string;
}

const FalconLink = ({
  to = '#!',
  icon = 'chevron-right',
  title,
  className
}: FalconLinkProps) => {
  return (
    <Link to={to} className={classNames('btn btn-link btn-sm p-0', className)}>
      {title}
      <FontAwesomeIcon icon={icon as IconProp} className="ms-1 fs-11" />
    </Link>
  );
};

export default FalconLink;

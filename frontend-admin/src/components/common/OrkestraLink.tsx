import { Link } from 'react-router-dom';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import classNames from 'classnames';

interface OrkestraLinkProps {
  to?: string;
  icon?: IconProp;
  title: string;
  className?: string;
}

const OrkestraLink = ({
  to = '#!',
  icon = 'chevron-right',
  title,
  className
}: OrkestraLinkProps) => {
  return (
    <Link to={to} className={classNames('btn btn-link btn-sm p-0', className)}>
      {title}
      <FontAwesomeIcon icon={icon as IconProp} className="ms-1 fs-11" />
    </Link>
  );
};

export default OrkestraLink;

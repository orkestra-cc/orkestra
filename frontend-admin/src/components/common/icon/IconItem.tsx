import { ElementType, MouseEventHandler } from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp, Transform } from '@fortawesome/fontawesome-svg-core';
import classNames from 'classnames';

interface IconItemProps {
  tag?: ElementType;
  icon: IconProp;
  bg?: string;
  size?: 'sm' | 'lg' | 'xl' | '2xl';
  color?: string;
  className?: string;
  transform?: string | Transform;
  iconClass?: string;
  onClick?: MouseEventHandler;
  href?: string;
}

const IconItem = ({
  tag: Tag = 'a',
  icon,
  bg,
  size,
  color,
  className,
  transform,
  iconClass,
  onClick,
  ...rest
}: IconItemProps) => (
  <Tag
    className={classNames(className, 'icon-item', {
      [`icon-item-${size}`]: size,
      [`bg-${bg}`]: bg,
      [`text-${color}`]: color
    })}
    {...rest}
    onClick={onClick}
  >
    <FontAwesomeIcon icon={icon} transform={transform} className={iconClass} />
  </Tag>
);

export default IconItem;

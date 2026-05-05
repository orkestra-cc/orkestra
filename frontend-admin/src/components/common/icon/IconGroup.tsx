import { HTMLAttributes } from 'react';
import classNames from 'classnames';
import IconItem from './IconItem';
import { IconProp, Transform } from '@fortawesome/fontawesome-svg-core';

interface IconGroupIcon {
  icon: IconProp;
  bg?: string;
  size?: 'sm' | 'lg' | 'xl' | '2xl';
  color?: string;
  transform?: string | Transform;
  iconClass?: string;
  href?: string;
}

interface IconGroupProps extends HTMLAttributes<HTMLDivElement> {
  icons: IconGroupIcon[];
  className?: string;
}

const IconGroup = ({ icons, className, ...rest }: IconGroupProps) => (
  <div className={classNames('icon-group', className)} {...rest}>
    {icons.map((icon, index) => (
      <IconItem {...icon} key={index} />
    ))}
  </div>
);

export default IconGroup;

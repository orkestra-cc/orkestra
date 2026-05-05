import classNames from 'classnames';
import { ReactNode } from 'react';

interface HoverboxProps {
  children: ReactNode;
  className?: string;
}

const Hoverbox = ({ children, className }: HoverboxProps) => {
  return <div className={classNames('hoverbox', className)}>{children}</div>;
};

export const HoverboxContent = ({ children, className }: HoverboxProps) => {
  return (
    <div className={classNames('hoverbox-content', className)}>{children}</div>
  );
};

Hoverbox.Content = HoverboxContent;

export default Hoverbox;

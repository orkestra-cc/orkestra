import Background from 'components/common/Background';
import React, { ReactNode } from 'react';
import { Card } from 'react-bootstrap';

interface VehicleProfileBannerProps {
  children?: ReactNode;
}

interface HeaderProps {
  coverSrc: string;
  className?: string;
  children?: ReactNode;
}

interface BodyProps {
  children: ReactNode;
}

const VehicleProfileBanner: React.FC<VehicleProfileBannerProps> & {
  Header: React.FC<HeaderProps>;
  Body: React.FC<BodyProps>;
} = ({ children }) => {
  return <Card className="mb-3">{children}</Card>;
};

const Header: React.FC<HeaderProps> = ({ coverSrc, className = '', children }) => {
  return (
    <Card.Header
      className={`position-relative min-vh-25 ${className}`}
      style={{ height: '180px' }}
    >
      <Background image={coverSrc} className="rounded-3 rounded-bottom-0" />
      {children}
    </Card.Header>
  );
};

const Body: React.FC<BodyProps> = ({ children }) => {
  return (
    <Card.Body>
      {children}
    </Card.Body>
  );
};

VehicleProfileBanner.Header = Header;
VehicleProfileBanner.Body = Body;

export default VehicleProfileBanner;
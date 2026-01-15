import Background from 'components/common/Background';
import React, { ReactNode } from 'react';
import { Card } from 'react-bootstrap';

interface TachographProfileBannerProps {
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

const TachographProfileBanner: React.FC<TachographProfileBannerProps> & {
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

TachographProfileBanner.Header = Header;
TachographProfileBanner.Body = Body;

export default TachographProfileBanner;
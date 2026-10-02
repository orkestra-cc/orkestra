import { type PropsWithChildren } from 'react';
import { Col, Row } from 'react-bootstrap';
import Logo from 'components/common/Logo';
import { Outlet } from 'react-router';

import Section from 'components/common/Section';

const ErrorLayout: React.FC<PropsWithChildren> = ({ children }) => {
  return (
    <Section className="py-0">
      <Row className="flex-center min-vh-100 py-6">
        <Col sm={11} md={9} lg={7} xl={6} className="col-xxl-5">
          <Logo />
          {children ?? <Outlet />}
        </Col>
      </Row>
    </Section>
  );
};

export default ErrorLayout;


import { Link } from 'react-router';
import { Card, Col, Row } from 'react-bootstrap';
import Background from 'components/common/Background';
import Flex from 'components/common/Flex';
import Section from 'components/common/Section';

import bgShape from 'assets/img/illustrations/bg-shape.png';
import shape1 from 'assets/img/illustrations/shape-1.png';
import halfCircle from 'assets/img/illustrations/half-circle.png';

interface AuthCardLayoutProps {
  leftSideContent?: React.ReactNode;
  children: React.ReactNode;
  footer?: boolean;
}

const AuthCardLayout: React.FC<AuthCardLayoutProps> = ({
  leftSideContent,
  children,
  footer = true
}) => {
  return (
    <Section fluid className="py-0">
      <Row className="g-0 min-vh-100 flex-center">
        <Col lg={8} xxl={8} className="py-3 position-relative">
          <img
            className="bg-auth-circle-shape"
            src={bgShape}
            alt=""
            width="250"
          />
          <img
            className="bg-auth-circle-shape-2"
            src={shape1}
            alt=""
            width="150"
          />
          <Card className="overflow-hidden z-1">
            <Card.Body className="p-0">
              <Row className="h-100 g-0">
                <Col md={5} className="text-white text-center bg-card-gradient">
                  <div className="position-relative p-4 pt-md-5 pb-md-7">
                    <Background
                      image={halfCircle}
                      className="bg-auth-card-shape"
                    />
                    <div
                      className="z-1 position-relative"
                      data-bs-theme="light"
                    >
                      <Link
                        className="link-light mb-4 font-sans-serif fw-bolder fs-5 d-inline-block"
                        to="/"
                      >
                        ORKESTRA
                      </Link>
                      <p className="text-white">
                        Accesso riservato: questo gestionale è destinato
                        esclusivamente ad uso interno.
                      </p>
                      <p className="opacity-75 text-white">
                        Ogni utilizzo non autorizzato sarà considerato una
                        violazione e potrà comportare responsabilità civili e
                        penali ai sensi della normativa vigente
                      </p>
                    </div>
                  </div>
                  <div
                    className="mt-3 mb-4 mt-md-4 mb-md-5"
                    data-bs-theme="light"
                  >
                    {leftSideContent}

                    {footer && (
                      <p className="mb-0 mt-4 mt-md-5 fs-10 fw-semibold text-white opacity-75">
                        Leggi{' '}
                        <Link
                          className="text-decoration-underline text-white"
                          to="#!"
                        >
                          i termini
                        </Link>{' '}
                        e{' '}
                        <Link
                          className="text-decoration-underline text-white"
                          to="#!"
                        >
                          le condizioni{' '}
                        </Link>
                      </p>
                    )}
                  </div>
                </Col>
                <Col
                  md={7}
                  as={Flex}
                  alignItems="center"
                  justifyContent="center"
                >
                  <div className="p-4 p-md-5 flex-grow-1">{children}</div>
                </Col>
              </Row>
            </Card.Body>
          </Card>
        </Col>
      </Row>
    </Section>
  );
};

export default AuthCardLayout;

import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import coverSrc from 'assets/img/generic/bg-1.jpg';
import Flex from 'components/common/Flex';

import { Col, Row, Badge } from 'react-bootstrap';
import ProfileBanner from './CraneProfileBanner';
import { CraneResponse } from 'store/api/craneApi';
import {
  FaTruck,
  FaExclamationTriangle
} from 'react-icons/fa';
import { GiCrane } from 'react-icons/gi';

interface CraneBannerProps {
  crane: CraneResponse;
}

const CraneBanner: React.FC<CraneBannerProps> = ({ crane }) => {
  // Helper function to format date
  const formatDate = (dateString?: string) => {
    if (!dateString) return 'N/A';
    return new Date(dateString).toLocaleDateString('en-GB', {
      year: 'numeric',
      month: 'long',
      day: 'numeric'
    });
  };

  // Helper function to calculate days until verification
  const getDaysUntilVerification = (date?: string) => {
    if (!date) return null;
    const verificationDate = new Date(date);
    const today = new Date();
    const diffTime = verificationDate.getTime() - today.getTime();
    const diffDays = Math.ceil(diffTime / (1000 * 60 * 60 * 24));
    return diffDays;
  };

  const daysUntilVerification = getDaysUntilVerification(
    crane.scadenzaVerifica
  );

  return (
    <ProfileBanner>
      <ProfileBanner.Header coverSrc={coverSrc}>
        <div className="position-absolute bottom-0 start-0 p-3 d-flex align-items-center">
          <div
            className="avatar-5xl rounded-circle bg-white d-flex align-items-center justify-content-center shadow-sm"
            style={{ width: '120px', height: '120px' }}
          >
            <GiCrane className="text-warning" size={60} />
          </div>
        </div>
      </ProfileBanner.Header>
      <ProfileBanner.Body>
        <Row className="mt-4">
          <Col lg={8}>
            <Flex alignItems="center" className="mb-2">
              <h4 className="mb-0 me-2">{crane.nome}</h4>
              <Badge
                bg={crane.isActive ? 'success' : 'secondary'}
                className="ms-2"
              >
                {crane.isActive ? 'Attiva' : 'Inattiva'}
              </Badge>
            </Flex>

            <div className="fs-10 fw-medium text-500 mb-2">
              <FontAwesomeIcon icon="id-card" className="me-2" />
              Matricola:{' '}
              <span className="text-900 fw-bold">{crane.matricola}</span>
            </div>

            <p className="text-1000 mb-0">
              <Badge bg="soft-warning" className="me-2 text-dark">
                {crane.tipo}
              </Badge>
              {crane.verificareSuMezzo && (
                <>
                  <FaTruck className="me-1 text-muted" />
                  <span className="text-muted">Montata su: </span>
                  <Badge bg="info" className="ms-1">
                    {crane.verificareSuMezzo}
                  </Badge>
                </>
              )}
            </p>
          </Col>
          <Col lg={4} className="text-lg-end">
            <div className="border-start-lg ps-lg-4">
              <div className="mb-3">
                <h6 className="text-uppercase text-600 mb-0">
                  <FontAwesomeIcon icon="calendar-alt" className="me-2" />
                  Scadenza Verifica
                </h6>
                <div className="fs-5 fw-medium text-1000">
                  {formatDate(crane.scadenzaVerifica)}
                </div>
                {daysUntilVerification !== null && (
                  <Badge
                    bg={
                      daysUntilVerification < 0
                        ? 'danger'
                        : daysUntilVerification <= 30
                          ? 'warning'
                          : 'success'
                    }
                    className="mt-1"
                  >
                    {daysUntilVerification < 0 ? (
                      <>
                        <FaExclamationTriangle className="me-1" />
                        Scaduta da {Math.abs(daysUntilVerification)} giorni
                      </>
                    ) : daysUntilVerification === 0 ? (
                      'Scade oggi'
                    ) : daysUntilVerification <= 30 ? (
                      <>
                        <FaExclamationTriangle className="me-1" />
                        {daysUntilVerification} giorni rimanenti
                      </>
                    ) : (
                      `${daysUntilVerification} giorni rimanenti`
                    )}
                  </Badge>
                )}
              </div>

              {crane.note && (
                <div>
                  <h6 className="text-uppercase text-600 mb-0">
                    <FontAwesomeIcon icon="sticky-note" className="me-2" />
                    Note
                  </h6>
                  <div className="fs-10 fw-medium text-1000">
                    {crane.note.length > 50
                      ? crane.note.substring(0, 50) + '...'
                      : crane.note}
                  </div>
                </div>
              )}
            </div>
          </Col>
        </Row>
      </ProfileBanner.Body>
    </ProfileBanner>
  );
};

export default CraneBanner;

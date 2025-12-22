import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import coverSrc from 'assets/img/generic/bg-1.jpg';
import Flex from 'components/common/Flex';

import { Col, Row, Badge } from 'react-bootstrap';
import ProfileBanner from './TachographProfileBanner';
import { TachographResponse } from 'store/api/tachographApi';
import { FaMapMarkerAlt, FaCalendarCheck } from 'react-icons/fa';

interface TachographBannerProps {
  tachograph: TachographResponse;
}

const TachographBanner: React.FC<TachographBannerProps> = ({ tachograph }) => {
  // Helper function to format date
  const formatDate = (dateString?: string) => {
    if (!dateString) return 'N/A';
    return new Date(dateString).toLocaleDateString('en-GB', {
      year: 'numeric',
      month: 'long',
      day: 'numeric'
    });
  };

  // Helper function to calculate days until revision
  const getDaysUntilRevision = (date?: string) => {
    if (!date) return null;
    const revisionDate = new Date(date);
    const today = new Date();
    const diffTime = revisionDate.getTime() - today.getTime();
    const diffDays = Math.ceil(diffTime / (1000 * 60 * 60 * 24));
    return diffDays;
  };

  const daysUntilRevision = getDaysUntilRevision(tachograph.scadenzaRevisione);

  return (
    <ProfileBanner>
      <ProfileBanner.Header coverSrc={coverSrc}>
        <div className="position-absolute bottom-0 start-0 p-3 d-flex align-items-center">
          <div
            className="avatar-5xl rounded-circle bg-white d-flex align-items-center justify-content-center shadow-sm"
            style={{ width: '120px', height: '120px' }}
          >
            <FontAwesomeIcon
              icon="gauge-high"
              className="text-info"
              style={{ fontSize: '60px' }}
            />
          </div>
        </div>
      </ProfileBanner.Header>
      <ProfileBanner.Body>
        <Row className="mt-4">
          <Col lg={8}>
            <Flex alignItems="center" className="mb-2">
              <h4 className="mb-0 me-2">{tachograph.nome}</h4>
              <Badge
                bg={tachograph.isActive ? 'success' : 'secondary'}
                className="ms-2"
              >
                {tachograph.isActive ? 'Active' : 'Inactive'}
              </Badge>
            </Flex>

            <div className="fs-10 fw-medium text-500 mb-2">
              <FontAwesomeIcon icon="id-card" className="me-2" />
              License Plate:{' '}
              <span className="text-900 fw-bold">{tachograph.targa}</span>
            </div>

            <p className="text-1000 mb-0">
              {tachograph.luogo && (
                <>
                  <FaMapMarkerAlt className="me-1 text-muted" />
                  <span className="text-muted">
                    Location: {tachograph.luogo}
                  </span>
                </>
              )}
            </p>
          </Col>
          <Col lg={4} className="text-lg-end">
            <div className="border-start-lg ps-lg-4">
              <div className="mb-3">
                <h6 className="text-uppercase text-600 mb-0">
                  <FontAwesomeIcon icon="calendar-alt" className="me-2" />
                  Tachograph Expiry
                </h6>
                <div className="fs-5 fw-medium text-1000">
                  {formatDate(tachograph.scadenzaRevisione)}
                </div>
                {daysUntilRevision !== null && (
                  <Badge
                    text={'dark'}
                    bg={
                      daysUntilRevision <= 30
                        ? 'warning'
                        : daysUntilRevision < 0
                          ? 'danger'
                          : 'success'
                    }
                    className="mt-1"
                  >
                    {daysUntilRevision < 0
                      ? `Expired ${Math.abs(daysUntilRevision)} days ago`
                      : daysUntilRevision === 0
                        ? 'Expires today'
                        : `${daysUntilRevision} days remaining`}
                  </Badge>
                )}
              </div>

              {tachograph.revisioneProgrammata && (
                <div>
                  <h6 className="text-uppercase text-600 mb-0">
                    <FaCalendarCheck className="me-2" />
                    Scheduled Inspection
                  </h6>
                  <div className="fs-5 fw-medium text-1000">
                    {formatDate(tachograph.revisioneProgrammata)}
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

export default TachographBanner;

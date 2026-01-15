
import { Card, Row, Col, ProgressBar } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { CraneResponse } from 'store/api/craneApi';
import { FaCheckCircle, FaHistory, FaTruck, FaChartLine, FaClipboardCheck } from 'react-icons/fa';
import { GiCrane } from 'react-icons/gi';

interface CraneStatsProps {
  crane: CraneResponse;
}

// Mock statistics data (in real app, this would come from API)
const mockStats = {
  verificheCompletate: 12,
  verificheScadute: 0,
  giorniDaUltimaVerifica: 45,
  tassoConformita: 100,
  oreUtilizzo: 850,
  sollevamentiEffettuati: 3420,
  pesoMedioSollevato: 2.5, // tonnellate
  pesoMassimoSollevato: 8.2, // tonnellate
  utilizzoMensile: {
    percentage: 68,
    days: 20
  }
};

const CraneStats: React.FC<CraneStatsProps> = ({ crane }) => {
  const formatNumber = (num: number) => {
    return num.toLocaleString('en-GB');
  };

  // Calculate days until verification
  const getDaysUntilVerification = () => {
    if (!crane.scadenzaVerifica) return null;
    const verificationDate = new Date(crane.scadenzaVerifica);
    const today = new Date();
    const diffTime = verificationDate.getTime() - today.getTime();
    const diffDays = Math.ceil(diffTime / (1000 * 60 * 60 * 24));
    return diffDays;
  };

  const daysUntilVerification = getDaysUntilVerification();
  const verificationProgress = daysUntilVerification !== null ?
    Math.max(0, Math.min(100, ((365 - Math.abs(daysUntilVerification)) / 365) * 100)) : 0;

  // Determine verification status color
  const getVerificationVariant = () => {
    if (!daysUntilVerification) return 'secondary';
    if (daysUntilVerification < 0) return 'danger';
    if (daysUntilVerification <= 30) return 'warning';
    return 'success';
  };

  return (
    <Card>
      <Card.Header className="bg-body-tertiary">
        <h5 className="mb-0">
          <FaChartLine className="me-2" />
          Statistiche Gru
        </h5>
      </Card.Header>
      <Card.Body>
        {/* Verification Status */}
        <div className="mb-4">
          <h6 className="text-uppercase text-600 mb-3">Stato Verifica</h6>

          <div className="mb-3">
            <div className="d-flex justify-content-between mb-1">
              <small>Prossima verifica</small>
              <small className="text-muted">
                {daysUntilVerification !== null ? (
                  daysUntilVerification < 0 ?
                    `Scaduta da ${Math.abs(daysUntilVerification)} giorni` :
                    `${daysUntilVerification} giorni`
                ) : 'N/D'}
              </small>
            </div>
            <ProgressBar
              variant={getVerificationVariant()}
              now={verificationProgress}
              style={{ height: '8px' }}
            />
          </div>

          <Row className="g-3">
            <Col xs={6}>
              <div className="border-start border-3 border-success ps-3">
                <small className="text-muted d-block">Verifiche OK</small>
                <div className="fs-6 fw-bold text-900">
                  <FaCheckCircle className="text-success me-1" />
                  {mockStats.verificheCompletate}
                </div>
              </div>
            </Col>
            <Col xs={6}>
              <div className="border-start border-3 border-info ps-3">
                <small className="text-muted d-block">Conformità</small>
                <div className="fs-6 fw-bold text-900">
                  <FaClipboardCheck className="text-info me-1" />
                  {mockStats.tassoConformita}%
                </div>
              </div>
            </Col>
          </Row>
        </div>

        {/* Operational Metrics */}
        <div className="mb-4">
          <h6 className="text-uppercase text-600 mb-3">Metriche Operative</h6>

          <Row className="g-3">
            <Col xs={6}>
              <div className="border-start border-3 border-primary ps-3">
                <small className="text-muted d-block">Ore Utilizzo</small>
                <div className="fs-6 fw-bold text-900">
                  <FontAwesomeIcon icon="clock" className="text-primary me-1" />
                  {formatNumber(mockStats.oreUtilizzo)}h
                </div>
              </div>
            </Col>
            <Col xs={6}>
              <div className="border-start border-3 border-warning ps-3">
                <small className="text-muted d-block">Sollevamenti</small>
                <div className="fs-6 fw-bold text-900">
                  <GiCrane className="text-warning me-1" size={14} />
                  {formatNumber(mockStats.sollevamentiEffettuati)}
                </div>
              </div>
            </Col>
            <Col xs={6}>
              <div className="border-start border-3 border-success ps-3">
                <small className="text-muted d-block">Peso Medio</small>
                <div className="fs-6 fw-bold text-900">
                  <FontAwesomeIcon icon="weight" className="text-success me-1" />
                  {mockStats.pesoMedioSollevato}t
                </div>
              </div>
            </Col>
            <Col xs={6}>
              <div className="border-start border-3 border-danger ps-3">
                <small className="text-muted d-block">Peso Max</small>
                <div className="fs-6 fw-bold text-900">
                  <FontAwesomeIcon icon="weight-hanging" className="text-danger me-1" />
                  {mockStats.pesoMassimoSollevato}t
                </div>
              </div>
            </Col>
          </Row>
        </div>

        {/* Utilizzo Mensile */}
        <div className="mb-4">
          <h6 className="text-uppercase text-600 mb-3">Utilizzo Mensile</h6>
          <div className="d-flex justify-content-between mb-1">
            <small>{mockStats.utilizzoMensile.days} giorni attiva</small>
            <small className="text-muted">{mockStats.utilizzoMensile.percentage}%</small>
          </div>
          <ProgressBar
            variant="primary"
            now={mockStats.utilizzoMensile.percentage}
            style={{ height: '10px' }}
          />
        </div>

        {/* Associated Vehicle */}
        {crane.verificareSuMezzo && (
          <div className="border-top pt-3">
            <h6 className="text-uppercase text-600 mb-2">Mezzo Associato</h6>
            <div className="d-flex align-items-center p-2 bg-light rounded">
              <FaTruck className="text-primary me-2" size={20} />
              <div>
                <div className="fw-bold">{crane.verificareSuMezzo}</div>
                <small className="text-muted">Targa del veicolo</small>
              </div>
            </div>
          </div>
        )}

        {/* Last Verification */}
        <div className="border-top pt-3 mt-3">
          <div className="d-flex justify-content-between align-items-center">
            <small className="text-muted">
              <FaHistory className="me-1" />
              Ultima verifica
            </small>
            <small className="text-muted">
              {mockStats.giorniDaUltimaVerifica} giorni fa
            </small>
          </div>
        </div>
      </Card.Body>
    </Card>
  );
};

export default CraneStats;
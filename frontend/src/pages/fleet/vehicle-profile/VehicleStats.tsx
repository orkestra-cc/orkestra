
import { Card, Row, Col, ProgressBar } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { VehicleResponse } from 'store/api/vehicleApi';
import { FaTachometerAlt, FaRoute, FaGasPump, FaTools, FaCalendarAlt, FaChartLine } from 'react-icons/fa';

interface VehicleStatsProps {
  vehicle: VehicleResponse;
}

// Mock statistics data (in real app, this would come from API)
const mockStats = {
  kmPercorsi: 120000,
  kmMensili: 4500,
  consumoMedio: 7.8, // L/100km
  viaggiCompletati: 342,
  oreUtilizzo: 1250,
  efficienza: 92,
  costoManutenzione: 8500,
  prossimaManutenzione: 5000, // km remaining
  utilizzoMensile: {
    percentage: 78,
    days: 23
  }
};

const VehicleStats: React.FC<VehicleStatsProps> = ({ vehicle }) => {
  const formatNumber = (num: number) => {
    return num.toLocaleString('en-GB');
  };

  const formatCurrency = (num: number) => {
    return num.toLocaleString('en-GB', {
      style: 'currency',
      currency: 'EUR'
    });
  };

  // Calculate days until revision
  const getDaysUntilRevision = () => {
    if (!vehicle.scadenzaRevisione) return null;
    const revisionDate = new Date(vehicle.scadenzaRevisione);
    const today = new Date();
    const diffTime = revisionDate.getTime() - today.getTime();
    const diffDays = Math.ceil(diffTime / (1000 * 60 * 60 * 24));
    return diffDays;
  };

  const daysUntilRevision = getDaysUntilRevision();
  const revisionProgress = daysUntilRevision !== null ?
    Math.max(0, Math.min(100, ((365 - Math.abs(daysUntilRevision)) / 365) * 100)) : 0;

  return (
    <Card>
      <Card.Header className="bg-body-tertiary">
        <h5 className="mb-0">
          <FaChartLine className="me-2" />
          Statistiche Veicolo
        </h5>
      </Card.Header>
      <Card.Body>
        {/* Performance Metrics */}
        <div className="mb-4">
          <h6 className="text-uppercase text-600 mb-3">Performance</h6>

          <Row className="g-3">
            <Col xs={6}>
              <div className="border-start border-3 border-primary ps-3">
                <small className="text-muted d-block">Km Totali</small>
                <div className="fs-5 fw-bold text-900">
                  <FaTachometerAlt className="text-primary me-1" />
                  {formatNumber(mockStats.kmPercorsi)}
                </div>
              </div>
            </Col>
            <Col xs={6}>
              <div className="border-start border-3 border-info ps-3">
                <small className="text-muted d-block">Km/Mese</small>
                <div className="fs-5 fw-bold text-900">
                  <FaRoute className="text-info me-1" />
                  {formatNumber(mockStats.kmMensili)}
                </div>
              </div>
            </Col>
            <Col xs={6}>
              <div className="border-start border-3 border-success ps-3">
                <small className="text-muted d-block">Consumo</small>
                <div className="fs-5 fw-bold text-900">
                  <FaGasPump className="text-success me-1" />
                  {mockStats.consumoMedio} L/100km
                </div>
              </div>
            </Col>
            <Col xs={6}>
              <div className="border-start border-3 border-warning ps-3">
                <small className="text-muted d-block">Viaggi</small>
                <div className="fs-5 fw-bold text-900">
                  <FontAwesomeIcon icon="truck" className="text-warning me-1" />
                  {formatNumber(mockStats.viaggiCompletati)}
                </div>
              </div>
            </Col>
          </Row>
        </div>

        <hr />

        {/* Efficiency Score */}
        <div className="mb-4">
          <h6 className="text-uppercase text-600 mb-3">Efficienza Operativa</h6>
          <div className="d-flex justify-content-between mb-2">
            <span className="text-700">Punteggio Efficienza</span>
            <span className="fw-bold">{mockStats.efficienza}%</span>
          </div>
          <ProgressBar
            now={mockStats.efficienza}
            variant={mockStats.efficienza > 80 ? 'success' : mockStats.efficienza > 60 ? 'warning' : 'danger'}
            style={{ height: '8px' }}
          />
        </div>

        {/* Monthly Usage */}
        <div className="mb-4">
          <h6 className="text-uppercase text-600 mb-3">Utilizzo Mensile</h6>
          <div className="d-flex justify-content-between mb-2">
            <span className="text-700">Giorni di Utilizzo</span>
            <span className="fw-bold">{mockStats.utilizzoMensile.days}/30</span>
          </div>
          <ProgressBar
            now={mockStats.utilizzoMensile.percentage}
            variant="primary"
            style={{ height: '8px' }}
          />
          <small className="text-muted">
            {mockStats.utilizzoMensile.percentage}% del tempo disponibile
          </small>
        </div>

        <hr />

        {/* Maintenance Info */}
        <div className="mb-4">
          <h6 className="text-uppercase text-600 mb-3">Manutenzione</h6>

          <div className="mb-3">
            <div className="d-flex justify-content-between align-items-center mb-2">
              <span className="text-700">
                <FaTools className="me-2 text-muted" />
                Prossima Manutenzione
              </span>
            </div>
            <div className="bg-soft-warning rounded-2 p-2 text-center">
              <div className="fw-bold text-warning">
                tra {formatNumber(mockStats.prossimaManutenzione)} km
              </div>
            </div>
          </div>

          <div className="mb-3">
            <div className="d-flex justify-content-between mb-1">
              <span className="text-700">Costo Manutenzione (Anno)</span>
            </div>
            <div className="fs-6 fw-bold text-900">
              {formatCurrency(mockStats.costoManutenzione)}
            </div>
          </div>
        </div>

        {/* Revision Status */}
        {vehicle.scadenzaRevisione && (
          <>
            <hr />
            <div className="mb-3">
              <h6 className="text-uppercase text-600 mb-3">Stato Revisione</h6>
              <div className="d-flex justify-content-between mb-2">
                <span className="text-700">
                  <FaCalendarAlt className="me-2" />
                  Scadenza
                </span>
                <span className="fw-bold">
                  {daysUntilRevision !== null && daysUntilRevision > 0
                    ? `${daysUntilRevision} giorni`
                    : daysUntilRevision === 0
                    ? 'Oggi'
                    : 'Scaduta'}
                </span>
              </div>
              <ProgressBar
                now={revisionProgress}
                variant={
                  daysUntilRevision !== null && daysUntilRevision > 90
                    ? 'success'
                    : daysUntilRevision !== null && daysUntilRevision > 30
                    ? 'warning'
                    : 'danger'
                }
                style={{ height: '8px' }}
              />
            </div>
          </>
        )}

        {/* Quick Stats Summary */}
        <div className="bg-body-tertiary rounded-2 p-3 mt-3">
          <Row className="text-center g-2">
            <Col xs={4}>
              <div className="fw-bold fs-6">{formatNumber(mockStats.oreUtilizzo)}</div>
              <small className="text-muted">Ore Totali</small>
            </Col>
            <Col xs={4}>
              <div className="fw-bold fs-6">{mockStats.efficienza}%</div>
              <small className="text-muted">Efficienza</small>
            </Col>
            <Col xs={4}>
              <div className="fw-bold fs-6">A+</div>
              <small className="text-muted">Rating</small>
            </Col>
          </Row>
        </div>
      </Card.Body>
    </Card>
  );
};

export default VehicleStats;
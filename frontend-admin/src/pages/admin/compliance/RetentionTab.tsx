import { Col, Row, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faClockRotateLeft,
  faCircleInfo
} from '@fortawesome/free-solid-svg-icons';
import { useRetentionPreviewQuery } from 'store/api/complianceApi';
import ComplianceEmptyState from './ComplianceEmptyState';
import ComplianceSection from './ComplianceSection';
import { formatDateTime } from './complianceFormat';

// RetentionTab is a read-only dry-run: it shows which anonymized tombstones are
// past the retention window and would be hard-deleted by the auto-cleanup job.
// Nothing is deleted here — it's a preview surface only.
const RetentionTab = () => {
  const { data, isLoading } = useRetentionPreviewQuery();

  const count = data?.count ?? 0;

  return (
    <ComplianceSection
      icon={faClockRotateLeft}
      iconColor="info"
      title="Retention Preview"
    >
      {isLoading ? (
        <Spinner animation="border" size="sm" className="mt-2" />
      ) : (
        <>
          <div className="d-flex align-items-start mb-4 p-3 rounded bg-info-subtle">
            <FontAwesomeIcon
              icon={faCircleInfo}
              className="text-info mt-1 me-2 flex-shrink-0"
            />
            <div>
              <p className="mb-1 fw-semibold text-900">
                Retention cleanup preview (dry run)
              </p>
              <p className="mb-0 fs-11 text-700">
                Anonymized tombstones past the retention window that the
                auto-cleanup job would hard-delete. Nothing is deleted from this
                screen.
              </p>
            </div>
          </div>

          <Row className="g-3 mb-4">
            <Col sm={6}>
              <div className="border-start border-4 border-info rounded bg-body-tertiary py-3 px-4">
                <h6 className="text-muted mb-1">Cutoff</h6>
                <h4 className="mb-0 fw-bold text-900">
                  {formatDateTime(data?.cutoff)}
                </h4>
              </div>
            </Col>
            <Col sm={6}>
              <div
                className={`border-start border-4 border-${
                  count > 0 ? 'warning' : 'success'
                } rounded bg-body-tertiary py-3 px-4`}
              >
                <h6 className="text-muted mb-1">Candidates</h6>
                <h4
                  className={`mb-0 fw-bold text-${
                    count > 0 ? 'warning' : 'success'
                  }`}
                >
                  {count}
                </h4>
              </div>
            </Col>
          </Row>

          {count > 0 ? (
            <>
              <h6 className="text-700 mb-2">
                <FontAwesomeIcon icon={faClockRotateLeft} className="me-2" />
                Subjects to be purged
              </h6>
              <ul className="list-group">
                {data?.userUuids.map(u => (
                  <li
                    key={u}
                    className="list-group-item font-monospace small py-2"
                  >
                    {u}
                  </li>
                ))}
              </ul>
            </>
          ) : (
            <ComplianceEmptyState
              icon={faClockRotateLeft}
              message="Nothing past the retention window."
              hint="Anonymized tombstones eligible for hard deletion will be listed here."
            />
          )}
        </>
      )}
    </ComplianceSection>
  );
};

export default RetentionTab;

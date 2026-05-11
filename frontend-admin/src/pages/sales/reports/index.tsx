import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Row, Col, Card, Table, Badge, Button, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faFileAlt, faSync, faEye, faTrash } from '@fortawesome/free-solid-svg-icons';
import Background from 'components/common/Background';
import greetingsBg from 'assets/img/illustrations/ticket-greetings-bg.png';
import { useListSalesReportsQuery, useListSalesJobsQuery, useGenerateSalesReportMutation, useDeleteSalesReportMutation } from '../../../store/api/salesApi';
import type { Report } from '../../../store/api/salesApi';

const GRADE_COLORS: Record<string, string> = {
  A: 'success',
  B: 'primary',
  C: 'warning',
  D: 'danger',
  F: 'dark',
};

const ReportsPage = () => {
  const navigate = useNavigate();
  const [generating, setGenerating] = useState(false);
  const { data, isLoading, refetch } = useListSalesReportsQuery({ pageSize: 50 });
  const { data: jobsData } = useListSalesJobsQuery({ pageSize: 50 });
  const [generateReport] = useGenerateSalesReportMutation();
  const [deleteReport] = useDeleteSalesReportMutation();

  const reports = data?.reports || [];
  const completedJobs = (jobsData?.jobs || []).filter((j: any) => j.status === 'completed');
  const jobsWithoutReports = completedJobs.filter(
    (j: any) => !reports.some((r: Report) => r.jobUuid === j.uuid) && !j.reportUuid
  );

  const handleGenerateAll = async () => {
    setGenerating(true);
    for (const job of completedJobs) {
      try {
        await generateReport(job.uuid).unwrap();
      } catch { /* skip failures */ }
    }
    setGenerating(false);
    refetch();
  };

  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <Card className="bg-body-tertiary dark__bg-opacity-50 shadow-none h-100">
            <Background image={greetingsBg} className="bg-card d-none d-sm-block" />
            <Card.Header className="d-flex align-items-center z-1 p-0">
              <div className="bg-primary rounded-circle p-3 ms-3">
                <FontAwesomeIcon icon={faFileAlt} className="text-white" size="2x" />
              </div>
              <div className="ms-3">
                <h6 className="mb-1 text-primary">Sales Intelligence</h6>
                <h4 className="mb-0 text-primary fw-bold">
                  Prospect<span className="text-info fw-medium"> Reports</span>
                </h4>
              </div>
            </Card.Header>
          </Card>
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col lg={12}>
          <Card>
            <Card.Header className="d-flex justify-content-between align-items-center">
              <h5 className="mb-0">Reports ({reports.length})</h5>
              <div className="d-flex gap-2">
                {jobsWithoutReports.length > 0 && (
                  <Button variant="warning" size="sm" onClick={handleGenerateAll} disabled={generating}>
                    {generating ? <Spinner size="sm" className="me-1" /> : <FontAwesomeIcon icon={faFileAlt} className="me-1" />}
                    Generate ({jobsWithoutReports.length} missing)
                  </Button>
                )}
                <Button variant="outline-primary" size="sm" onClick={() => refetch()} disabled={isLoading}>
                  <FontAwesomeIcon icon={faSync} spin={isLoading} className="me-1" /> Refresh
                </Button>
              </div>
            </Card.Header>
            <Card.Body className="p-0">
              {isLoading && reports.length === 0 ? (
                <div className="text-center py-5"><Spinner /></div>
              ) : reports.length === 0 ? (
                <div className="text-center text-muted py-5">
                  <FontAwesomeIcon icon={faFileAlt} size="3x" className="mb-3 opacity-50" />
                  <h6>No reports yet</h6>
                  <p>Reports are generated automatically when a prospect analysis completes.</p>
                </div>
              ) : (
                <Table hover responsive className="mb-0">
                  <thead>
                    <tr>
                      <th>Company</th>
                      <th>URL</th>
                      <th>Score</th>
                      <th>Generated</th>
                      <th></th>
                    </tr>
                  </thead>
                  <tbody>
                    {reports.map((report: Report) => (
                      <tr key={report.uuid} style={{ cursor: 'pointer' }} onClick={() => navigate(`/sales/reports/${report.uuid}`)}>
                        <td className="fw-semibold">{report.companyName}</td>
                        <td className="text-truncate text-muted" style={{ maxWidth: 200 }}>
                          <small>{report.companyUrl}</small>
                        </td>
                        <td>
                          <Badge bg={GRADE_COLORS[report.grade] || 'secondary'}>
                            {report.score} ({report.grade})
                          </Badge>
                        </td>
                        <td><small>{new Date(report.createdAt).toLocaleString()}</small></td>
                        <td>
                          <div className="d-flex gap-1">
                            <Button variant="outline-primary" size="sm" onClick={e => { e.stopPropagation(); navigate(`/sales/reports/${report.uuid}`); }}>
                              <FontAwesomeIcon icon={faEye} />
                            </Button>
                            <Button variant="outline-danger" size="sm" onClick={e => {
                              e.stopPropagation();
                              if (window.confirm('Delete this report?')) deleteReport(report.uuid);
                            }}>
                              <FontAwesomeIcon icon={faTrash} />
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}
            </Card.Body>
          </Card>
        </Col>
      </Row>
    </>
  );
};

export default ReportsPage;

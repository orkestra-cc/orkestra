import React, { useState } from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Button, Card, Collapse, Row, Col, Badge } from 'react-bootstrap';
import { User } from 'store/api/userApi';

interface AdminProfileIntroProps {
  user: User;
}

const AdminProfileIntro: React.FC<AdminProfileIntroProps> = ({ user }) => {
  // Helper function to format date with time
  const formatDateTime = (dateString: string) => {
    return new Date(dateString).toLocaleString('en-GB', {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  };

  // Helper function to format date only
  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString('en-GB', {
      year: 'numeric',
      month: 'long',
      day: 'numeric'
    });
  };

  const [collapsed, setCollapsed] = useState(false);
  return (
    <Card className="mb-3">
      <Card.Header className="bg-body-tertiary">
        <h5 className="mb-0">User Information</h5>
      </Card.Header>

      <Card.Body className="text-1000">
        {/* <Row className="mb-3">
          <Col md={6}>
            <small className="text-700 d-block mb-1">Email</small>
            <p className="mb-2">{user.email}</p>
          </Col>
          <Col md={6}>
            <small className="text-700 d-block mb-1">Username</small>
            <p className="mb-2">@{user.username}</p>
          </Col>
          <Col md={6}>
            <small className="text-700 d-block mb-1">Ruolo</small>
            <p className="mb-2">
              <Badge
                bg={
                  user.role === 'super_admin'
                    ? 'danger'
                    : user.role === 'administrator'
                      ? 'warning'
                      : user.role === 'developer'
                        ? 'primary'
                        : user.role === 'manager'
                          ? 'info'
                          : user.role === 'operator'
                            ? 'success'
                            : 'secondary'
                }
              >
                {roleLabels[user.role] || user.role}
              </Badge>
            </p>
          </Col>
          <Col md={6}>
            <small className="text-700 d-block mb-1">ID Utente</small>
            <p className="mb-2 fs-10 text-700">{user.id}</p>
          </Col>
        </Row> */}

        {/* <div className="mb-3">
          <small className="text-700 d-block mb-2">Stato Account</small>
          <div>
            <Badge bg={user.isActive ? 'success' : 'danger'} className="me-2 mb-1">
              {user.isActive ? 'Attivo' : 'Inattivo'}
            </Badge>
            <Badge bg={user.emailVerified ? 'success' : 'warning'} className="me-2 mb-1">
              Email {user.emailVerified ? 'Verificata' : 'Non Verificata'}
            </Badge>
          </div>
        </div> */}

        {user.providers && user.providers.length > 0 && (
          <div className="mb-3">
            <small className="text-700 d-block mb-2">Social Login</small>
            <div>
              {user.providers.map((provider, index) => (
                <div key={index} className="d-flex align-items-center mb-2">
                  {provider.avatar ? (
                    <img
                      src={provider.avatar}
                      alt={`${provider.provider} avatar`}
                      className="rounded-circle me-2"
                      style={{ width: '32px', height: '32px' }}
                    />
                  ) : provider.provider === 'apple' ? (
                    <div
                      className="rounded-circle me-2 d-flex align-items-center justify-content-center bg-dark"
                      style={{ width: '32px', height: '32px' }}
                    >
                      <svg
                        xmlns="http://www.w3.org/2000/svg"
                        width="16"
                        height="16"
                        fill="white"
                        viewBox="0 0 16 16"
                      >
                        <path d="M11.182.008C11.148-.03 9.923.023 8.857 1.18c-1.066 1.156-.902 2.482-.878 2.516.024.034 1.52.087 2.475-1.258.955-1.345.762-2.391.728-2.43zm3.314 11.733c-.048-.096-2.325-1.234-2.113-3.422.212-2.189 1.675-2.789 1.698-2.854.023-.065-.597-.79-1.254-1.157a3.692 3.692 0 0 0-1.563-.434c-.108-.003-.483-.095-1.254.116-.508.139-1.653.589-1.968.607-.316.018-1.256-.522-2.267-.665-.647-.125-1.333.131-1.824.328-.49.196-1.422.754-2.074 2.237-.652 1.482-.311 3.83-.067 4.56.244.729.625 1.924 1.273 2.796.576.984 1.34 1.667 1.659 1.899.319.232 1.219.386 1.843.067.502-.308 1.408-.485 1.766-.472.357.013 1.061.154 1.782.539.571.197 1.111.115 1.652-.105.541-.221 1.324-1.059 2.238-2.758.347-.79.505-1.217.473-1.282z" />
                      </svg>
                    </div>
                  ) : null}
                  <div className="flex-grow-1">
                    <div className="d-flex align-items-center">
                      <Badge
                        bg={
                          provider.provider === 'google'
                            ? 'danger'
                            : provider.provider === 'apple'
                              ? 'dark'
                              : provider.provider === 'discord'
                                ? 'primary'
                                : provider.provider === 'github'
                                  ? 'secondary'
                                  : 'info'
                        }
                        className="me-2"
                      >
                        {provider.provider.charAt(0).toUpperCase() +
                          provider.provider.slice(1)}
                      </Badge>
                      <small className="text-900">{provider.email}</small>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        <Collapse in={collapsed}>
          <div>
            <Row className="mb-3">
              <Col md={6}>
                <small className="text-700 d-block mb-1">Account Created</small>
                <p className="mb-2">{formatDateTime(user.createdAt)}</p>
              </Col>
              <Col md={6}>
                <small className="text-700 d-block mb-1">
                  Last Updated
                </small>
                <p className="mb-2">{formatDateTime(user.updatedAt)}</p>
              </Col>
              <Col md={6}>
                <small className="text-700 d-block mb-1">Last Login</small>
                <p className="mb-2">
                  {user.lastLogin ? formatDateTime(user.lastLogin) : 'Never'}
                </p>
              </Col>
              <Col md={6}>
                <small className="text-700 d-block mb-1">Email Verification</small>
                <p className="mb-2">
                  <Badge bg={user.emailVerified ? 'success' : 'warning'}>
                    {user.emailVerified ? 'Verified' : 'Not Verified'}
                  </Badge>
                </p>
              </Col>
            </Row>

            <div className="mb-3">
              <small className="text-700 d-block mb-1">System Notes</small>
              <p className="text-600 fst-italic">
                Account {user.role} created on {formatDate(user.createdAt)}.
                {user.emailVerified
                  ? ' Email verified.'
                  : ' Email not yet verified.'}
                {user.isActive ? ' Account active.' : ' Account disabled.'}
              </p>
            </div>
          </div>
        </Collapse>
      </Card.Body>

      <Card.Footer className="bg-body-tertiary p-0 border-top d-grid">
        <Button variant="link" onClick={() => setCollapsed(!collapsed)}>
          Show {collapsed ? 'less' : 'more'} details
          <FontAwesomeIcon
            icon="chevron-down"
            className="ms-2 fs-11"
            transform={collapsed ? 'rotate-180' : ''}
          />
        </Button>
      </Card.Footer>
    </Card>
  );
};

export default AdminProfileIntro;

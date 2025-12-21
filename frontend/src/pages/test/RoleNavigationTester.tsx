/**
 * Development component for viewing role-based navigation
 * Only available in development environment
 *
 * Note: Navigation is now fetched from the backend API and filtered server-side.
 * This component shows what navigation the current user sees based on their role.
 */

import React from 'react';
import { Card, Row, Col, Badge, ListGroup, Alert, Spinner } from 'react-bootstrap';
import { useRoleBasedNavigation, NavItem } from 'hooks/useRoleBasedNavigation';
import { useCurrentUser } from 'hooks/auth/useAuthRTK';
import { extractUserRole } from 'utils/roleUtils';

interface RoleNavigationTesterProps {
  // This component doesn't require any props
}

const RoleNavigationTester: React.FC<RoleNavigationTesterProps> = () => {
  const { user: realUser, isAuthenticated } = useCurrentUser();
  const realUserRole = extractUserRole(realUser);

  // Get navigation from backend API (pre-filtered by user's role)
  const { filteredNavigation, isLoading, isError, userRole, refetch } = useRoleBasedNavigation();

  const renderNavItem = (item: NavItem, depth: number = 0) => {
    const indentStyle = { paddingLeft: `${depth * 20 + 10}px` };

    return (
      <div key={item.name}>
        <ListGroup.Item style={indentStyle} className="d-flex justify-content-between align-items-center">
          <div>
            {item.icon && <i className={`fas fa-${item.icon} me-2`}></i>}
            {item.name}
            {item.to && <small className="text-muted ms-2">({item.to})</small>}
          </div>
          {item.badge && (
            <Badge bg={item.badge.type || 'secondary'}>
              {item.badge.text}
            </Badge>
          )}
        </ListGroup.Item>
        {item.children && item.children.map(child => renderNavItem(child, depth + 1))}
      </div>
    );
  };

  // Don't render in production
  if (process.env.NODE_ENV !== 'development') {
    return null;
  }

  return (
    <div className="p-4">
      <h2>🔐 Role-Based Navigation Viewer</h2>
      <p className="text-muted">
        This tool shows the navigation menu as it appears for the current user.
        Navigation is fetched from the backend API and filtered server-side based on your role.
        Only visible in development mode.
      </p>

      <Row className="mb-4">
        <Col>
          <Alert variant="info">
            <div className="d-flex justify-content-between align-items-center">
              <div>
                <strong>Current User:</strong> {' '}
                {isAuthenticated ? (
                  <>
                    <Badge bg="success">{userRole || realUserRole || 'Unknown Role'}</Badge>
                    <span className="ms-2 text-muted">({realUser?.fullName || realUser?.email || realUser?.id})</span>
                  </>
                ) : (
                  <Badge bg="danger">Not Authenticated</Badge>
                )}
              </div>
              <div>
                <small>Navigation groups: {filteredNavigation.length}</small>
                <button
                  className="btn btn-sm btn-outline-primary ms-2"
                  onClick={() => refetch()}
                  disabled={isLoading}
                >
                  {isLoading ? <Spinner size="sm" /> : 'Refresh'}
                </button>
              </div>
            </div>
          </Alert>
        </Col>
      </Row>

      <Row>
        <Col md={4}>
          <Card>
            <Card.Header>
              <h5>Navigation Info</h5>
              <small className="text-muted">Backend-filtered navigation</small>
            </Card.Header>
            <Card.Body>
              <div className="mb-3">
                <strong>Status:</strong>{' '}
                {isLoading && <Badge bg="warning">Loading...</Badge>}
                {isError && <Badge bg="danger">Error</Badge>}
                {!isLoading && !isError && <Badge bg="success">Loaded</Badge>}
              </div>
              <div className="mb-3">
                <strong>User Role:</strong>{' '}
                <Badge bg="primary">{userRole || 'N/A'}</Badge>
              </div>
              <div className="mb-3">
                <strong>Total Groups:</strong>{' '}
                <Badge bg="secondary">{filteredNavigation.length}</Badge>
              </div>
              <div>
                <strong>Total Items:</strong>{' '}
                <Badge bg="secondary">
                  {filteredNavigation.reduce((acc, group) => {
                    const countItems = (items: NavItem[]): number => {
                      return items.reduce((sum, item) => {
                        return sum + 1 + (item.children ? countItems(item.children) : 0);
                      }, 0);
                    };
                    return acc + countItems(group.children);
                  }, 0)}
                </Badge>
              </div>
            </Card.Body>
          </Card>

          <Card className="mt-3">
            <Card.Header>
              <h6>Role Hierarchy</h6>
            </Card.Header>
            <Card.Body>
              <p className="mb-2">
                <Badge bg="primary">developer</Badge> {'>'}
                <Badge bg="danger">ceo</Badge> {'>'}
                <Badge bg="warning" text="dark">administrator</Badge> {'>'}
                <Badge bg="info">manager</Badge> {'>'}
                <Badge bg="success">operator</Badge> {'>'}
                <Badge bg="secondary">guest</Badge>
              </p>
              <small className="text-muted">
                Higher roles inherit access from lower roles. Navigation is filtered on the backend
                based on your role for security.
              </small>
            </Card.Body>
          </Card>
        </Col>

        <Col md={8}>
          <Card>
            <Card.Header>
              <h5>Your Navigation Menu</h5>
              <Badge bg="info">
                {filteredNavigation.length} visible group(s)
              </Badge>
            </Card.Header>
            <Card.Body style={{ maxHeight: '600px', overflowY: 'auto' }}>
              {isLoading && (
                <div className="text-center p-4">
                  <Spinner animation="border" role="status">
                    <span className="visually-hidden">Loading...</span>
                  </Spinner>
                  <p className="mt-2">Loading navigation...</p>
                </div>
              )}

              {isError && (
                <div className="text-center text-danger p-4">
                  <i className="fas fa-exclamation-triangle fa-3x mb-3"></i>
                  <p>Failed to load navigation from backend.</p>
                  <button className="btn btn-outline-danger" onClick={() => refetch()}>
                    Retry
                  </button>
                </div>
              )}

              {!isLoading && !isError && filteredNavigation.length === 0 && (
                <div className="text-center text-muted p-4">
                  <i className="fas fa-ban fa-3x mb-3"></i>
                  <p>No navigation items visible for your role.</p>
                </div>
              )}

              {!isLoading && !isError && filteredNavigation.map((group) => (
                <div key={group.label} className="mb-4">
                  <div className="d-flex justify-content-between align-items-center mb-2">
                    <h6 className="text-primary mb-0">
                      📂 {group.label.toUpperCase()}
                    </h6>
                    {group.labelDisable && (
                      <Badge bg="secondary">Hidden Label</Badge>
                    )}
                  </div>
                  <ListGroup variant="flush">
                    {group.children.map(item => renderNavItem(item))}
                  </ListGroup>
                </div>
              ))}
            </Card.Body>
          </Card>
        </Col>
      </Row>
    </div>
  );
};

export default RoleNavigationTester;

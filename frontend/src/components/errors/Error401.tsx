
import { Card, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Link, useNavigate, useLocation } from 'react-router';
import { faHome, faSignOutAlt, faShieldAlt, faArrowLeft } from '@fortawesome/free-solid-svg-icons';
import { useAuth } from 'hooks/auth/useAuthRTK';

interface Error401Props {
  requiredRole?: string;
  userRole?: string;
  message?: string;
}

const Error401: React.FC<Error401Props> = ({
  requiredRole,
  userRole,
  message
}) => {
  const navigate = useNavigate();
  const location = useLocation();
  const { user } = useAuth();

  // Get error context from navigation state
  const {
    requiredPermissions,
    accessDeniedReason,
    from
  } = location.state || {};

  const handleGoBack = () => {
    navigate(-1);
  };

  const handleLogout = () => {
    // This will be handled by the logout functionality in your auth system
    window.location.href = '/login';
  };

  const getRoleDisplayName = (role?: string) => {
    const roleMap: Record<string, string> = {
      'super_admin': 'Super Admin',
      'administrator': 'Administrator',
      'developer': 'Developer',
      'manager': 'Manager',
      'operator': 'Operator',
      'guest': 'Guest'
    };
    return role ? roleMap[role] || role : 'Unknown';
  };

  const getErrorMessage = () => {
    if (message) return message;
    if (accessDeniedReason === 'insufficient_permissions') {
      return 'Insufficient Permissions';
    }
    return 'Access Denied';
  };

  const getDetailedMessage = () => {
    const currentUserRole = userRole || user?.role;

    let details = 'You do not have the necessary permissions to access this page.';

    if (currentUserRole) {
      details += `\n\nYour current role: ${getRoleDisplayName(currentUserRole)}`;
    }

    if (requiredPermissions && requiredPermissions.length > 0) {
      const flattenedPermissions = Array.isArray(requiredPermissions[0])
        ? requiredPermissions.flat()
        : requiredPermissions;

      const requiredRoleNames = flattenedPermissions
        .map(getRoleDisplayName)
        .join(', ');

      details += `\n\nRequired roles: ${requiredRoleNames}`;
    }

    if (requiredRole) {
      details += `\n\nRequired role: ${getRoleDisplayName(requiredRole)}`;
    }

    if (from?.pathname) {
      details += `\n\nRequested page: ${from.pathname}`;
    }

    return details;
  };

  return (
    <Card className="text-center">
      <Card.Body className="p-5">
        <div className="display-1 text-warning fs-error">
          <FontAwesomeIcon icon={faShieldAlt} />
        </div>
        <div className="display-4 text-800 mt-3">401</div>
        <p className="lead mt-4 text-800 font-sans-serif fw-semibold">
          {getErrorMessage()}
        </p>
        <hr />
        <div className="text-600">
          <p style={{ whiteSpace: 'pre-line' }}>
            {getDetailedMessage()}
          </p>
        </div>
        <div className="mt-4">
          <Button
            variant="outline-secondary"
            size="sm"
            className="me-2"
            onClick={handleGoBack}
          >
            <FontAwesomeIcon icon={faArrowLeft} className="me-2" />
            Go Back
          </Button>
          <Link className="btn btn-primary btn-sm me-2" to="/">
            <FontAwesomeIcon icon={faHome} className="me-2" />
            Go to Home
          </Link>
          <Button
            variant="outline-danger"
            size="sm"
            onClick={handleLogout}
          >
            <FontAwesomeIcon icon={faSignOutAlt} className="me-2" />
            Logout
          </Button>
        </div>
        <div className="mt-4 pt-3 border-top">
          <small className="text-500">
            If you think this is an error, please contact the system administrator.
          </small>
        </div>
      </Card.Body>
    </Card>
  );
};

export default Error401;
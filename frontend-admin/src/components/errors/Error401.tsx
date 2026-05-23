import { Card, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Link, useNavigate, useLocation } from 'react-router';
import {
  faHome,
  faSignOutAlt,
  faShieldAlt,
  faArrowLeft
} from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
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
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { user } = useAuth();

  // Get error context from navigation state
  const { requiredPermissions, accessDeniedReason, from } =
    location.state || {};

  const handleGoBack = () => {
    navigate(-1);
  };

  const handleLogout = () => {
    // This will be handled by the logout functionality in your auth system
    window.location.href = '/login';
  };

  const knownRoles = new Set([
    'super_admin',
    'administrator',
    'developer',
    'manager',
    'operator',
    'guest'
  ]);

  const getRoleDisplayName = (role?: string) => {
    if (!role) return t('errors.401.roles.unknown');
    if (knownRoles.has(role)) {
      return t(`errors.401.roles.${role}` as const);
    }
    // Unknown role string — surface the raw value so admins can debug
    // unexpected JWT claims rather than swallow it under "Unknown".
    return role;
  };

  const getErrorMessage = () => {
    if (message) return message;
    if (accessDeniedReason === 'insufficient_permissions') {
      return t('errors.401.insufficientPermissions');
    }
    return t('errors.401.accessDenied');
  };

  const getDetailedMessage = () => {
    const currentUserRole = userRole || user?.role;

    let details = t('errors.401.message');

    if (currentUserRole) {
      details += `\n\n${t('errors.401.currentRole')} ${getRoleDisplayName(
        currentUserRole
      )}`;
    }

    if (requiredPermissions && requiredPermissions.length > 0) {
      const flattenedPermissions = Array.isArray(requiredPermissions[0])
        ? requiredPermissions.flat()
        : requiredPermissions;

      const requiredRoleNames = flattenedPermissions
        .map(getRoleDisplayName)
        .join(', ');

      details += `\n\n${t('errors.401.requiredRoles')} ${requiredRoleNames}`;
    }

    if (requiredRole) {
      details += `\n\n${t('errors.401.requiredRole')} ${getRoleDisplayName(
        requiredRole
      )}`;
    }

    if (from?.pathname) {
      details += `\n\n${t('errors.401.requestedPage')} ${from.pathname}`;
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
          <p style={{ whiteSpace: 'pre-line' }}>{getDetailedMessage()}</p>
        </div>
        <div className="mt-4">
          <Button
            variant="outline-secondary"
            size="sm"
            className="me-2"
            onClick={handleGoBack}
          >
            <FontAwesomeIcon icon={faArrowLeft} className="me-2" />
            {t('errors.401.goBack')}
          </Button>
          <Link className="btn btn-primary btn-sm me-2" to="/">
            <FontAwesomeIcon icon={faHome} className="me-2" />
            {t('errors.401.goHome')}
          </Link>
          <Button variant="outline-danger" size="sm" onClick={handleLogout}>
            <FontAwesomeIcon icon={faSignOutAlt} className="me-2" />
            {t('errors.401.logout')}
          </Button>
        </div>
        <div className="mt-4 pt-3 border-top">
          <small className="text-500">{t('errors.401.contactAdmin')}</small>
        </div>
      </Card.Body>
    </Card>
  );
};

export default Error401;

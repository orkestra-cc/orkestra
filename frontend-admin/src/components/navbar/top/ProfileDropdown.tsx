import { Link } from 'react-router';
import { Dropdown } from 'react-bootstrap';
import team3 from 'assets/img/team/3.jpg';
import Avatar from 'components/common/Avatar';
import paths from 'routes/paths';
import { useAuthContext } from 'providers/AuthProvider';

const ProfileDropdown = () => {
  const auth = useAuthContext();
  const user = auth.user;

  // Get avatar from primary OAuth provider metadata
  const primaryOAuthProvider = user?.oauthProviders?.find(
    provider => provider.isPrimary
  );
  const oauthAvatar = primaryOAuthProvider?.metadata?.picture as
    | string
    | undefined;
  const avatarSrc = oauthAvatar || user?.avatar || team3;

  const handleLogout = () => {
    auth.logout();
  };

  return (
    <Dropdown navbar={true} as="li">
      <Dropdown.Toggle
        bsPrefix="toggle"
        as={Link}
        to="#!"
        className="pe-0 ps-2 nav-link"
      >
        <Avatar src={avatarSrc} name={user?.fullName || user?.email} />
      </Dropdown.Toggle>

      <Dropdown.Menu className="dropdown-caret dropdown-menu-card  dropdown-menu-end">
        <div className="bg-white rounded-2 py-2 dark__bg-1000">
          {user?.fullName && (
            <>
              <div className="px-3 py-2">
                <h6 className="mb-0">{user.fullName}</h6>
                <small className="text-muted">{user.email}</small>
              </div>
              <Dropdown.Divider />
            </>
          )}
          {/* <Dropdown.Item className="fw-bold text-warning" href="#!">
            <FontAwesomeIcon icon="crown" className="me-1" />
            <span>Go Pro</span>
          </Dropdown.Item> */}

          <Dropdown.Item href="/login">Login</Dropdown.Item>
          <Dropdown.Divider />
          <Dropdown.Item as={Link} to={paths.userProfile}>
            Profile &amp; account
          </Dropdown.Item>

          <Dropdown.Item as={Link} to={paths.userSettings}>
            Settings
          </Dropdown.Item>
          <Dropdown.Item as={Link} to={paths.userSecurity}>
            Security
          </Dropdown.Item>
          <Dropdown.Divider />
          <Dropdown.Item onClick={handleLogout}>Logout</Dropdown.Item>
        </div>
      </Dropdown.Menu>
    </Dropdown>
  );
};

export default ProfileDropdown;

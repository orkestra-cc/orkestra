
import { Link } from 'react-router';
import { Nav } from 'react-bootstrap';
import ThemeControlDropdown from 'components/navbar/top/ThemeControlDropdown';

const LandingRightSideNavItem: React.FC = () => {
  return (
    <Nav navbar className="ms-auto align-items-lg-center">
      <Nav.Item as={'li'} className="me-2 d-none d-lg-block">
        <ThemeControlDropdown dropdownClassName="" iconClassName="fs-10" />
      </Nav.Item>

      <Nav.Item>
        <Nav.Link as={Link} to="/login" className="fw-semibold">
          Login
        </Nav.Link>
      </Nav.Item>
    </Nav>
  );
};

export default LandingRightSideNavItem;

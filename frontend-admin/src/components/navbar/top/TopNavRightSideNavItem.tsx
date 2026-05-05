
import NotificationDropdown from 'components/navbar/top/NotificationDropdown';
import ProfileDropdown from 'components/navbar/top/ProfileDropdown';
import { Nav } from 'react-bootstrap';
import NineDotMenu from './NineDotMenu';
import OrgSwitcher from 'components/tenant/OrgSwitcher';
import AdminTenantSwitcher from 'components/tenant/AdminTenantSwitcher';
import ThemeControlDropdown from './ThemeControlDropdown';

const TopNavRightSideNavItem = () => {
  return (
    <Nav
      navbar
      className="navbar-nav-icons ms-auto flex-row align-items-center"
      as="ul"
    >
      <OrgSwitcher />
      <AdminTenantSwitcher />
      <ThemeControlDropdown />
      <NotificationDropdown />
      <NineDotMenu />
      <ProfileDropdown />
    </Nav>
  );
};

export default TopNavRightSideNavItem;

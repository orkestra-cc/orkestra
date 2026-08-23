import NotificationDropdown from 'components/navbar/top/NotificationDropdown';
import ProfileDropdown from 'components/navbar/top/ProfileDropdown';
import { Nav } from 'react-bootstrap';
import NineDotMenu from './NineDotMenu';
import ThemeControlDropdown from './ThemeControlDropdown';
import ModuleGlobalNavActions from 'modules/runtime/ModuleGlobalNavActions';

const TopNavRightSideNavItem = () => {
  return (
    <Nav
      navbar
      className="navbar-nav-icons ms-auto flex-row align-items-center"
      as="ul"
    >
      <ModuleGlobalNavActions />
      <ThemeControlDropdown />
      <NotificationDropdown />
      <NineDotMenu />
      <ProfileDropdown />
    </Nav>
  );
};

export default TopNavRightSideNavItem;

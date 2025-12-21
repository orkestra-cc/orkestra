import React, { useEffect, useState } from 'react';
import classNames from 'classnames';
import { Link } from 'react-router';
import { Container, Nav, Navbar } from 'react-bootstrap';
import handleNavbarTransparency from 'helpers/handleNavbarTransparency';
import NavbarTopDropDownMenus from 'components/navbar/top/NavbarTopDropDownMenus';
import LandingRightSideNavItem from './LandingRightSideNavItem';
import { topNavbarBreakpoint } from 'config';
import { useAppContext } from 'providers/AppProvider';
import ThemeControlDropdown from 'components/navbar/top/ThemeControlDropdown';
import Flex from 'components/common/Flex';

const NavbarStandard: React.FC = () => {
  const {
    config: { isDark }
  } = useAppContext();
  const [navbarCollapsed, setNavbarCollapsed] = useState(true);

  useEffect(() => {
    window.addEventListener('scroll', handleNavbarTransparency, { passive: true } as AddEventListenerOptions);
    return () => window.removeEventListener('scroll', handleNavbarTransparency, { passive: true } as EventListenerOptions);
  }, []);

  return (
    <Navbar
      variant={isDark ? 'light' : 'dark'}
      fixed="top"
      expand={topNavbarBreakpoint}
      className={classNames('navbar-standard navbar-theme', {
        'bg-100': !navbarCollapsed && isDark,
        'bg-dark': !navbarCollapsed && !isDark
      })}
    >
      <Container>
        <Navbar.Brand className="text-white" as={Link as any} to="/">
          Falcon
        </Navbar.Brand>
        <Flex alignItems="center" className="gap-2">
          <ThemeControlDropdown dropdownClassName="d-lg-none" />
          <Navbar.Toggle onClick={() => setNavbarCollapsed(!navbarCollapsed)} />
        </Flex>
        <Navbar.Collapse className="scrollbar">
          <Nav>
            <NavbarTopDropDownMenus />
          </Nav>
          <LandingRightSideNavItem />
        </Navbar.Collapse>
      </Container>
    </Navbar>
  );
};

export default NavbarStandard;

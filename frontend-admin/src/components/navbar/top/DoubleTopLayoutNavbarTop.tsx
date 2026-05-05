import Logo from 'components/common/Logo';
import { useAppContext } from 'providers/AppProvider';
import { useEffect, useRef, useState } from 'react';
import { Nav, Navbar } from 'react-bootstrap';
import SearchBox from './SearchBox';
import TopNavRightSideNavItem from './TopNavRightSideNavItem';
import classNames from 'classnames';
import { topNavbarBreakpoint } from 'config';
import autoCompleteInitialItem from 'data/autocomplete/autocomplete';
import NavbarTopDropDownMenus from './NavbarTopDropDownMenus';

export const DoubleTopLayoutNavbarTop = () => {
  const {
    config: { navbarCollapsed },
    setConfig
  } = useAppContext();
  const burgerMenuRef = useRef<HTMLDivElement>(null);
  const [showDropShadow, setShowDropShadow] = useState(false);
  const handleBurgerMenu = () => {
    setConfig('navbarCollapsed', !navbarCollapsed);
  };
  const setDropShadow = () => {
    const el = document.documentElement;
    if (el.scrollTop > 0) {
      setShowDropShadow(true);
    } else {
      setShowDropShadow(false);
    }
  };
  useEffect(() => {
    window.addEventListener('scroll', setDropShadow, { passive: true });
    return () => window.removeEventListener('scroll', setDropShadow);
  }, []);

  return (
    <Navbar
      className={classNames('navbar-glass fs-10 navbar-top sticky-kit', {
        'navbar-glass-shadow': showDropShadow
      })}
      expand={topNavbarBreakpoint}
    >
      <div className="w-100">
        <div className="d-flex flex-between-center">
          <Navbar.Toggle
            ref={burgerMenuRef}
            className={`toggle-icon-wrapper me-md-3 me-2 d-lg-none`}
            as="div"
          >
            <button
              className="navbar-toggler-humburger-icon btn btn-link d-flex flex-center"
              onClick={handleBurgerMenu}
              id="burgerMenu"
            >
              <span className="navbar-toggle-icon">
                <span className="toggle-line" />
              </span>
            </button>
          </Navbar.Toggle>

          <Logo
            at="navbar-top"
            textClass="text-primary"
            width={40}
            id="topLogo"
          />
          <Nav
            navbar
            className={`align-items-center d-none d-${topNavbarBreakpoint}-block`}
            as="ul"
          >
            <Nav.Item as="li">
              <SearchBox autoCompleteItem={autoCompleteInitialItem} />
            </Nav.Item>
          </Nav>
          <TopNavRightSideNavItem />
        </div>
        <hr className="my-2 d-none d-lg-block" />
        <Navbar.Collapse in={navbarCollapsed} className="scrollbar py-2">
          <Nav navbar>
            <NavbarTopDropDownMenus />
          </Nav>
        </Navbar.Collapse>
      </div>
    </Navbar>
  );
};

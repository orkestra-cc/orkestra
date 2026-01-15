import Logo from 'components/common/Logo';
import { useAppContext } from 'providers/AppProvider';
import { useEffect, useRef, useState } from 'react';
import { Nav, Navbar } from 'react-bootstrap';
import SearchBox from './SearchBox';
import TopNavRightSideNavItem from './TopNavRightSideNavItem';
import classNames from 'classnames';
import { topNavbarBreakpoint } from 'config';
import { navbarBreakPoint } from 'config';
import autoCompleteInitialItem from 'data/autocomplete/autocomplete';

const VerticalLayoutNavbarTop = () => {
  const {
    config: { showBurgerMenu },
    setConfig
  } = useAppContext();
  const burgerMenuRef = useRef<HTMLDivElement>(null);
  const [showDropShadow, setShowDropShadow] = useState(false);
  const handleBurgerMenu = () => {
    setConfig('showBurgerMenu', !showBurgerMenu);
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
      <Navbar.Toggle
        ref={burgerMenuRef}
        className={`toggle-icon-wrapper me-md-3 me-2 d-${navbarBreakPoint}-none`}
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

      <Logo at="navbar-top" textClass="text-primary" width={160} id="topLogo" />
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
    </Navbar>
  );
};

export default VerticalLayoutNavbarTop;


import { Nav } from 'react-bootstrap';
import classNames from 'classnames';
import { Link } from 'react-router';
import { useAppContext } from 'providers/AppProvider';

interface NavRoute {
  name?: string;
  to?: string;
  active?: boolean;
  newtab?: boolean;
}

interface NavbarNavLinkProps {
  title?: string;
  route?: NavRoute;
}

const NavbarNavLink = ({ title, route }: NavbarNavLinkProps) => {
  const {
    config: { navbarCollapsed, showBurgerMenu },
    setConfig
  } = useAppContext();

  const handleClick = () => {
    if (route?.name === 'Modal') {
      setConfig('openAuthModal', true);
    }
    if (navbarCollapsed) {
      setConfig('navbarCollapsed', !navbarCollapsed);
    }
    if (showBurgerMenu) {
      setConfig('showBurgerMenu', !showBurgerMenu);
    }
  };
  if (title) {
    return (
      <Nav.Link
        as="p"
        className={classNames('fw-medium', {
          'text-500': !route?.active,
          'text-700 mb-0 fw-bold': true
        })}
        onClick={handleClick}
      >
        {title}
      </Nav.Link>
    );
  }

  return (
    <Nav.Link
      as={Link}
      className={classNames('fw-medium', {
        'text-500': !route?.active,
        'py-1': true,
        'link-600': route?.active
      })}
      to={route?.to || '#'}
      onClick={handleClick}
      target={route?.newtab ? '_blank' : undefined}
    >
      {route?.name}
    </Nav.Link>
  );
};

export default NavbarNavLink;

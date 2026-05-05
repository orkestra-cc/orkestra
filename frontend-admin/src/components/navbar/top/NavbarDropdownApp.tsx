
import { Nav, Row, Col } from 'react-bootstrap';
import { getFlatRoutes } from 'helpers/utils';
import NavbarNavLink from './NavbarNavLink';
import type { NavItem } from 'store/api/navigationApi';

interface NavbarDropdownAppProps {
  items: NavItem[] | undefined;
}

const NavbarDropdownApp = ({ items }: NavbarDropdownAppProps) => {
  const routes = getFlatRoutes(items ?? []);

  return (
    <Row>
      <Col xs={6} md={4}>
        <Nav className="flex-column">
          {routes.unTitled.map(route => (
            <NavbarNavLink key={route.name} route={route} />
          ))}
          <NavbarNavLink title="Social" />
          {routes.social.map(route => (
            <NavbarNavLink key={route.name} route={route} />
          ))}
          <NavbarNavLink title="Support Desk" />
          {routes.supportDesk.map(route => (
            <NavbarNavLink key={route.name} route={route} />
          ))}
        </Nav>
      </Col>
      <Col xs={6} md={4}>
        <NavbarNavLink title="E Learning" />
        {routes.eLearning.map(route => (
          <NavbarNavLink key={route.name} route={route} />
        ))}
        <NavbarNavLink title="Events" />
        {routes.events.map(route => (
          <NavbarNavLink key={route.name} route={route} />
        ))}
        <NavbarNavLink title="Email" />
        {routes.email.map(route => (
          <NavbarNavLink key={route.name} route={route} />
        ))}
      </Col>
      <Col xs={6} md={4}>
        <NavbarNavLink title="E Commerce" />
        {routes.eCommerce.map(route => (
          <NavbarNavLink key={route.name} route={route} />
        ))}
      </Col>
    </Row>
  );
};

export default NavbarDropdownApp;

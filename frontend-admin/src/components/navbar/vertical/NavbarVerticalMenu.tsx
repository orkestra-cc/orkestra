import { useState } from 'react';
import classNames from 'classnames';
import { Collapse, Nav } from 'react-bootstrap';
import { NavLink, useLocation } from 'react-router';
import NavbarVerticalMenuItem, {
  NavbarVerticalMenuItemRoute
} from './NavbarVerticalMenuItem';
import { useAppContext } from 'providers/AppProvider';

interface RouteItem {
  name: string;
  to?: string;
  icon?: string | string[];
  active?: boolean;
  exact?: boolean;
  newtab?: boolean;
  children?: RouteItem[];
  badge?: {
    type: string;
    text: string;
  };
}

interface CollapseItemsProps {
  route: RouteItem;
}

interface NavbarVerticalMenuProps {
  routes: RouteItem[];
}

const CollapseItems = ({ route }: CollapseItemsProps) => {
  const { pathname } = useLocation();

  const openCollapse = (childrens: RouteItem[]) => {
    const checkLink = (children: RouteItem): boolean => {
      if (children.to === pathname) {
        return true;
      }
      return (
        (Object.prototype.hasOwnProperty.call(children, 'children') &&
          children.children?.some(checkLink)) ||
        false
      );
    };
    return childrens.some(checkLink);
  };

  const [open, setOpen] = useState(openCollapse(route.children || []));

  return (
    <Nav.Item as="li">
      <Nav.Link
        onClick={() => {
          setOpen(!open);
        }}
        className={classNames('dropdown-indicator cursor-pointer', {
          'text-500': !route.active
        })}
        aria-expanded={open}
        // {...route}
      >
        <NavbarVerticalMenuItem route={route as NavbarVerticalMenuItemRoute} />
      </Nav.Link>
      <Collapse in={open}>
        <Nav className="flex-column nav" as="ul">
          <NavbarVerticalMenu routes={route.children || []} />
        </Nav>
      </Collapse>
    </Nav.Item>
  );
};

const NavbarVerticalMenu = ({ routes }: NavbarVerticalMenuProps) => {
  const {
    config: { showBurgerMenu },
    setConfig
  } = useAppContext();
  const { pathname } = useLocation();

  // Leaf destinations at this level. A shorter path (e.g. `/forms`) is a prefix
  // of a sibling (e.g. `/forms/reports`), and React Router's NavLink matches by
  // prefix — so without this both would light up on `/forms/reports`. We treat
  // a leaf as active only when no *more specific* sibling also matches the
  // current path ("most specific wins"). This still highlights the list leaf on
  // its own detail routes (`/forms/:id`), where no sibling is more specific.
  const siblingTos = routes
    .filter(r => !r.children && r.to && r.to !== '#' && r.to !== '#!')
    .map(r => r.to as string);

  const hasMoreSpecificSibling = (to: string): boolean =>
    siblingTos.some(
      other =>
        other.length > to.length &&
        (pathname === other || pathname.startsWith(`${other}/`))
    );

  const handleNavItemClick = () => {
    if (showBurgerMenu) {
      setConfig('showBurgerMenu', !showBurgerMenu);
    }
  };
  return routes.map((route: RouteItem) => {
    if (!route.children) {
      return (
        <Nav.Item as="li" key={route.name} onClick={handleNavItemClick}>
          <NavLink
            end={route.exact}
            to={route.to || '#'}
            target={route?.newtab ? '_blank' : undefined}
            onClick={() =>
              route.name === 'Modal'
                ? setConfig('openAuthModal', true)
                : undefined
            }
            className={({ isActive }) =>
              isActive &&
              route.to !== '#!' &&
              !hasMoreSpecificSibling(route.to ?? '')
                ? 'active nav-link'
                : 'nav-link'
            }
          >
            <NavbarVerticalMenuItem
              route={route as NavbarVerticalMenuItemRoute}
            />
          </NavLink>
        </Nav.Item>
      );
    }
    return <CollapseItems route={route} key={route.name} />;
  });
};

export default NavbarVerticalMenu;

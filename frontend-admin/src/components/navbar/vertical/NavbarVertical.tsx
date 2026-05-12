import { useEffect, Fragment } from 'react';
import classNames from 'classnames';
import { Nav, Navbar, Row, Col, Placeholder } from 'react-bootstrap';
import { navbarBreakPoint, topNavbarBreakpoint } from 'config';
import Flex from 'components/common/Flex';
import Logo from 'components/common/Logo';
import NavbarVerticalMenu from './NavbarVerticalMenu';
import ToggleButton from './ToggleButton';
import { capitalize } from 'helpers/utils';
import NavbarTopDropDownMenus from 'components/navbar/top/NavbarTopDropDownMenus';
import bgNavbar from 'assets/img/generic/bg-navbar.png';
import { useAppContext } from 'providers/AppProvider';
import { useRoleBasedNavigation } from 'hooks/useRoleBasedNavigation';
import { developerRealm } from 'reference/navigation/referenceRoutes';

// Show the Developer realm whenever the reference routes are registered.
// Mirrors the gate in `src/routes/referenceRoutes.tsx` so nav and routes
// stay in lockstep — they're either both present or both absent.
const SHOW_DEVELOPER_REALM =
  import.meta.env.DEV || !!import.meta.env.VITE_ENABLE_REFERENCE;

interface NavbarLabelProps {
  label: string;
}

/**
 * Loading skeleton for navigation items
 * Displays placeholder content while navigation is being fetched from backend
 */
const NavbarSkeleton = () => (
  <div className="navbar-vertical-content scrollbar">
    <Nav className="flex-column" as="ul">
      {[1, 2, 3].map(group => (
        <Fragment key={group}>
          <Nav.Item as="li">
            <Row className="mt-3 mb-2 navbar-vertical-label-wrapper">
              <Col xs="auto" className="navbar-vertical-label">
                <Placeholder animation="glow">
                  <Placeholder xs={6} />
                </Placeholder>
              </Col>
              <Col className="ps-0">
                <hr className="mb-0 navbar-vertical-divider" />
              </Col>
            </Row>
          </Nav.Item>
          {[1, 2, 3].map(item => (
            <Nav.Item as="li" key={`${group}-${item}`} className="px-3 py-2">
              <Placeholder as="div" animation="glow">
                <Placeholder xs={8} />
              </Placeholder>
            </Nav.Item>
          ))}
        </Fragment>
      ))}
    </Nav>
  </div>
);

const NavbarVertical = () => {
  const {
    config: {
      navbarPosition,
      navbarStyle,
      isNavbarVerticalCollapsed,
      showBurgerMenu
    }
  } = useAppContext();

  // Get navigation from backend API (pre-filtered by role + tenant kind)
  const { filteredNavigation, realms, isAuthenticated, isLoading, isError } =
    useRoleBasedNavigation();

  const HTMLClassList = document.getElementsByTagName('html')[0].classList;

  useEffect(() => {
    if (isNavbarVerticalCollapsed) {
      HTMLClassList.add('navbar-vertical-collapsed');
    } else {
      HTMLClassList.remove('navbar-vertical-collapsed');
    }
    return () => {
      HTMLClassList.remove('navbar-vertical-collapsed-hover');
    };
  }, [isNavbarVerticalCollapsed, HTMLClassList]);

  // Control mouseEnter event
  let time: ReturnType<typeof setTimeout> | null = null;
  const handleMouseEnter = () => {
    if (isNavbarVerticalCollapsed) {
      time = setTimeout(() => {
        HTMLClassList.add('navbar-vertical-collapsed-hover');
      }, 100);
    }
  };
  const handleMouseLeave = () => {
    if (time) clearTimeout(time);
    HTMLClassList.remove('navbar-vertical-collapsed-hover');
  };

  const NavbarLabel = ({ label }: NavbarLabelProps) => (
    <Nav.Item as="li">
      <Row className="mt-3 mb-2 navbar-vertical-label-wrapper">
        <Col xs="auto" className="navbar-vertical-label navbar-vertical-label">
          {label}
        </Col>
        <Col className="ps-0">
          <hr className="mb-0 navbar-vertical-divider"></hr>
        </Col>
      </Row>
    </Nav.Item>
  );

  // Sub-label for a realm's sections. Less prominent than NavbarLabel —
  // no divider, smaller, so the realm header stays visually dominant.
  const NavbarSectionLabel = ({ label }: NavbarLabelProps) => (
    <Nav.Item as="li">
      <div className="px-3 pt-3 pb-1 text-uppercase text-500 small fw-semibold">
        {label}
      </div>
    </Nav.Item>
  );

  // Don't render navigation if user is not authenticated
  if (!isAuthenticated) {
    return null;
  }

  return (
    <Navbar
      expand={navbarBreakPoint}
      className={classNames('navbar-vertical', {
        [`navbar-${navbarStyle}`]: navbarStyle !== 'transparent'
      })}
      variant="light"
    >
      <Flex alignItems="center">
        <ToggleButton />
        <Logo at="navbar-vertical" textClass="text-primary" width={160} />
      </Flex>
      <Navbar.Collapse
        in={showBurgerMenu}
        onMouseEnter={handleMouseEnter}
        onMouseLeave={handleMouseLeave}
        style={{
          backgroundImage:
            navbarStyle === 'vibrant'
              ? `linear-gradient(-45deg, rgba(0, 160, 255, 0.86), #0048a2),url(${bgNavbar})`
              : 'none'
        }}
      >
        {/* Loading state */}
        {isLoading && <NavbarSkeleton />}

        {/* Error state - show minimal message */}
        {isError && !isLoading && (
          <div className="navbar-vertical-content scrollbar text-center py-4">
            <small className="text-muted">Navigation unavailable</small>
          </div>
        )}

        {/* Loaded navigation — prefer v2 realms shape; fall back to v1 flat groups.
            In dev (or when VITE_ENABLE_REFERENCE is set), append the Developer realm
            pointing at the dev-only /reference/* routes. */}
        {!isLoading &&
          !isError &&
          (() => {
            const renderedRealms = SHOW_DEVELOPER_REALM
              ? [...realms, developerRealm]
              : realms;
            return (
              <div className="navbar-vertical-content scrollbar">
                <Nav className="flex-column" as="ul">
                  {renderedRealms.length > 0
                    ? renderedRealms.map(realm => (
                        <Fragment key={realm.key}>
                          <NavbarLabel label={capitalize(realm.label)} />
                          {realm.sections.map(section => (
                            <Fragment key={`${realm.key}::${section.label}`}>
                              {section.label &&
                                section.label !== realm.label && (
                                  <NavbarSectionLabel
                                    label={capitalize(section.label)}
                                  />
                                )}
                              <NavbarVerticalMenu routes={section.children} />
                            </Fragment>
                          ))}
                        </Fragment>
                      ))
                    : filteredNavigation.map(route => (
                        <Fragment key={route.label}>
                          {!route.labelDisable && (
                            <NavbarLabel label={capitalize(route.label)} />
                          )}
                          <NavbarVerticalMenu routes={route.children} />
                        </Fragment>
                      ))}
                </Nav>

                <>
                  {navbarPosition === 'combo' && (
                    <div className={`d-${topNavbarBreakpoint}-none`}>
                      <div className="navbar-vertical-divider">
                        <hr className="navbar-vertical-hr my-2" />
                      </div>
                      <Nav navbar>
                        <NavbarTopDropDownMenus />
                      </Nav>
                    </div>
                  )}
                </>
              </div>
            );
          })()}
      </Navbar.Collapse>
    </Navbar>
  );
};

export default NavbarVertical;

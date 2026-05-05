import { useState, ReactNode } from 'react';
import classNames from 'classnames';
import { Link } from 'react-router';
import { Card, Dropdown } from 'react-bootstrap';
import AuthCornerImage from 'assets/img/illustrations/authentication-corner.png';
import { breakpoints, capitalize } from 'helpers/utils';
import { topNavbarBreakpoint } from 'config';


interface NavbarDropdownProps {
  title: string;
  children: ReactNode;
}

const NavbarDropdown = ({ title, children }: NavbarDropdownProps) => {
  const [dropdownOpen, setDropdownOpen] = useState(false);

  return (
    <Dropdown
      show={dropdownOpen}
      onToggle={() => setDropdownOpen(!dropdownOpen)}
      onMouseOver={() => {
        let windowWidth = window.innerWidth;
        const breakpointValue = breakpoints[topNavbarBreakpoint as keyof typeof breakpoints];
        if (windowWidth >= breakpointValue) {
          setDropdownOpen(true);
        }
      }}
      onMouseLeave={() => {
        let windowWidth = window.innerWidth;
        const breakpointValue = breakpoints[topNavbarBreakpoint as keyof typeof breakpoints];
        if (windowWidth >= breakpointValue) {
          setDropdownOpen(false);
        }
      }}
    >
      <Dropdown.Toggle as={Link} to="#!" className="nav-link fw-semibold">
        {capitalize(title)}
      </Dropdown.Toggle>
      <Dropdown.Menu className="dropdown-menu-card mt-0 dropdown-caret">
        {/* {children} */}
        <Card
          className={classNames('shadow-none dark__bg-1000', {
            'navbar-card-app': title === 'app',
            'navbar-card-pages': title === 'pages',
            'navbar-card-components': title === 'modules'
          })}
        >
          <Card.Body
            className={classNames('scrollbar max-h-dropdown', {
              'p-0 py-2': title === 'dashboard' || title === 'documentation'
            })}
          >
            {title !== 'dashboard' && title !== 'documentation' && (
              <img
                src={AuthCornerImage}
                alt=""
                className="img-dropdown"
                width={130}
              />
            )}
            {children}
          </Card.Body>
        </Card>
      </Dropdown.Menu>
    </Dropdown>
  );
};

export default NavbarDropdown;

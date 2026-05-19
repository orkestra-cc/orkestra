import { useEffect, useState } from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Link } from 'react-router';
import { Card, Dropdown } from 'react-bootstrap';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';

const NotificationDropdown = () => {
  const [isOpen, setIsOpen] = useState(false);

  const handleToggle = () => {
    setIsOpen(!isOpen);
  };

  useEffect(() => {
    window.addEventListener(
      'scroll',
      () => {
        window.innerWidth < 1200 && setIsOpen(false);
      },
      { passive: true }
    );
  }, []);

  return (
    <Dropdown navbar={true} as="li" show={isOpen} onToggle={handleToggle}>
      <Dropdown.Toggle
        bsPrefix="toggle"
        as={Link}
        to="#!"
        className="px-0 nav-link"
      >
        <FontAwesomeIcon icon="bell" transform="shrink-6" className="fs-5" />
      </Dropdown.Toggle>

      <Dropdown.Menu className="dropdown-menu-card dropdown-menu-end dropdown-caret dropdown-caret-bg">
        <Card
          className="dropdown-menu-notification dropdown-menu-end shadow-none"
          style={{ maxWidth: '20rem' }}
        >
          <OrkestraCardHeader
            className="card-header"
            title="Notifications"
            titleTag="h6"
            light={false}
          />
          <div className="p-4 text-center text-muted fs-10">
            You&apos;re all caught up.
          </div>
        </Card>
      </Dropdown.Menu>
    </Dropdown>
  );
};

export default NotificationDropdown;

import { useState } from 'react';
import { Nav } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { Link } from 'react-router';

interface MenuItem {
  icon: IconProp;
  title: string;
  link: string;
}

const ModalSidebar = () => {
  const [addToCardMenu] = useState<MenuItem[]>([
    { icon: 'user' as IconProp, title: 'Members', link: '#!' },
    { icon: 'tag' as IconProp, title: 'Label', link: '#!' },
    { icon: 'paperclip' as IconProp, title: 'Attachments', link: '#!' },
    { icon: 'check' as IconProp, title: 'Checklists', link: '#!' }
  ]);

  const [actionMenu] = useState<MenuItem[]>([
    { icon: ['far', 'copy'] as IconProp, title: 'Copy', link: '#!' },
    { icon: 'arrow-right' as IconProp, title: 'Move', link: '#!' },
    { icon: 'trash-alt' as IconProp, title: 'Remove', link: '#!' }
  ]);
  return (
    <>
      <h6 className="mt-5 mt-lg-0">Add To Card</h6>
      {addToCardMenu.map(menu => (
        <Nav key={menu.title} className="flex-lg-column fs-10">
          <Nav.Item className="me-2 me-lg-0">
            <Nav.Link as={Link} to="#!" className="nav-link-card-details">
              <FontAwesomeIcon icon={menu.icon} className="me-2" />
              {menu.title}
            </Nav.Link>
          </Nav.Item>
        </Nav>
      ))}

      <h6 className="mt-3">Actions</h6>
      {actionMenu.map(menu => (
        <Nav key={menu.title} className="flex-lg-column fs-10">
          <Nav.Item className="me-2 me-lg-0">
            <Nav.Link as={Link} to="#!" className="nav-link-card-details">
              <FontAwesomeIcon icon={menu.icon} className="me-2" />
              {menu.title}
            </Nav.Link>
          </Nav.Item>
        </Nav>
      ))}
    </>
  );
};

export default ModalSidebar;

import NavbarVertical from 'components/navbar/vertical/NavbarVertical';

import ModalAuth from 'components/authentication/modal/ModalAuth';
import VerticalLayoutNavbarTop from 'components/navbar/top/VerticalLayoutNavbarTop';
import { Outlet } from 'react-router';

const VerticalNavLayout: React.FC = () => {
  return (
    <div className="container">
      <NavbarVertical />
      <div className="content">
        <VerticalLayoutNavbarTop />
        <Outlet />
      </div>
      <ModalAuth />
    </div>
  );
};

export default VerticalNavLayout;

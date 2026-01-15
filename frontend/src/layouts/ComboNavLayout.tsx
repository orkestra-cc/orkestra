import NavbarVertical from 'components/navbar/vertical/NavbarVertical';

import ModalAuth from 'components/authentication/modal/ModalAuth';
import { ComboLayoutNavbarTop } from 'components/navbar/top/ComboLayoutNavbarTop';
import { Outlet } from 'react-router';

const ComboNavLayout: React.FC = () => {
  return (
    <div className="container">
      <NavbarVertical />
      <div className="content">
        <ComboLayoutNavbarTop />
        <Outlet />
      </div>
      <ModalAuth />
    </div>
  );
};

export default ComboNavLayout;

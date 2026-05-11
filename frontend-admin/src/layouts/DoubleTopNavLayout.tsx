
import ModalAuth from 'components/authentication/modal/ModalAuth';
import { DoubleTopLayoutNavbarTop } from 'components/navbar/top/DoubleTopLayoutNavbarTop';
import { Outlet } from 'react-router';

const DoubleTopNavLayout: React.FC = () => {
  return (
    <div className="container">
      <div className="content">
        <DoubleTopLayoutNavbarTop />
        <Outlet />
      </div>
      <ModalAuth />
    </div>
  );
};

export default DoubleTopNavLayout;

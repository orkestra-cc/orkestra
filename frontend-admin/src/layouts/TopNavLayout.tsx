
import ModalAuth from 'components/authentication/modal/ModalAuth';
import TopNavLayoutNavbarTop from 'components/navbar/top/TopNavLayoutNavbarTop';
import { Outlet } from 'react-router';

const TopNavLayout: React.FC = () => {
  return (
    <div className="container">
      <div className="content">
        <TopNavLayoutNavbarTop />
        <Outlet />
      </div>
      <ModalAuth />
    </div>
  );
};

export default TopNavLayout;

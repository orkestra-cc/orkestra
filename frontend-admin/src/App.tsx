import { useEffect } from 'react';
import Bowser from 'bowser';
import { Outlet } from 'react-router';
import { ToastContainer } from 'react-toastify';
import { CloseButton } from 'components/common/Toast';
import { useAppContext } from 'providers/AppProvider';
import AuthProvider from 'providers/AuthProvider';
import SetupGate from 'pages/setup/SetupGate';
import { useModuleApiInjection } from 'modules/useModuleApi';
import 'react-datepicker/dist/react-datepicker.css';
import 'react-toastify/dist/ReactToastify.css';
import 'simplebar-react/dist/simplebar.min.css';


const App = () => {
  useModuleApiInjection();

  const HTMLClassList = document.getElementsByTagName('html')[0].classList;
  const {
    config: { navbarPosition }
  } = useAppContext();

  useEffect(() => {
    const browser = Bowser.getParser(window.navigator.userAgent);
    const parsedResult = browser.parse() as any;
    const { platform, browser: browserInfo } = parsedResult;

    if (platform?.type === 'windows') {
      HTMLClassList.add('windows');
    }
    if (browserInfo?.name === 'Chrome') {
      HTMLClassList.add('chrome');
    }
    if (browserInfo?.name === 'Firefox') {
      HTMLClassList.add('firefox');
    }
    if (browserInfo?.name === 'Safari') {
      HTMLClassList.add('safari');
    }
  }, [HTMLClassList]);

  useEffect(() => {
    if ((navbarPosition as string) === 'double-top') {
      HTMLClassList.add('double-top-nav-layout');
    }
    return () => HTMLClassList.remove('double-top-nav-layout');
  }, [navbarPosition]);

  return (
    <AuthProvider>
      <SetupGate>
        <Outlet />
      </SetupGate>
      <ToastContainer
        closeButton={CloseButton as any}
        icon={false}
        position="bottom-left"
      />
    </AuthProvider>
  );
};

export default App;

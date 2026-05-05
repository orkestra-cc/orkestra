
import logo from 'assets/img/illustrations/falcon.png';
import classNames from 'classnames';

interface FalconLoaderProps {
  fullPage?: boolean;
}

const FalconLoader: React.FC<FalconLoaderProps> = ({ fullPage }) => {
  return (
    <div
      className={classNames(
        'd-flex justify-content-center align-items-center h-100 w-100',
        {
          'vh-100': fullPage
        }
      )}
    >
      <div>
        <img src={logo} alt="falcon" className="logo-ripple ripple-1" width={75} />
        <img src={logo} alt="falcon" className="logo-ripple ripple-2" width={75} />
        <img src={logo} alt="falcon" className="logo-ripple ripple-3" width={75} />
        <img src={logo} alt="falcon" className="logo-ripple ripple-4" width={75} />
        <img src={logo} alt="falcon" className="logo-ripple ripple-5" width={75} />
      </div>
    </div>
  );
};

export default FalconLoader;

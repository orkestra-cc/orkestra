
import classNames from 'classnames';
import { Link, LinkProps } from 'react-router';
import logo from 'assets/img/logos/orkestra-logo.webp';

type LogoLocation = 'auth' | 'navbar-vertical' | 'navbar-top';

interface LogoProps extends Omit<LinkProps, 'to'> {
  at?: LogoLocation;
  width?: number;
  className?: string;
  textClass?: string;
}

const Logo: React.FC<LogoProps> = ({
  at = 'auth',
  width = 350,
  className,
  textClass,
  ...rest
}) => {
  return (
    <Link
      to="/"
      className={classNames(
        'text-decoration-none',
        { 'navbar-brand text-left': at === 'navbar-vertical' },
        { 'navbar-brand text-left': at === 'navbar-top' }
      )}
      {...rest}
    >
      <div
        className={classNames(
          'd-flex',
          {
            'align-items-center py-3': at === 'navbar-vertical',
            'align-items-center': at === 'navbar-top',
            'flex-center fw-bolder fs-4 mb-4': at === 'auth'
          },
          className
        )}
      >
        <img className="me-2" src={logo} alt="Logo" width={width} />
        {/* <span className={classNames('font-sans-serif', textClass)}>Orkestra</span> */}
      </div>
    </Link>
  );
};

export default Logo;

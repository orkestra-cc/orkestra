
import classNames from 'classnames';

interface DividerProps {
  className?: string;
  children?: React.ReactNode;
}

const Divider: React.FC<DividerProps> = ({ className, children }) => (
  <div className={classNames('w-100 position-relative text-center', className)}>
    <hr className="text-300" />
    <div className="divider-content-center">{children}</div>
  </div>
);

export default Divider;

import { useAppContext } from 'providers/AppProvider';
import classNames from 'classnames';
import { CloseButton, CloseButtonProps } from 'react-bootstrap';

type ButtonSize = 'sm' | 'lg';

interface OrkestraCloseButtonProps extends Omit<CloseButtonProps, 'variant'> {
  size?: ButtonSize;
  onClick?: (event: React.MouseEvent<HTMLButtonElement>) => void;
  noOutline?: boolean;
  variant?: 'white';
  className?: string;
}

const OrkestraCloseButton: React.FC<OrkestraCloseButtonProps> = ({
  size,
  onClick,
  noOutline,
  variant,
  className,
  ...rest
}) => {
  const {
    config: { isDark }
  } = useAppContext();

  return (
    <CloseButton
      variant={variant ? variant : isDark ? 'white' : undefined}
      className={classNames(
        'btn',
        {
          [`btn-${size}`]: size,
          'outline-none': noOutline
        },
        className
      )}
      onClick={onClick}
      {...rest}
    />
  );
};

export default OrkestraCloseButton;

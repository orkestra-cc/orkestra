import { cssTransition } from 'react-toastify';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

export const Fade = cssTransition({ enter: 'fadeIn', exit: 'fadeOut' });

interface CloseButtonProps {
  closeToast?: () => void;
}

export const CloseButton: React.FC<CloseButtonProps> = ({ closeToast }) => (
  <FontAwesomeIcon
    icon="times"
    className="my-2 fs-11"
    style={{ opacity: 0.5 }}
    onClick={closeToast}
  />
);

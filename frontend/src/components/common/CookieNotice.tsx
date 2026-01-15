import { ReactNode } from 'react';
import { Toast, ToastProps } from 'react-bootstrap';

interface CookieNoticeProps extends Omit<ToastProps, 'show' | 'onClose'> {
  show: boolean;
  setShow: (show: boolean) => void;
  children: ReactNode;
}

const CookieNotice = ({ show, setShow, children, ...rest }: CookieNoticeProps) => {
  return (
    <Toast
      onClose={() => setShow(false)}
      show={show}
      className="notice shadow-none bg-transparent"
      style={{
        maxWidth: '35rem'
      }}
      {...rest}
    >
      <Toast.Body className="my-3 ms-0 ms-md-5">{children}</Toast.Body>
    </Toast>
  );
};

export default CookieNotice;

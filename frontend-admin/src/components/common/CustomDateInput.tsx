import { Form } from 'react-bootstrap';
import { HTMLProps, Ref } from 'react';

interface CustomDateInputProps extends HTMLProps<HTMLInputElement> {
  value?: string;
  onClick?: () => void;
  isInvalid?: boolean;
  isValid?: boolean;
  formControlProps?: any;
  errorMessage?: string;
  ref: Ref<HTMLInputElement>;
}

const CustomDateInput = ({
  value,
  onClick,
  isInvalid,
  isValid,
  formControlProps,
  errorMessage,
  ref
}: CustomDateInputProps) => {
  return (
    <>
      <Form.Control
        ref={ref}
        isInvalid={isInvalid}
        isValid={isValid}
        value={value}
        onClick={onClick}
        {...formControlProps}
      />
      <Form.Control.Feedback type="invalid">
        {errorMessage}
      </Form.Control.Feedback>
    </>
  );
};

export default CustomDateInput;

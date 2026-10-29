import { Form } from 'react-bootstrap';
import { HTMLProps, Ref } from 'react';

interface CustomDateInputProps extends Omit<
  HTMLProps<HTMLInputElement>,
  'ref'
> {
  value?: string;
  onClick?: () => void;
  isInvalid?: boolean;
  isValid?: boolean;
  formControlProps?: any;
  errorMessage?: string;
  // react-datepicker attaches it when it clones its `customInput`.
  ref?: Ref<HTMLInputElement>;
}

// The remaining props are what react-datepicker hands its `customInput`
// (onChange, onKeyDown, onBlur, onFocus, id, aria-*): forwarded so the date
// can be typed and the calendar driven from the keyboard. `formControlProps`
// still wins, so a caller that registers the field keeps its own handlers.
const CustomDateInput = ({
  value,
  onClick,
  isInvalid,
  isValid,
  formControlProps,
  errorMessage,
  ref,
  ...rest
}: CustomDateInputProps) => {
  return (
    <>
      <Form.Control
        ref={ref}
        {...(rest as Record<string, unknown>)}
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

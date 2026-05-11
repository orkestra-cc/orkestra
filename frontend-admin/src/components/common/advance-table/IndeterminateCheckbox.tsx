import { useRef, useEffect, InputHTMLAttributes, Ref, FC } from 'react';
import classNames from 'classnames';
import { Form } from 'react-bootstrap';

interface IndeterminateCheckboxProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  indeterminate?: boolean;
  className?: string;
  ref?: Ref<HTMLInputElement>;
}

const IndeterminateCheckbox: FC<IndeterminateCheckboxProps> = ({
  indeterminate,
  className,
  ref,
  ...rest
}) => {
  const defaultRef = useRef<HTMLInputElement>(null);
  const resolvedRef = ref || defaultRef;

  useEffect(() => {
    if (resolvedRef && 'current' in resolvedRef && resolvedRef.current) {
      resolvedRef.current.indeterminate = indeterminate || false;
    }
  }, [resolvedRef, indeterminate]);

  return (
    <Form.Check
      type="checkbox"
      className={classNames(
        'form-check mb-0 d-flex align-items-center',
        className
      )}
    >
      <Form.Check.Input
        type="checkbox"
        className="mt-0"
        ref={resolvedRef}
        {...rest}
      />
    </Form.Check>
  );
};

export default IndeterminateCheckbox;

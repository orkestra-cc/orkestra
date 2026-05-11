import { useState, forwardRef, MouseEventHandler, Ref } from 'react';
import { Form, FormControlProps, FormGroupProps } from 'react-bootstrap';
import DatePicker, { DatePickerProps } from 'react-datepicker';
import { FieldErrors, UseFormSetValue } from 'react-hook-form';

interface CustomDateInputProps {
  value?: string;
  onClick?: MouseEventHandler<HTMLElement>;
  isInvalid?: boolean;
  isValid?: boolean;
  formControlProps?: FormControlProps;
  errorMessage?: string;
}

const CustomDateInput = forwardRef<HTMLInputElement, CustomDateInputProps>(
  ({ value, onClick, isInvalid, isValid, formControlProps, errorMessage }, ref) => {
    return (
      <>
        <Form.Control
          ref={ref as Ref<HTMLInputElement>}
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
  }
);

CustomDateInput.displayName = 'CustomDateInput';

interface WizardInputProps {
  label?: string | React.ReactNode;
  name: string;
  errors: FieldErrors;
  type?: 'text' | 'email' | 'password' | 'date' | 'checkbox' | 'switch' | 'radio' | 'select' | 'textarea' | 'number';
  options?: string[];
  placeholder?: string;
  formControlProps?: FormControlProps & { [key: string]: unknown };
  formGroupProps?: FormGroupProps & { as?: React.ElementType; sm?: number; md?: number; lg?: number; [key: string]: unknown };
  setValue?: UseFormSetValue<Record<string, unknown>>;
  datepickerProps?: Partial<Omit<DatePickerProps, 'onChange'>>;
}

const WizardInput = ({
  label,
  name,
  errors,
  type = 'text',
  options = [],
  placeholder,
  formControlProps,
  formGroupProps,
  setValue,
  datepickerProps
}: WizardInputProps) => {
  const [date, setDate] = useState<Date | null>(null);

  if (type === 'date') {
    return (
      <Form.Group {...formGroupProps}>
        {!!label && <Form.Label>{label}</Form.Label>}

{/* @ts-expect-error react-datepicker has complex discriminated union types for onChange */}
        <DatePicker
          selected={date}
          onChange={(newDate: Date | null) => {
            setDate(newDate);
            if (setValue) setValue(name, newDate);
          }}
          customInput={
            <CustomDateInput
              formControlProps={formControlProps}
              errorMessage={errors[name]?.message as string | undefined}
              isInvalid={!!errors[name]}
              isValid={Object.keys(errors).length > 0 && !errors[name]}
            />
          }
          {...datepickerProps}
        />
      </Form.Group>
    );
  }

  if (type === 'checkbox' || type === 'switch') {
    const { type: _type, size: _size, ...checkboxProps } = formControlProps || {};
    return (
      <Form.Check type={type === 'switch' ? 'switch' : 'checkbox'} id={name + Math.floor(Math.random() * 100)}>
        <Form.Check.Input
          {...checkboxProps}
          type="checkbox"
          isInvalid={!!errors[name]}
          isValid={Object.keys(errors).length > 0 && !errors[name]}
        />
        <Form.Check.Label className="ms-2">{label}</Form.Check.Label>
        <Form.Control.Feedback type="invalid" className="mt-0">
          {errors[name]?.message as string | undefined}
        </Form.Control.Feedback>
      </Form.Check>
    );
  }
  if (type === 'radio') {
    const { type: _type, size: _size, ...radioProps } = formControlProps || {};
    return (
      <Form.Check type="radio" id={name + Math.floor(Math.random() * 100)}>
        <Form.Check.Input
          {...radioProps}
          type="radio"
          isInvalid={!!errors[name]}
          isValid={Object.keys(errors).length > 0 && !errors[name]}
        />
        <Form.Check.Label className="ms-2">{label}</Form.Check.Label>
        <Form.Control.Feedback type="invalid" className="mt-0">
          {errors[name]?.message as string | undefined}
        </Form.Control.Feedback>
      </Form.Check>
    );
  }
  if (type === 'select') {
    const { ref: _ref, size: _size, ...selectProps } = formControlProps || {};
    return (
      <Form.Group {...formGroupProps}>
        <Form.Label>{label}</Form.Label>
        <Form.Select
          {...(selectProps as Omit<React.SelectHTMLAttributes<HTMLSelectElement>, 'size'>)}
          isInvalid={!!errors[name]}
          isValid={Object.keys(errors).length > 0 && !errors[name]}
        >
          <option value="">{placeholder}</option>
          {options.map(option => (
            <option value={option} key={option}>
              {option}
            </option>
          ))}
        </Form.Select>
        <Form.Control.Feedback type="invalid">
          {errors[name]?.message as string | undefined}
        </Form.Control.Feedback>
      </Form.Group>
    );
  }
  if (type === 'textarea') {
    return (
      <Form.Group {...formGroupProps}>
        <Form.Label>{label}</Form.Label>
        <Form.Control
          as="textarea"
          placeholder={placeholder}
          {...formControlProps}
          isValid={Object.keys(errors).length > 0 && !errors[name]}
          isInvalid={!!errors[name]}
          rows={4}
        />
        <Form.Control.Feedback type="invalid">
          {errors[name]?.message as string | undefined}
        </Form.Control.Feedback>
      </Form.Group>
    );
  }
  return (
    <Form.Group {...formGroupProps}>
      <Form.Label>{label}</Form.Label>
      <Form.Control
        type={type}
        placeholder={placeholder}
        {...formControlProps}
        isInvalid={!!errors[name]}
        isValid={Object.keys(errors).length > 0 && !errors[name]}
      />
      <Form.Control.Feedback type="invalid">
        {errors[name]?.message as string | undefined}
      </Form.Control.Feedback>
    </Form.Group>
  );
};

export default WizardInput;

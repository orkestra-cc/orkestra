import WizardInput from './WizardInput';
import { Col, Row } from 'react-bootstrap';
import { Link } from 'react-router';
import { UseFormRegister, FieldErrors, UseFormWatch } from 'react-hook-form';

interface AccountFormProps {
  register: UseFormRegister<Record<string, unknown>>;
  errors: FieldErrors;
  watch: UseFormWatch<Record<string, unknown>>;
}

const AccountForm = ({ register, errors, watch }: AccountFormProps) => {
  return (
    <>
      <WizardInput
        label="Name"
        name="name"
        errors={errors}
        formGroupProps={{ className: 'mb-3' }}
        formControlProps={{
          ...register('name')
        }}
      />

      <WizardInput
        type="email"
        errors={errors}
        label="Email*"
        name="email"
        formGroupProps={{ className: 'mb-3' }}
        formControlProps={{
          ...register('email', {
            required: 'Email field is required',
            pattern: {
              value:
                /[A-Za-z0-9._%+-]{3,}@[a-zA-Z]{3,}([.]{1}[a-zA-Z]{2,}|[.]{1}[a-zA-Z]{2,}[.]{1}[a-zA-Z]{2,})/i,
              message: 'Email must be valid'
            }
          })
        }}
      />

      <Row className="g-2 mb-3">
        <WizardInput
          type="password"
          errors={errors}
          label="Password*"
          name="password"
          formGroupProps={{ as: Col, sm: 6 }}
          formControlProps={{
            ...register('password', {
              required: 'You must specify a password',
              minLength: {
                value: 2,
                message: 'Password must have at least 2 characters'
              }
            })
          }}
        />
        <WizardInput
          type="password"
          errors={errors}
          label="Confirm Password*"
          name="confirmPassword"
          formGroupProps={{ as: Col, sm: 6 }}
          formControlProps={{
            ...register('confirmPassword', {
              required: 'Confirm Password field is required',
              validate: value =>
                value === watch('password') || 'The password do not match'
            })
          }}
        />
      </Row>

      <WizardInput
        type="checkbox"
        errors={errors}
        label={
          <>
            I accept the <Link to="#!"> terms</Link> and{' '}
            <Link to="#!"> privacy policy</Link>
          </>
        }
        name="agreedToTerms"
        formControlProps={{
          ...register('agreedToTerms', {
            required: 'You need to agree the terms and privacy policy.'
          })
        }}
      />
    </>
  );
};

export default AccountForm;

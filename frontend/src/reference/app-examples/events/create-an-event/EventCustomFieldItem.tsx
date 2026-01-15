import { useState, forwardRef } from 'react';
import { Form, Button } from 'react-bootstrap';
import DatePicker from 'react-datepicker';
import CustomDateInput from 'components/common/CustomDateInput';
import classNames from 'classnames';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { UseFormRegister, UseFormSetValue, FieldValues } from 'react-hook-form';

// Pre-create a properly typed forwardRef component for DatePicker customInput
const DateInputWrapper = forwardRef<HTMLInputElement, any>((props, ref) => (
  <CustomDateInput {...props} ref={ref} />
));

interface CustomButtonProps {
  handleRemove: (id: string | number) => void;
  id: string | number;
}

interface EventCustomFieldItemProps {
  name: string;
  type: string;
  options?: string[];
  index: number;
  register: UseFormRegister<FieldValues>;
  setValue: UseFormSetValue<FieldValues>;
  id: string | number;
  handleRemove: (id: string | number) => void;
}

const CustomButton = ({ handleRemove, id }: CustomButtonProps) => {
  return (
    <div id={String(id)} className="position-absolute end-0 top-0 z-1 hover-actions">
      <Button
        size="sm"
        variant="link"
        className="p-0"
        onClick={() => handleRemove(id)}
      >
        <FontAwesomeIcon className="text-danger" icon="times-circle" />
      </Button>
    </div>
  );
};

const EventCustomFieldItem = ({
  name,
  type,
  options,
  index,
  register,
  setValue,
  id,
  handleRemove
}: EventCustomFieldItemProps) => {
  const [date, setDate] = useState<Date | null>(null);
  const [time, setTime] = useState<Date | null>(null);

  {
    switch (type) {
      case 'number':
        return (
          <Form.Group
            className={classNames('position-relative hover-actions-trigger', {
              'mt-3': index !== 0
            })}
          >
            <Form.Label>{name}</Form.Label>
            <CustomButton handleRemove={handleRemove} id={id} />
            <Form.Control
              type="number"
              placeholder={`Enter ${name} ...`}
              {...register(`customField${index}`)}
            />
          </Form.Group>
        );
      case 'password':
        return (
          <Form.Group
            className={classNames('position-relative hover-actions-trigger', {
              'mt-3': index !== 0
            })}
          >
            <CustomButton handleRemove={handleRemove} id={id} />
            <Form.Label>{name}</Form.Label>
            <Form.Control
              type="password"
              placeholder={`Enter ${name} ...`}
              {...register(`customField${index}`)}
            />
          </Form.Group>
        );

      case 'email':
        return (
          <Form.Group
            className={classNames('position-relative hover-actions-trigger', {
              'mt-3': index !== 0
            })}
          >
            <CustomButton handleRemove={handleRemove} id={id} />
            <Form.Label>{name}</Form.Label>
            <Form.Control
              type="email"
              placeholder={`Enter ${name} ...`}
              {...register(`customField${index}`)}
            />
          </Form.Group>
        );

      case 'checkboxes':
        return (
          <Form.Group
            className={classNames('position-relative hover-actions-trigger', {
              'mt-3': index !== 0
            })}
          >
            <CustomButton handleRemove={handleRemove} id={id} />
            <Form.Label>{name}</Form.Label>

            {options?.map((option, key) => (
              <Form.Check id={`customCheckbox${key}`} key={key}>
                <Form.Check.Input
                  value={option}
                  {...register(`customField${index}`)}
                  type="checkbox"
                />
                <Form.Check.Label className="mb-0">{option}</Form.Check.Label>
              </Form.Check>
            ))}
          </Form.Group>
        );
      case 'radio':
        return (
          <Form.Group
            className={classNames('position-relative hover-actions-trigger', {
              'mt-3': index !== 0
            })}
          >
            <CustomButton handleRemove={handleRemove} id={id} />
            <Form.Label>{name}</Form.Label>

            {options?.map((option, key) => (
              <Form.Check id={`customCheckbox${key}`} key={key}>
                <Form.Check.Input
                  value={option}
                  {...register(`customField${index}`)}
                  type="radio"
                />
                <Form.Check.Label className="mb-0">{option}</Form.Check.Label>
              </Form.Check>
            ))}
          </Form.Group>
        );

      case 'select':
        return (
          <Form.Group
            className={classNames('position-relative hover-actions-trigger', {
              'mt-3': index !== 0
            })}
          >
            <CustomButton handleRemove={handleRemove} id={id} />
            <Form.Label>{name}</Form.Label>
            <Form.Select
              aria-label="Default select example"
              {...register(`CustomField${index}`)}
            >
              {options?.map((option, key) => (
                <option key={key} value={option}>
                  {option}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
        );

      case 'textarea':
        return (
          <Form.Group
            className={classNames('position-relative hover-actions-trigger', {
              'mt-3': index !== 0
            })}
            controlId="ControlTextarea"
          >
            <CustomButton handleRemove={handleRemove} id={id} />
            <Form.Label>{name}</Form.Label>
            <Form.Control
              as="textarea"
              rows={3}
              placeholder={`Enter ${name} ...`}
              {...register(`CustomField${index}`)}
            />
          </Form.Group>
        );

      case 'file':
        return (
          <Form.Group
            controlId="formFileSm"
            className={classNames('position-relative hover-actions-trigger', {
              'mt-3': index !== 0
            })}
          >
            <CustomButton handleRemove={handleRemove} id={id} />
            <Form.Label>{name}</Form.Label>
            <Form.Control type="file" {...register(`CustomField${index}`)} />
          </Form.Group>
        );

      case 'time':
        return (
          <Form.Group
            className={classNames('position-relative hover-actions-trigger', {
              'mt-3': index !== 0
            })}
            controlId="startDate"
          >
            <CustomButton handleRemove={handleRemove} id={id} />
            <Form.Label>{name}</Form.Label>
            <DatePicker
              selected={time}
              showTimeSelect
              showTimeSelectOnly
              timeIntervals={15}
              timeCaption="Time"
              dateFormat="h:mm"
              onChange={(newDate: Date | null) => {
                setTime(newDate);
                setValue(`customField${index}`, newDate);
              }}
              customInput={<DateInputWrapper formControlProps={{ placeholder: 'H:i', ...register(`customField${index}`) }} />}
            />
          </Form.Group>
        );
      case 'date':
        return (
          <Form.Group
            className={classNames('position-relative hover-actions-trigger', {
              'mt-3': index !== 0
            })}
            controlId="startDate"
          >
            <CustomButton handleRemove={handleRemove} id={id} />
            <Form.Label>{name}</Form.Label>
            <DatePicker
              selected={date}
              onChange={(newDate: Date | null) => {
                setDate(newDate);
                setValue(`customField${index}`, newDate);
              }}
              customInput={<DateInputWrapper formControlProps={{ placeholder: 'd/m/y', ...register(`customField${index}`) }} />}
            />
          </Form.Group>
        );

      default:
        return (
          <Form.Group
            className={classNames('position-relative hover-actions-trigger', {
              'mt-3': index !== 0
            })}
          >
            <CustomButton handleRemove={handleRemove} id={id} />
            <Form.Label>{name}</Form.Label>
            <Form.Control
              type="text"
              placeholder={`Enter ${name} ...`}
              {...register('customField' + index)}
            />
          </Form.Group>
        );
    }
  }
};

export default EventCustomFieldItem;

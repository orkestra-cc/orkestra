import { Card, Row, Col, Form } from 'react-bootstrap';
import { useState } from 'react';
import { useEffect } from 'react';
import IconButton from 'components/common/IconButton';
import { UseFormRegister, UseFormSetValue, FieldValues } from 'react-hook-form';

import { v4 as uuid } from 'uuid';
import EventCustomFieldItem from './EventCustomFieldItem';

interface CustomFieldType {
  name: string;
  type: string;
  options?: string[];
  id: string;
}

interface EventCustomFieldProps {
  register: UseFormRegister<FieldValues>;
  setValue: UseFormSetValue<FieldValues>;
}

const EventCustomField = ({ register, setValue }: EventCustomFieldProps) => {
  const [name, setName] = useState('');
  const [type, setType] = useState('text');
  const [options, setOptions] = useState('');
  const [hasOptions, setHasOptions] = useState(false);
  const [disabled, setDisabled] = useState(true);
  const [customFields, setCustomFields] = useState<CustomFieldType[]>([]);

  const handleRemove = (id: string | number) => {
    const items = customFields.filter(fields => fields.id !== id);
    setCustomFields(items);
  };

  const handleSubmit = () => {
    if (hasOptions) {
      const updatedOptions = options.split(',');
      setCustomFields([
        ...customFields,
        { name, type, options: updatedOptions, id: uuid() }
      ]);
    } else {
      setCustomFields([...customFields, { name, type, id: uuid() }]);
    }

    setName('');
    setType('text');
    setOptions('');
    setHasOptions(false);
    setDisabled(true);
  };

  // Handler
  useEffect(() => {
    setHasOptions(
      type === 'select' || type === 'checkboxes' || type === 'radio'
    );
  }, [type]);

  useEffect(() => {
    let disableState = true;
    if (!hasOptions && name.length) {
      disableState = false;
    }
    if (hasOptions && options.length && name.length) {
      disableState = false;
    }
    setDisabled(disableState);
  }, [name, type, hasOptions, options]);

  return (
    <Card>
      <Card.Header as="h5">Custom Fields</Card.Header>
      {customFields.length > 0 && (
        <Card.Body>
          {customFields.map((customField, index) => (
            <EventCustomFieldItem
              {...customField}
              register={register}
              index={index}
              setValue={setValue}
              key={customField.id}
              handleRemove={handleRemove}
            />
          ))}
        </Card.Body>
      )}
      <Card.Body className="bg-body-tertiary">
        <div className="bg-body-emphasis border p-3 position-relative rounded-1 mb-2">
          <Row className="gx-2 gy-3">
            <Col md="6">
              <Form.Group controlId="name">
                <Form.Label>Name</Form.Label>
                <Form.Control
                  size="sm"
                  type="text"
                  placeholder="Name (e.g. T-shirt)"
                  value={name}
                  onChange={({ target }) => setName(target.value)}
                />
              </Form.Group>
            </Col>
            <Col md="6">
              <Form.Group>
                <Form.Label>Type</Form.Label>
                <Form.Select
                  size="sm"
                  aria-label="Default select example"
                  value={type}
                  onChange={({ target }) => setType(target.value)}
                >
                  <option value="text">Text</option>
                  <option value="number">Number</option>
                  <option value="email">Email</option>
                  <option value="password">Password</option>
                  <option value="date">Date</option>
                  <option value="time">Time</option>
                  <option value="url">Url</option>
                  <option value="checkboxes">Checkboxes</option>
                  <option value="radio">Radio Buttons</option>
                  <option value="textarea">Textarea</option>
                  <option value="select">Select</option>
                  <option value="file">File</option>
                </Form.Select>
              </Form.Group>
            </Col>
            {hasOptions && (
              <Col md="12">
                <Form.Group>
                  <Form.Label>Field Option</Form.Label>
                  <Form.Control
                    as="textarea"
                    rows={3}
                    value={options}
                    onChange={({ target }) => setOptions(target.value)}
                  />
                  <Form.Text className="text-warning fs-10">
                    * Separate your options with comma
                  </Form.Text>
                </Form.Group>
              </Col>
            )}
            <Col>
              <IconButton
                variant="falcon-default"
                size="sm"
                icon="plus"
                disabled={disabled}
                transform="shrink-3"
                onClick={() => handleSubmit()}
              >
                Add Item
              </IconButton>
            </Col>
          </Row>
        </div>
      </Card.Body>
    </Card>
  );
};

export default EventCustomField;

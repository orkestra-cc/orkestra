import classNames from 'classnames';
import { useEffect, useRef, useState } from 'react';
import { Button, Col, Form, Row } from 'react-bootstrap';

interface AddAnotherFormProps {
  onSubmit: (formData: { title?: string }) => void;
  type: 'list' | 'card';
  showForm: boolean;
  setShowForm: (show: boolean) => void;
}

const AddAnotherForm = ({
  onSubmit: handleSubmit,
  type,
  showForm,
  setShowForm
}: AddAnotherFormProps) => {
  const [formData, setFormData] = useState<{ title?: string }>({});
  const inputRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (showForm && inputRef.current) {
      inputRef.current.focus();
    }
  }, [showForm]);

  return (
    <>
      {showForm && (
        <div
          className={classNames('rounded-3 transition-none', {
            'bg-100 p-x1': type === 'list',
            'p-3 border bg-white dark__bg-1000 mt-3': type === 'card'
          })}
        >
          <Form
            onSubmit={e => {
              e.preventDefault();
              return handleSubmit(formData);
            }}
          >
            <Form.Control
              as="textarea"
              rows={2}
              className="mb-2"
              ref={inputRef}
              onChange={({ target }) =>
                setFormData({ ...formData, title: target.value })
              }
              placeholder={
                type === 'list'
                  ? 'Enter list title...'
                  : 'Enter a title for this card...'
              }
            />
            <Row className="gx-2">
              <Col>
                <Button
                  variant="primary"
                  size="sm"
                  className="d-block w-100"
                  type="submit"
                >
                  Add
                </Button>
              </Col>
              <Col>
                <Button
                  variant="outline-secondary"
                  size="sm"
                  className="d-block w-100 border-400"
                  type="button"
                  onClick={() => setShowForm(false)}
                >
                  Cancel
                </Button>
              </Col>
            </Row>
          </Form>
        </div>
      )}
    </>
  );
};

export default AddAnotherForm;

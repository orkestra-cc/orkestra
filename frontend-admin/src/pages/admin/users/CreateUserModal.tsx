import React, { useState } from 'react';
import { Modal, Button, Form, Alert } from 'react-bootstrap';
import { useCreateUserMutation, CreateUserInput } from 'store/api/userApi';
import FalconCloseButton from 'components/common/FalconCloseButton';

interface CreateUserModalProps {
  show: boolean;
  onHide: () => void;
  onSuccess?: () => void;
}

const CreateUserModal: React.FC<CreateUserModalProps> = ({
  show,
  onHide,
  onSuccess
}) => {
  const [createUser, { isLoading }] = useCreateUserMutation();
  const [error, setError] = useState<string>('');
  const [formData, setFormData] = useState<CreateUserInput>({
    fullName: '',
    email: '',
    username: '',
    phone: '',
    pin: '',
    role: 'operator'
  });

  const roles = [
    { value: 'super_admin', label: 'Super Admin' },
    { value: 'administrator', label: 'Administrator' },
    { value: 'developer', label: 'Developer' },
    { value: 'manager', label: 'Manager' },
    { value: 'operator', label: 'Operator' },
    { value: 'guest', label: 'Guest' }
  ];

  const handleChange = (
    e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>
  ) => {
    const { name, value } = e.target;
    setFormData(prev => ({ ...prev, [name]: value }));
    setError('');
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    // Validation
    if (!formData.fullName.trim()) {
      setError('Full name is required');
      return;
    }
    if (!formData.email.trim()) {
      setError('Email is required');
      return;
    }

    if (!formData.phone.trim()) {
      setError('Phone number is required');
      return;
    }
    // if (!formData.username.trim()) {
    //   setError("L'username è obbligatorio");
    //   return;
    // }
    // if (!formData.pin.trim()) {
    //   setError('Il PIN è obbligatorio');
    //   return;
    // }
    // if (!/^\d{5}$/.test(formData.pin)) {
    //   setError('Il PIN deve essere un numero di 5 cifre');
    //   return;
    // }

    try {
      await createUser(formData).unwrap();
      // Reset form
      setFormData({
        fullName: '',
        email: '',
        username: '',
        phone: '',
        pin: '',
        role: 'operator'
      });
      onHide();
      if (onSuccess) onSuccess();
    } catch (err: any) {
      setError(err?.data?.message || 'Error creating user');
    }
  };

  const handleClose = () => {
    setFormData({
      fullName: '',
      email: '',
      username: '',
      phone: '',
      pin: '',
      role: 'operator'
    });
    setError('');
    onHide();
  };

  return (
    <Modal show={show} onHide={handleClose} centered size="lg">
      <Modal.Header>
        <Modal.Title>New User</Modal.Title>
        <FalconCloseButton onClick={handleClose} />
      </Modal.Header>
      <Form onSubmit={handleSubmit}>
        <Modal.Body>
          {error && (
            <Alert variant="danger" dismissible onClose={() => setError('')}>
              {error}
            </Alert>
          )}

          <Form.Group className="mb-3">
            <Form.Label>
              Full Name <span className="text-danger">*</span>
            </Form.Label>
            <Form.Control
              type="text"
              name="fullName"
              value={formData.fullName}
              onChange={handleChange}
              placeholder="e.g. John Smith"
              required
            />
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>
              Email <span className="text-danger">*</span>
            </Form.Label>
            <Form.Control
              type="email"
              name="email"
              value={formData.email}
              onChange={handleChange}
              placeholder="john.smith@example.com"
              required
            />
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>
              Phone <span className="text-danger">*</span>
            </Form.Label>
            <Form.Control
              type="tel"
              name="phone"
              value={formData.phone}
              onChange={handleChange}
              placeholder="+1 555 1234567"
              required
            />
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>Username</Form.Label>
            <Form.Control
              type="text"
              name="username"
              value={formData.username}
              onChange={handleChange}
              placeholder="johnsmith"
            />
            <Form.Text className="text-muted">
              Username will be used for system login
            </Form.Text>
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>PIN</Form.Label>
            <Form.Control
              type="text"
              name="pin"
              value={formData.pin}
              onChange={handleChange}
              placeholder="12345"
              pattern="\d{5}"
              maxLength={5}
            />
            <Form.Text className="text-muted">
              5-digit numeric code for access
            </Form.Text>
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>
              Role <span className="text-danger">*</span>
            </Form.Label>
            <Form.Select
              name="role"
              value={formData.role}
              onChange={handleChange}
              required
            >
              {roles.map(role => (
                <option key={role.value} value={role.value}>
                  {role.label}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="secondary"
            onClick={handleClose}
            disabled={isLoading}
          >
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={isLoading}>
            {isLoading ? 'Creating...' : 'Create User'}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default CreateUserModal;

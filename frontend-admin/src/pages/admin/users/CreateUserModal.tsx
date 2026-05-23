import React, { useState } from 'react';
import { Modal, Button, Form, Alert } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useCreateUserMutation, CreateUserInput } from 'store/api/userApi';
import OrkestraCloseButton from 'components/common/OrkestraCloseButton';

interface CreateUserModalProps {
  show: boolean;
  onHide: () => void;
  onSuccess?: () => void;
}

const ROLE_VALUES = [
  'super_admin',
  'administrator',
  'developer',
  'manager',
  'operator',
  'guest'
] as const;

const CreateUserModal: React.FC<CreateUserModalProps> = ({
  show,
  onHide,
  onSuccess
}) => {
  const { t } = useTranslation();
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

  const handleChange = (
    e: React.ChangeEvent<
      HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement
    >
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
      setError(t('adminUsers.createModal.errorFullNameRequired'));
      return;
    }
    if (!formData.email.trim()) {
      setError(t('adminUsers.createModal.errorEmailRequired'));
      return;
    }

    if (!formData.phone.trim()) {
      setError(t('adminUsers.createModal.errorPhoneRequired'));
      return;
    }

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
      setError(err?.data?.message || t('adminUsers.createModal.errorGeneric'));
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

  const requiredMark = (
    <span className="text-danger">
      {t('adminUsers.createModal.requiredMark')}
    </span>
  );

  return (
    <Modal show={show} onHide={handleClose} centered size="lg">
      <Modal.Header>
        <Modal.Title>{t('adminUsers.createModal.title')}</Modal.Title>
        <OrkestraCloseButton onClick={handleClose} />
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
              {t('adminUsers.createModal.labelFullName')} {requiredMark}
            </Form.Label>
            <Form.Control
              type="text"
              name="fullName"
              value={formData.fullName}
              onChange={handleChange}
              placeholder={t('adminUsers.createModal.placeholderFullName')}
              required
            />
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>
              {t('adminUsers.createModal.labelEmail')} {requiredMark}
            </Form.Label>
            <Form.Control
              type="email"
              name="email"
              value={formData.email}
              onChange={handleChange}
              placeholder={t('adminUsers.createModal.placeholderEmail')}
              required
            />
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>
              {t('adminUsers.createModal.labelPhone')} {requiredMark}
            </Form.Label>
            <Form.Control
              type="tel"
              name="phone"
              value={formData.phone}
              onChange={handleChange}
              placeholder={t('adminUsers.createModal.placeholderPhone')}
              required
            />
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>{t('adminUsers.createModal.labelUsername')}</Form.Label>
            <Form.Control
              type="text"
              name="username"
              value={formData.username}
              onChange={handleChange}
              placeholder={t('adminUsers.createModal.placeholderUsername')}
            />
            <Form.Text className="text-muted">
              {t('adminUsers.createModal.helpUsername')}
            </Form.Text>
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>{t('adminUsers.createModal.labelPin')}</Form.Label>
            <Form.Control
              type="text"
              name="pin"
              value={formData.pin}
              onChange={handleChange}
              placeholder={t('adminUsers.createModal.placeholderPin')}
              pattern="\d{5}"
              maxLength={5}
            />
            <Form.Text className="text-muted">
              {t('adminUsers.createModal.helpPin')}
            </Form.Text>
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>
              {t('adminUsers.createModal.labelRole')} {requiredMark}
            </Form.Label>
            <Form.Select
              name="role"
              value={formData.role}
              onChange={handleChange}
              required
            >
              {ROLE_VALUES.map(value => (
                <option key={value} value={value}>
                  {t(`adminUsers.roles.${value}`)}
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
            {t('adminUsers.createModal.cancel')}
          </Button>
          <Button variant="primary" type="submit" disabled={isLoading}>
            {isLoading
              ? t('adminUsers.createModal.submitting')
              : t('adminUsers.createModal.submit')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default CreateUserModal;

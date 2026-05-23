import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import React, { useState } from 'react';
import { Button, Card, Form } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

const ChangePassword: React.FC = () => {
  const { t } = useTranslation();
  const [formData, setFormData] = useState({
    oldPassword: '',
    newPassword: '',
    confirmPassword: ''
  });

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setFormData({
      ...formData,
      [e.target.name]: e.target.value
    });
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
  };

  return (
    <Card className="mb-3">
      <OrkestraCardHeader title={t('settings.changePassword.title')} />
      <Card.Body className="bg-body-tertiary">
        <Form onSubmit={handleSubmit}>
          <Form.Group className="mb-3" controlId="oldPassword">
            <Form.Label>{t('settings.changePassword.oldPassword')}</Form.Label>
            <Form.Control
              type="text"
              value={formData.oldPassword}
              name="oldPassword"
              onChange={handleChange}
            />
          </Form.Group>
          <Form.Group className="mb-3" controlId="newPassword">
            <Form.Label>{t('settings.changePassword.newPassword')}</Form.Label>
            <Form.Control
              type="text"
              value={formData.newPassword}
              name="newPassword"
              onChange={handleChange}
            />
          </Form.Group>
          <Form.Group className="mb-3" controlId="confirmPassword">
            <Form.Label>
              {t('settings.changePassword.confirmPassword')}
            </Form.Label>
            <Form.Control
              type="text"
              value={formData.confirmPassword}
              name="confirmPassword"
              onChange={handleChange}
            />
          </Form.Group>
          <Button className="w-100" type="submit">
            {t('settings.changePassword.submit')}
          </Button>
        </Form>
      </Card.Body>
    </Card>
  );
};

export default ChangePassword;

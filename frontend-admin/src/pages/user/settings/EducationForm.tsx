import React, { useState } from 'react';
import { Button, Col, Form, Row } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import InputField from '../InputField';

const EducationForm: React.FC = () => {
  const { t } = useTranslation();
  const [formData, setFormData] = useState({
    school: '',
    degree: '',
    field: '',
    from: '',
    to: ''
  });

  const handleChange = (
    e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>
  ) => {
    setFormData({
      ...formData,
      [e.target.name]: e.target.value
    });
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
  };

  return (
    <Form onSubmit={handleSubmit}>
      <InputField
        value={formData.school}
        label={t('userProfileScaffold.education.labelSchool')}
        name="school"
        handleChange={handleChange}
      />
      <InputField
        value={formData.degree}
        label={t('userProfileScaffold.education.labelDegree')}
        name="degree"
        handleChange={handleChange}
      />
      <InputField
        value={formData.field}
        label={t('userProfileScaffold.education.labelField')}
        name="field"
        handleChange={handleChange}
      />

      <InputField
        type="date"
        value={formData.from}
        label={t('userProfileScaffold.education.labelFrom')}
        name="from"
        onChange={value => {
          setFormData({ ...formData, from: value });
        }}
      />

      <InputField
        type="date"
        value={formData.to}
        label={t('userProfileScaffold.education.labelTo')}
        name="to"
        onChange={value => {
          setFormData({ ...formData, to: value });
        }}
      />

      <Form.Group as={Row} className="mb-3">
        <Col sm={{ span: 10, offset: 3 }}>
          <Button type="submit">
            {t('userProfileScaffold.education.save')}
          </Button>
        </Col>
      </Form.Group>
    </Form>
  );
};

export default EducationForm;

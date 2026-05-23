import React, { useState } from 'react';
import { Button, Card, Col, Form, Row } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';

const ProfileSettings: React.FC = () => {
  const { t } = useTranslation();
  const [formData, setFormData] = useState({
    firstName: 'Anthony',
    lastName: 'Hopkins',
    email: 'anthony@gmail.com',
    phone: '+44098098304',
    heading: 'Software Engineer',
    intro:
      'Dedicated, passionate, and accomplished Full Stack Developer with 9+ years of progressive experience working as an Independent Contractor for Google and developing and growing my educational social network that helps others learn programming, web design, game development, networking. I’ve acquired a wide depth of knowledge and expertise in using my technical skills in programming, computer science, software development, and mobile app development to developing solutions to help organizations increase productivity, and accelerate business performance. It’s great that we live in an age where we can share so much with technology but I’m but I’m ready for the next phase of my career, with a healthy balance between the virtual world and a workplace where I help others face-to-face. There’s always something new to learn, especially in IT-related fields. People like working with me because I can explain technology to everyone, from staff to executives who need me to tie together the details and the big picture. I can also implement the technologies that successful projects need.'
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
    <Card>
      <OrkestraCardHeader title={t('userScaffold.profileSettings.cardTitle')} />
      <Card.Body className="bg-body-tertiary">
        <Form onSubmit={handleSubmit}>
          <Row className="mb-3 g-3">
            <Form.Group as={Col} lg={6} controlId="firstName">
              <Form.Label>
                {t('userScaffold.profileSettings.labelFirstName')}
              </Form.Label>
              <Form.Control
                type="text"
                placeholder={t(
                  'userScaffold.profileSettings.placeholderFirstName'
                )}
                value={formData.firstName}
                name="firstName"
                onChange={handleChange}
              />
            </Form.Group>

            <Form.Group as={Col} lg={6} controlId="lastName">
              <Form.Label>
                {t('userScaffold.profileSettings.labelLastName')}
              </Form.Label>
              <Form.Control
                type="text"
                placeholder={t(
                  'userScaffold.profileSettings.placeholderLastName'
                )}
                value={formData.lastName}
                name="lastName"
                onChange={handleChange}
              />
            </Form.Group>
          </Row>
          <Row className="mb-3 g-3">
            <Form.Group as={Col} lg={6} controlId="email">
              <Form.Label>
                {t('userScaffold.profileSettings.labelEmail')}
              </Form.Label>
              <Form.Control
                type="email"
                placeholder={t('userScaffold.profileSettings.placeholderEmail')}
                value={formData.email}
                name="email"
                onChange={handleChange}
              />
            </Form.Group>

            <Form.Group as={Col} lg={6} controlId="phone">
              <Form.Label>
                {t('userScaffold.profileSettings.labelPhone')}
              </Form.Label>
              <Form.Control
                type="text"
                placeholder={t('userScaffold.profileSettings.placeholderPhone')}
                value={formData.phone}
                name="phone"
                onChange={handleChange}
              />
            </Form.Group>
          </Row>

          <Form.Group className="mb-3" controlId="heading">
            <Form.Label>
              {t('userScaffold.profileSettings.labelHeading')}
            </Form.Label>
            <Form.Control
              type="text"
              placeholder={t('userScaffold.profileSettings.placeholderHeading')}
              value={formData.heading}
              name="heading"
              onChange={handleChange}
            />
          </Form.Group>

          <Form.Group className="mb-3" controlId="intro">
            <Form.Label>
              {t('userScaffold.profileSettings.labelIntro')}
            </Form.Label>
            <Form.Control
              as="textarea"
              rows={13}
              placeholder={t('userScaffold.profileSettings.placeholderIntro')}
              value={formData.intro}
              name="intro"
              onChange={handleChange}
            />
          </Form.Group>
          <div className="text-end">
            <Button variant="primary" type="submit">
              {t('userScaffold.profileSettings.submit')}
            </Button>
          </div>
        </Form>
      </Card.Body>
    </Card>
  );
};

export default ProfileSettings;

import React, { useState } from 'react';
import { Card } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import experiences from 'data/experiences';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import Experience from '../Experience';
import ExperienceForm from './ExperienceForm';

const ExperiencesSettings: React.FC = () => {
  const { t } = useTranslation();
  const [collapsed, setCollapsed] = useState(false);
  return (
    <Card className="mt-3">
      <OrkestraCardHeader
        title={t('userProfileScaffold.experiences.cardTitle')}
      />
      <Card.Body className="fs-10 bg-body-tertiary">
        <ExperienceForm collapsed={collapsed} setCollapsed={setCollapsed} />
        {experiences.map((experience: any, index: number) => (
          <Experience
            key={experience.id}
            experience={experience}
            isLast={index === experiences.length - 1}
          />
        ))}
      </Card.Body>
    </Card>
  );
};

export default ExperiencesSettings;

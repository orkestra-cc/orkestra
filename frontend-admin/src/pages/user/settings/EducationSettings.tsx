import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import Flex from 'components/common/Flex';
import educationData from 'data/educations';
import experiences from 'data/experiences';
import React, { useState } from 'react';
import { Card, Collapse } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import EducationItem from '../EducationItem';
import EducationForm from './EducationForm';

const EducationSettings: React.FC = () => {
  const { t } = useTranslation();
  const [collapsed, setCollapsed] = useState(false);
  return (
    <Card className="mt-3">
      <OrkestraCardHeader
        title={t('userProfileScaffold.education.cardTitle')}
      />
      <Card.Body className="fs-10 bg-body-tertiary">
        <div>
          <Flex
            alignItems="center"
            className="mb-4 text-primary cursor-pointer fs-9"
            onClick={() => {
              setCollapsed(!collapsed);
            }}
          >
            <span className="circle-dashed">
              <FontAwesomeIcon icon="plus" />
            </span>
            <span className="ms-3">
              {t('userProfileScaffold.education.addNew')}
            </span>
          </Flex>
          <Collapse in={collapsed}>
            <div>
              <EducationForm />
              <div className="border-dashed border-bottom my-3" />
            </div>
          </Collapse>
        </div>
        {educationData.map((item: any, index: number) => (
          <EducationItem
            key={item.id}
            details={item}
            isLast={index === experiences.length - 1}
          />
        ))}
      </Card.Body>
    </Card>
  );
};

export default EducationSettings;

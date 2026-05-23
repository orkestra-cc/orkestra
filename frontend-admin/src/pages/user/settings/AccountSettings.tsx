import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import TooltipBadge from 'components/common/TooltipBadge';
import React, { useState } from 'react';
import { Card, Form } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

const AccountSettings: React.FC = () => {
  const { t } = useTranslation();
  const [formData, setFormData] = useState({
    viewProfile: 'my-followers',
    tagSettings: 'group-members',
    showFollowers: true,
    showEmail: true,
    showExperience: false,
    numberVisibility: true,
    allowFollow: false
  });

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.type === 'checkbox') {
      setFormData({
        ...formData,
        [e.target.name]: e.target.checked
      });
    } else {
      setFormData({
        ...formData,
        [e.target.name]: e.target.value
      });
    }
  };

  return (
    <Card className="mb-3">
      <OrkestraCardHeader title={t('settings.account.title')} />
      <Card.Body className="bg-body-tertiary">
        <div>
          <h6 className="fw-bold">
            {t('settings.account.whoSeeProfile')}
            <TooltipBadge
              tooltip={t('settings.account.whoSeeProfileTooltip')}
              icon="question-circle"
            />
          </h6>
          <div className="ps-2 mb-2">
            <Form.Check
              type="radio"
              id="profile-everyone"
              label={t('settings.account.everyone')}
              className="form-label-nogutter"
              value="everyone"
              name="viewProfile"
              onChange={handleChange}
              checked={formData.viewProfile === 'everyone'}
            />
            <Form.Check
              type="radio"
              id="profile-followers"
              label={t('settings.account.myFollowers')}
              className="form-label-nogutter"
              value="my-followers"
              name="viewProfile"
              onChange={handleChange}
              checked={formData.viewProfile === 'my-followers'}
            />
            <Form.Check
              type="radio"
              id="profile-members"
              label={t('settings.account.onlyMe')}
              className="form-label-nogutter"
              value="only-me"
              name="viewProfile"
              onChange={handleChange}
              checked={formData.viewProfile === 'only-me'}
            />
          </div>
        </div>

        <div>
          <h6 className="fw-bold">
            {t('settings.account.whoCanTag')}
            <TooltipBadge
              tooltip={t('settings.account.whoCanTagTooltip')}
              icon="question-circle"
            />
          </h6>
          <div className="ps-2">
            <Form.Check
              type="radio"
              id="tag-everyone"
              label={t('settings.account.everyone')}
              className="form-label-nogutter"
              value="everyone"
              name="tagSettings"
              onChange={handleChange}
              checked={formData.tagSettings === 'everyone'}
            />
            <Form.Check
              type="radio"
              id="tag-members"
              label={t('settings.account.groupMembers')}
              className="form-label-nogutter"
              value="group-members"
              name="tagSettings"
              onChange={handleChange}
              checked={formData.tagSettings === 'group-members'}
            />
          </div>
        </div>

        <div className="border-dashed border-bottom my-3" />

        <div className="ps-2">
          <Form.Check
            type="checkbox"
            id="show-followers"
            label={t('settings.account.showFollowers')}
            className="form-label-nogutter"
            name="showFollowers"
            onChange={handleChange}
            checked={formData.showFollowers}
          />
          <Form.Check
            type="checkbox"
            id="show-email"
            label={t('settings.account.showEmail')}
            className="form-label-nogutter"
            name="showEmail"
            onChange={handleChange}
            checked={formData.showEmail}
          />
          <Form.Check
            type="checkbox"
            id="show-experience"
            label={t('settings.account.showExperience')}
            className="form-label-nogutter"
            name="showExperience"
            onChange={handleChange}
            checked={formData.showExperience}
          />
        </div>

        <div className="border-dashed border-bottom my-3" />

        <div className="ps-2">
          <Form.Check
            type="switch"
            id="custom-switch-phone"
            label={t('settings.account.showPhone')}
            className="form-label-nogutter"
            name="numberVisibility"
            onChange={handleChange}
            checked={formData.numberVisibility}
          />
          <Form.Check
            type="switch"
            id="custom-switch-follow"
            label={t('settings.account.allowFollow')}
            className="form-label-nogutter"
            name="allowFollow"
            onChange={handleChange}
            checked={formData.allowFollow}
          />
        </div>
      </Card.Body>
    </Card>
  );
};

export default AccountSettings;

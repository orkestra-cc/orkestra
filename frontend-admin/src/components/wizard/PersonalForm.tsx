import { useState } from 'react';
import WizardInput from './WizardInput';
import OrkestraDropzone, { CustomFile } from 'components/common/OrkestraDropzone';
import avatarImg from 'assets/img/team/avatar.png';
import { isIterableArray } from 'helpers/utils';
import Avatar from 'components/common/Avatar';
import cloudUpload from 'assets/img/icons/cloud-upload.svg';
import Flex from 'components/common/Flex';
import { Col, Row } from 'react-bootstrap';
import { useAuthWizardContext } from 'providers/AuthWizardProvider';
import { UseFormRegister, FieldErrors, UseFormSetValue } from 'react-hook-form';

interface PersonalFormProps {
  register: UseFormRegister<Record<string, unknown>>;
  errors: FieldErrors;
  setValue: UseFormSetValue<Record<string, unknown>>;
}

const PersonalForm = ({ register, errors, setValue }: PersonalFormProps) => {
  const { user } = useAuthWizardContext();
  const [avatar, setAvatar] = useState<CustomFile[]>([
    ...(user.avater ? user.avater : []),
    { src: avatarImg }
  ]);

  return (
    <>
      <Row className="mb-3">
        <Col md="auto">
          <Avatar
            size="4xl"
            src={
              isIterableArray(avatar) ? avatar[0]?.base64 || avatar[0]?.src : ''
            }
          />
        </Col>
        <Col md>
          <OrkestraDropzone
            files={avatar}
            onChange={files => {
              setAvatar(files);
              setValue('avatar', files);
            }}
            multiple={false}
            accept={{ 'image/*': [] }}
            placeholder={
              <>
                <Flex justifyContent="center">
                  <img src={cloudUpload} alt="" width={25} className="me-2" />
                  <p className="fs-9 mb-0 text-700">
                    Upload your profile picture
                  </p>
                </Flex>
                <p className="mb-0 w-75 mx-auto text-400">
                  Upload a 300x300 jpg image with a maximum size of 400KB
                </p>
              </>
            }
          />
        </Col>
      </Row>

      <WizardInput
        type="select"
        label="Gender"
        name="gender"
        placeholder="Select your gender..."
        errors={errors}
        options={['Male', 'Female', 'Other']}
        formGroupProps={{
          className: 'mb-3'
        }}
        formControlProps={{
          ...register('gender')
        }}
      />

      <WizardInput
        type="number"
        label="Phone"
        name="phone"
        errors={errors}
        formGroupProps={{
          className: 'mb-3'
        }}
        formControlProps={{
          className: 'input-spin-none',
          ...register('phone')
        }}
      />

      <WizardInput
        type="date"
        label="Date of Birth"
        name="birthDate"
        errors={errors}
        setValue={setValue}
        formGroupProps={{
          className: 'mb-3'
        }}
        formControlProps={{
          placeholder: 'Date of Birth',
          ...register('birthDate')
        }}
      />

      <WizardInput
        type="textarea"
        label="Address"
        name="address"
        errors={errors}
        formGroupProps={{
          className: 'mb-3'
        }}
        formControlProps={{
          ...register('address')
        }}
      />
    </>
  );
};

export default PersonalForm;


import EventDetails from './EventDetails';
import EventTicket from './EventTicket';
import EventSchedule from './EventSchedule';
import EventHeader from './EventHeader';
import EventUpload from './EventUpload';
import EventFooter from './EventFooter';
import { Col, Form, Row } from 'react-bootstrap';
import EventOtherInfo from './EventOtherInfo';
import EventBanner from './EventBanner';
import { useForm, FieldValues, UseFormRegister, UseFormSetValue, Control } from 'react-hook-form';
import EventCustomField from './EventCustomField';

interface FormValues extends FieldValues {
  timeZone: string;
  selectType: string;
  selectTopic: string;
}

const CreateEvent = () => {
  const defaultValues: FormValues = {
    timeZone: 'GMT-12:00/Etc/GMT-12',
    selectType: '1',
    selectTopic: '1'
  };
  const { register, handleSubmit, setValue, control, reset } = useForm<FormValues>({
    defaultValues
  });

  const onSubmit = (data: FormValues) => {
    console.log(data);
    // ------- Get all object keys form data and set empty values to reset ------------
    const submittedValues: Record<string, string> = {};
    const keys = Object.keys(data);
    for (const key of keys) {
      submittedValues[key] = '';
    }
    const allValues = { ...submittedValues, ...defaultValues };
    reset({ ...allValues });
  };

  return (
    <Form onSubmit={handleSubmit(onSubmit)}>
      <Row className="g-3">
        <Col xs={12}>
          <EventHeader />
        </Col>
        <Col xs={12}>
          <EventBanner />
        </Col>
        <Col lg={8}>
          <EventDetails register={register as unknown as UseFormRegister<FieldValues>} setValue={setValue as unknown as UseFormSetValue<FieldValues>} />
          <EventTicket />
          <EventSchedule register={register as unknown as UseFormRegister<FieldValues>} setValue={setValue as unknown as UseFormSetValue<FieldValues>} />
          <EventUpload setValue={setValue as unknown as UseFormSetValue<FieldValues>} />
          <EventCustomField register={register as unknown as UseFormRegister<FieldValues>} setValue={setValue as unknown as UseFormSetValue<FieldValues>} />
        </Col>
        <Col lg={4}>
          <div className="sticky-sidebar">
            <EventOtherInfo register={register as unknown as UseFormRegister<FieldValues>} control={control as unknown as Control<FieldValues>} />
          </div>
        </Col>
        <Col>
          <EventFooter />
        </Col>
      </Row>
    </Form>
  );
};

export default CreateEvent;

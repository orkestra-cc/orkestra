
import { Button, Form } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import FalconComponentCard from 'components/common/FalconComponentCard';
import Select from 'react-select';
import * as yup from 'yup';
import { Controller, useForm } from 'react-hook-form';
import MultiSelect from 'components/common/MultiSelect';
import { yupResolver } from '@hookform/resolvers/yup';
import classNames from 'classnames';

const singleSelectCode = `function SingleSelectExample() {
  const [value, setValue] = useState(null);
  const organizerOptions = [
    { value: '1', label: ' Massachusetts Institute of Technology' },
    { value: '2', label: 'University of Chicago' },
    { value: '3', label: 'GSAS Open Labs At Harvard' },
    { value: '4', label: 'California Institute of Technology' }
  ];
  return (
    <Select
      closeMenuOnSelect={false}
      options={organizerOptions}
      placeholder='Select...'
      classNamePrefix="react-select"
      value={value}
      onChange={value => setValue(value)}
    />
  );
}`;

const multipleSelectCode = `function MultipleSelectExample() {
  const [value, setValue] = useState(null);
  const organizerOptions = [
    { value: '1', label: ' Massachusetts Institute of Technology' },
    { value: '2', label: 'University of Chicago' },
    { value: '3', label: 'GSAS Open Labs At Harvard' },
    { value: '4', label: 'California Institute of Technology' }
  ];
  return (
    <Select
      closeMenuOnSelect={false}
      options={organizerOptions}
      placeholder='Select...'
      isMulti
      classNamePrefix="react-select"
      value={value}
      onChange={value => setValue(value)}
    />
  );
}`;

const reactSelectValidationCode = `
  const schema = yup
    .object({
      tags: yup.array().min(1, 'This field is required.').required()
    })
    .required();

  const ReactSelectValidation = () => {
    const {
      handleSubmit,
      control,
      formState: { errors }
    } = useForm({
      resolver: yupResolver(schema)
    });

    const tagOptions = [
      { value: 'gamingLaptop', label: 'Gaming laptop' },
      { value: 'ultrabooks', label: 'Ultrabooks' },
      { value: 'budgetLaptop', label: 'Budget laptop' },
      { value: 'professional', label: 'Professional' }
    ];

    const onSubmit = data => {
      console.log(data);
    };

    return (
      <Form onSubmit={handleSubmit(onSubmit)}>
        <Form.Group>
          <Controller
            name="tags"
            render={({ field, ref }) => (
              <MultiSelect
                ref={ref}
                closeMenuOnSelect={false}
                isMulti
                options={tagOptions}
                className={classNames({
                  'is-invalid': errors.tags
                })}
                {...field}
              />
            )}
            control={control}
          />
          <Form.Control.Feedback type="invalid">
            {errors.tags?.message}
          </Form.Control.Feedback>
        </Form.Group>
        {/* <div className="invalid-feedback">{errors.tags?.message}</div> */}

        <Button variant="primary" type="submit" className="mt-3" size="sm">
          Submit
        </Button>
      </Form>
    );
  };`;

const Placeholder = () => (
  <>
    <PageHeader
      title="Advance Select"
      description="React-Falcon uses <strong>React Select</strong> for advance select component. React Select is a flexible and beautiful Select Input control for ReactJS with multiselect, autocomplete, async and creatable support."
      className="mb-3"
    >
      <Button
        href="https://react-select.com/home"
        target="_blank" rel="noopener noreferrer"
        variant="link"
        size="sm"
        className="ps-0"
      >
        React Select Documentation
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Multiple Select" />
      <FalconComponentCard.Body
        code={multipleSelectCode}
        scope={{ Select }}
        language="jsx"
      />
    </FalconComponentCard>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Single Select" />
      <FalconComponentCard.Body
        code={singleSelectCode}
        scope={{ Select }}
        language="jsx"
      />
    </FalconComponentCard>

    <FalconComponentCard>
      <FalconComponentCard.Header title="With validation" />
      <FalconComponentCard.Body
        code={reactSelectValidationCode}
        language="jsx"
        hidePreview
      >
        <ReactSelectValidation />
      </FalconComponentCard.Body>
    </FalconComponentCard>
  </>
);

export default Placeholder;

const schema = yup
  .object({
    tags: yup.array().min(1).required('This field is required.')
  })
  .required();

const ReactSelectValidation = () => {
  const {
    handleSubmit,
    control,
    formState: { errors }
  } = useForm({
    resolver: yupResolver(schema)
  });

  const tagOptions = [
    { value: 'gamingLaptop', label: 'Gaming laptop' },
    { value: 'ultrabooks', label: 'Ultrabooks' },
    { value: 'budgetLaptop', label: 'Budget laptop' },
    { value: 'professional', label: 'Professional' }
  ];

  const onSubmit = (data: { tags: unknown[] }) => {
    console.log(data);
  };

  return (
    <Form onSubmit={handleSubmit(onSubmit)}>
      <Form.Group>
        <Controller
          name="tags"
          render={({ field }) => (
            <MultiSelect
              closeMenuOnSelect={false}
              isMulti
              options={tagOptions}
              className={classNames({
                'is-invalid': errors.tags
              })}
              {...field}
            />
          )}
          control={control}
        />
        <Form.Control.Feedback type="invalid">
          {errors.tags?.message}
        </Form.Control.Feedback>
      </Form.Group>
      <Button variant="primary" type="submit" className="mt-3" size="sm">
        Submit
      </Button>
    </Form>
  );
};

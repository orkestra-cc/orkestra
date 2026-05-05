
import { Button, Form } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import FalconComponentCard from 'components/common/FalconComponentCard';
import TinymceEditor from 'components/common/TinymceEditor';
import FalconEditor from 'components/common/FalconEditor';
import { Controller, useForm } from 'react-hook-form';
import * as yup from 'yup';
import { yupResolver } from '@hookform/resolvers/yup';

const exampleCode = `function SingleSelectExample() {
  const [value, setValue] = useState(null);
  return(
    <TinymceEditor
      value={value}
      handleChange={newValue => setValue(newValue)}
    />
  )
}`;

const Editor = () => (
  <>
    <PageHeader
      title="Editor"
      description="React-Falcon uses <strong>Tinymce React</strong> for rich text editor. TinyMCE React component integrates TinyMCE into React projects."
      className="mb-3"
    >
      <Button
        href="https://github.com/tinymce/tinymce-react"
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Tinymce React Documentation
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Pre Requirement" noPreview>
        <p className="mt-2 mb-0">
          To use Tinymce editor at first you need to sign up in{' '}
          <a
            href="https://www.tiny.cloud/auth/signup/"
            target="_blank"
            rel="noreferrer"
          >
            Tiny Cloud
          </a>
          . And collect your api key and paste it in .env file variable
          <code> REACT_APP_TINYMCE_APIKEY</code>
        </p>
      </FalconComponentCard.Header>
      <FalconComponentCard.Body>
        <FalconEditor
          code={`REACT_APP_TINYMCE_APIKEY= your_api_key_here`}
          language="bash"
          hidePreview
        />
      </FalconComponentCard.Body>
    </FalconComponentCard>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Self Hosted" noPreview>
        <p className="mt-2 mb-0">
          Please note that we have used{' '}
          <a
            href="https://www.tiny.cloud/docs/tinymce/latest/react-pm-host/"
            target="_blank"
            rel="noreferrer"
          >
            self hosted
          </a>
          <code> tinymce</code> in our Phoenix project. To do this we have added{' '}
          <code>tinymce</code> package in our public directory. Then added{' '}
          <code>tinymceScriptSrc="/tinymce/tinymce.min.js"</code> and{' '}
          <code>license_key: 'gpl'</code> in tinymce editor. If you use{' '}
          <a
            href="https://www.tiny.cloud/auth/signup/"
            target="_blank"
            rel="noreferrer"
          >
            Tiny Cloud
          </a>{' '}
          remove <code>tinymceScriptSrc</code> and <code>license_key</code>.
        </p>
      </FalconComponentCard.Header>
      <FalconComponentCard.Body>
        <FalconEditor
          code={`
            <TinymceEditor 
              tinymceScriptSrc="/tinymce/tinymce.min.js" // remove tinymceScriptSrc if you use tiny cloud
              init={{
                ....
                license_key: 'gpl' // remove license_key if you use tiny cloud
              }}
            />
          `}
          hidePreview
        />
      </FalconComponentCard.Body>
    </FalconComponentCard>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Example" />
      <FalconComponentCard.Body
        code={exampleCode}
        scope={{ TinymceEditor }}
        language="jsx"
      />
    </FalconComponentCard>

    <FalconComponentCard>
      <FalconComponentCard.Header title="With validation" />
      <FalconComponentCard.Body language="jsx" hidePreview>
        <TinymceValidation />
      </FalconComponentCard.Body>
    </FalconComponentCard>
  </>
);

export default Editor;

const schema = yup
  .object({
    description: yup.string().required('This field is required.')
  })
  .required();

const TinymceValidation = () => {
  const {
    handleSubmit,
    control,
    formState: { errors }
  } = useForm({
    resolver: yupResolver(schema)
  });
  const onSubmit = (data: { description: string }) => {
    console.log(data);
  };
  return (
    <Form onSubmit={handleSubmit(onSubmit)}>
      <Controller
        name="description"
        control={control}
        render={({ field }) => (
          <TinymceEditor
            {...field}
            handleChange={field.onChange}
            isInvalid={!!errors.description}
          />
        )}
      />
      <div className="invalid-feedback">{errors.description?.message}</div>
      <Button variant="primary" type="submit" className="mt-3" size="sm">
        Submit
      </Button>
    </Form>
  );
};

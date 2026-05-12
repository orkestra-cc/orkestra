
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import { reactBootstrapDocsUrl } from 'helpers/utils';

const checksExampleCode = `
<>
  <Form.Check 
    type='checkbox'
    id='defaultCheckbox'
    label='Default Checkbox'
  />
  <Form.Check 
    type='checkbox'
    id='checkedCheckbox'
    label='Checked Checkbox'
    defaultChecked
  />
  <Form.Check 
    type='checkbox'
    id='disabledCheckbox'
    label='Disabled Checkbox'
    disabled
  />
</>
`;
const radioExampleCode = `
<>
  <Form.Check 
    type='radio'
    id='defaultRadio'
    label='Default Radios'
    name='radio'
  />
  <Form.Check 
    type='radio'
    id='checkedRadio'
    label='Default Checked Radios'
    defaultChecked
    name='radio'
  />
  <Form.Check 
    type='radio'
    id='disabledRadio'
    label='Disabled Radio'
    disabled
    name='radio'
  />
</>
`;
const switchesCode = `
<>
  <Form.Check 
    type='switch'
    id='defaultSwitch'
    label='Default switch checkbox input'
  />
  <Form.Check 
    type='switch'
    id='checkedSwitch'
    label='Checked switch checkbox input'
    defaultChecked
  />
  <Form.Check 
    type='switch'
    id='defaultDisableSwitch'
    label='Default switch checkbox input'
    disabled 
  />
  <Form.Check 
    type='switch'
    id='checkedDisableSwitch'
    label='Checked switch checkbox input'
    defaultChecked
    disabled
  />
</>
`;
const inlineCheckboxCode = `
<>
  <Form.Check
    inline
    type='checkbox'
    id='item1Check'
    label='Item 1'
  />
  <Form.Check
    inline 
    type='checkbox'
    id='item2Check'
    label='Item 2'
    defaultChecked
  />
  <Form.Check
    inline 
    type='checkbox'
    id='item3Check'
    label='Item 3'
    disabled
  />
</>
`;
const inlineRadioCode = `
<>
  <Form.Check
    inline
    type='radio'
    id='item1Radio'
    label='Item 1'
    name='inline-radio'
  />
  <Form.Check
    inline 
    type='radio'
    id='item2Radio'
    label='Item 2'
    defaultChecked
    name='inline-radio'
  />
  <Form.Check
    inline 
    type='radio'
    id='item3Radio'
    label='Item 3'
    disabled
    name='inline-radio'
  />
</>
`;

const customRenderCode = `
<>
  {['checkbox', 'radio'].map((type) => (
    <div key={type} className="mb-3">
      <Form.Check type={type} id={'check-api-' + type }>
        <Form.Check.Input type={type} isValid />
        <Form.Check.Label className='mb-0'>{'Custom api ' + type }</Form.Check.Label>
        <Form.Control.Feedback type="valid">You did it!</Form.Control.Feedback>
      </Form.Check>
    </div>
  ))}
</>
`;

const Checks = () => (
  <>
    <PageHeader
      title="Checks"
      description="Create consistent cross-browser and cross-device checkboxes and radios with our completely rewritten checks component."
      className="mb-3"
    >
      <Button
        href={`${reactBootstrapDocsUrl}/docs/forms/checks-radios`}
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Checkboxes and Radios on React Bootstrap
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Checks" light={false} />
      <OrkestraComponentCard.Body code={checksExampleCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Radios" light={false} />
      <OrkestraComponentCard.Body code={radioExampleCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Switches" light={false} />
      <OrkestraComponentCard.Body code={switchesCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Check Inline" light={false} />
      <OrkestraComponentCard.Body code={inlineCheckboxCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Radio Inline" light={false} />
      <OrkestraComponentCard.Body code={inlineRadioCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Custom rendering" light={false}>
        <p className="mt-2">
          When you need tighter control, or want to customize how the{' '}
          <code>FormCheck</code> component renders, it may better to use it's
          constituent parts directly.
        </p>
        <p className="mb-0">
          By provided <code>children</code> to the <code>FormCheck</code> you
          can forgo the default rendering and handle it yourself. (You can still
          provide an <code>id</code> to the <code>FormCheck</code> or{' '}
          <code>FormGroup</code> and have it propagate to the label and input).
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={customRenderCode} language="jsx" />
    </OrkestraComponentCard>
  </>
);

export default Checks;


import { Button, Tab } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import { reactBootstrapDocsUrl } from 'helpers/utils';

const exampleCode = `
<>
  <Form.Group className="mb-3" controlId="exampleForm.ControlInput1">
    <Form.Label>Email address</Form.Label>
    <Form.Control type="email" placeholder="name@example.com" />
  </Form.Group>
  <Form.Group className="mb-3" controlId="exampleForm.ControlTextarea1">
    <Form.Label>Example textarea</Form.Label>
    <Form.Control as="textarea" rows={3} />
  </Form.Group>
</>
`;
const sizingCode = `
<>
  <Form.Control size="lg" type="text" placeholder="Large text" />
  <br />
  <Form.Control type="text" placeholder="Normal text" />
  <br />
  <Form.Control size="sm" type="text" placeholder="Small text" />
</>
`;
const readOnlyCode = `
<Form.Control type="text" placeholder="Readonly input here..." readOnly />
`;

const readOnlyTextCode = `
<>
  <Form.Group as={Row} className="mb-3" controlId="formPlaintextEmail">
    <Form.Label column sm="2">
      Email
    </Form.Label>
    <Col sm="10">
      <Form.Control plaintext readOnly defaultValue="email@example.com" />
    </Col>
  </Form.Group>

  <Form.Group as={Row} className="mb-3" controlId="formPlaintextPassword">
    <Form.Label column sm="2">
      Password
    </Form.Label>
    <Col sm="10">
      <Form.Control type="password" placeholder="Password" />
    </Col>
  </Form.Group>
</>
`;

const fileInputCode = `
<>
  <Form.Group controlId="formFile" className="mb-3">
    <Form.Label>Default file input example</Form.Label>
    <Form.Control type="file" />
  </Form.Group>
  <Form.Group controlId="formFileDisabled" className="mb-3">
    <Form.Label>Disabled file input example</Form.Label>
    <Form.Control type="file" disabled />
  </Form.Group>
  <Form.Group controlId="formFileMultiple" className="mb-3">
    <Form.Label>Multiple files input example</Form.Label>
    <Form.Control type="file" multiple />
  </Form.Group>
</>`;

const fileInputSizingCode = `
<>
  <Form.Group controlId="formFileSm" className="mb-3">
    <Form.Label>Small file input example</Form.Label>
    <Form.Control type="file" size="sm" />
  </Form.Group>
  <Form.Group controlId="formFileLg" className="mb-3">
    <Form.Label>Large file input example</Form.Label>
    <Form.Control type="file" size="lg" />
  </Form.Group>
</>
`;

const datalistCode = `
<Form.Group className="mb-3">
  <Form.Label>
    Choose your browser from the list:
  </Form.Label>
  <Form.Control size='sm' id="customDatalist" list="browsers" name="browser" />
  <datalist id="browsers">
    <option value="Edge"> </option>
    <option value="Firefox"> </option>
    <option value="Chrome"> </option>
    <option value="Opera"> </option>
    <option value="Safari"></option>
  </datalist>
</Form.Group>
`;

const FormControl = () => (
  <>
    <PageHeader
      title="Form Controls"
      description="Indicate the current page’s location within a navigational hierarchy that automatically adds separators via CSS."
      className="mb-3"
    >
      <Button
        href={`${reactBootstrapDocsUrl}/docs/forms/form-control/`}
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Form Controls on React Bootstrap
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Example" light={false} />
      <OrkestraComponentCard.Body code={exampleCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Sizing" light={false}>
        <p className="mt-2 mb-0">
          Use <code>size</code> on <code>&lt;FormControl&gt;</code> and{' '}
          <code>&lt;FormLabel&gt;</code> to change the size of inputs and labels
          respectively.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={sizingCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard multiSections>
      <Tab.Container defaultActiveKey="preview">
        <OrkestraComponentCard.Header title="Readonly" light={false}>
          <p className="mt-2 mb-0">
            Add the <code>readOnly</code> prop on an input to prevent
            modification of the input's value. Read-only inputs appear lighter
            (just like disabled inputs), but retain the standard cursor.
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body code={readOnlyCode} language="jsx" />
      </Tab.Container>
      <Tab.Container defaultActiveKey="preview">
        <OrkestraComponentCard.Header title="Readonly plain text" light={false}>
          <p className="mt-2 mb-0">
            If you want to have readonly elements in your form styled as plain
            text, use the <code>plaintext</code> prop on FormControls to remove
            the default form field styling and preserve the correct margin and
            padding.
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body code={readOnlyTextCode} language="jsx" />
      </Tab.Container>
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="File Input" light={false} />
      <OrkestraComponentCard.Body code={fileInputCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="File Input Sizing" light={false} />
      <OrkestraComponentCard.Body code={fileInputSizingCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard noGuttersBottom>
      <OrkestraComponentCard.Header title="Datalist" light={false} />
      <OrkestraComponentCard.Body code={datalistCode} language="jsx" />
    </OrkestraComponentCard>
  </>
);

export default FormControl;

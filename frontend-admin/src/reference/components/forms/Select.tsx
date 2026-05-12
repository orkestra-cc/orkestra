
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import { reactBootstrapDocsUrl } from 'helpers/utils';

const exampleCode = `
<Form.Select aria-label="Default select example">
  <option>Open this select menu</option>
  <option value="1">One</option>
  <option value="2">Two</option>
  <option value="3">Three</option>
</Form.Select>
`;

const sizingCode = `
<>
  <Form.Select size="lg" className="mb-3">
    <option>Large select</option>
    <option value="1">One</option>
    <option value="2">Two</option>
    <option value="3">Three</option>
  </Form.Select>
  <Form.Select className="mb-3">
    <option>Default select</option>
    <option value="1">One</option>
    <option value="2">Two</option>
    <option value="3">Three</option>
  </Form.Select>
  <Form.Select size="sm">
    <option>Small select</option>
    <option value="1">One</option>
    <option value="2">Two</option>
    <option value="3">Three</option>
  </Form.Select>
</>
`;

const Select = () => (
  <>
    <PageHeader
      title="Select"
      description="Customize the native <code>&lt;select&gt;</code> with custom CSS that changes the element’s initial appearance."
      className="mb-3"
    >
      <Button
        href={`${reactBootstrapDocsUrl}/forms/select/`}
        target="_blank" rel="noopener noreferrer"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Select on React Bootstrap
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Example" light={false} />
      <OrkestraComponentCard.Body code={exampleCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Sizing" light={false} />
      <OrkestraComponentCard.Body code={sizingCode} language="jsx" />
    </OrkestraComponentCard>
  </>
);

export default Select;

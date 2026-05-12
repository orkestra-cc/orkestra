
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import { reactBootstrapDocsUrl } from 'helpers/utils';

const exampleCode = `function Example() {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button
        onClick={() => setOpen(!open)}
        aria-controls="example-collapse-text"
        aria-expanded={open}
        variant='orkestra-primary'
        className='mb-3'
      >
        Click
      </Button>
      <Collapse in={open}>
        <div className="border rounded">
          <div className='p-x1'>
            Anim pariatur cliche reprehenderit, enim eiusmod high life accusamus terry richardson ad squid. Nihil anim keffiyeh helvetica, craft beer labore wes anderson cred nesciunt sapiente ea proident.
          </div>
        </div>
      </Collapse>
    </>
  );
}`;

const fadeCode = `function Example() {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button
        onClick={() => setOpen(!open)}
        aria-controls="example-fade-text"
        aria-expanded={open}
        variant='orkestra-primary'
        className='mb-3'
      >
        Toggle text
      </Button>
      <Fade in={open}>
        <div className="border rounded">
          <div className='p-x1'>
            Anim pariatur cliche reprehenderit, enim eiusmod high life accusamus terry richardson ad squid. Nihil anim keffiyeh helvetica, craft beer labore wes anderson cred nesciunt sapiente ea proident.
          </div>
        </div>
      </Fade>
    </>
  );
}`;

const Collapse = () => (
  <>
    <PageHeader
      title="Collapse"
      description="Toggle the visibility of an element or component with toggle animation."
      className="mb-3"
    >
      <Button
        href={`${reactBootstrapDocsUrl}/docs/utilities/transitions/#collapse`}
        target="_blank" rel="noopener noreferrer"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Collapse on React Bootstrap
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Example" light={false}>
        <p className="mt-2 mb-0">
          Add a collapse toggle animation to an element or component.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={exampleCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Fade" light={false}>
        <p className="mt-2 mb-0">
          Add a fade animation to a child element or component.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={fadeCode} language="jsx" />
    </OrkestraComponentCard>
  </>
);

export default Collapse;

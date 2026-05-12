
import { Button, Tab } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import { reactBootstrapDocsUrl } from 'helpers/utils';

const overlayExampleCode = `
function Example() {
  const [show, setShow] = useState(false);
  const target = useRef(null);

  return (
    <>
      <Button ref={target} onClick={() => setShow(!show)}>
        Click me!
      </Button>
      <Overlay target={target.current} show={show} placement="right">
        {(props) => (
          <Tooltip style={{ position: 'fixed' }} id="overlay-example" {...props}>
            My Tooltip
          </Tooltip>
        )}
      </Overlay>
    </>
  );
}
`;
const overlayTriggerCode = `
<OverlayTrigger
  overlay={
    <Tooltip style={{ position: 'fixed' }} id="overlay-trigger-example">
      My Tooltip
    </Tooltip>
  }
>
  <Button>Click me!</Button>
</OverlayTrigger>
`;

const placementCode = `
<>
  {['top', 'right', 'bottom', 'left'].map((placement) => (
    <OverlayTrigger
      key={placement}
      placement={placement}
      overlay={
        <Tooltip style={{ position: 'fixed' }} id={'tooltip-'+ placement}>
          Tooltip on <strong>{placement}</strong>.
        </Tooltip>
      }
    >
      <Button variant="secondary" className='mb-1 me-2'>Tooltip on {placement}</Button>
    </OverlayTrigger>
  ))}
</>
`;

const Tooltips = () => (
  <>
    <PageHeader
      title="Tooltips"
      description="A tooltip component for a more stylish alternative to that anchor tag title attribute."
      className="mb-3"
    >
      <Button
        href={`${reactBootstrapDocsUrl}/docs/components/overlays/#tooltips`}
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Tooltips on React Bootstrap
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Overview" noPreview />
      <OrkestraComponentCard.Body>
        <p>
          The <code>&lt;Tooltip&gt;</code> component do not position themselves.
          Instead the <code>&lt;Overlay&gt;</code> (or{' '}
          <code>&lt;OverlayTrigger&gt;</code>) components, inject{' '}
          <code>ref</code> and <code>style</code> props.
        </p>
        <Button
          href={`${reactBootstrapDocsUrl}/docs/components/overlays`}
          target="_blank"
          variant="link"
          size="sm"
          className="ps-0"
        >
          Learn more about Overlays
          <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
        </Button>
      </OrkestraComponentCard.Body>
    </OrkestraComponentCard>

    <OrkestraComponentCard multiSections>
      <Tab.Container defaultActiveKey="preview">
        <OrkestraComponentCard.Header title="Example" light={false}>
          <p className="mb-0 mt-2">
            You can pass the <code>Overlay</code> injected props directly to the
            Tooltip component.
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body code={overlayExampleCode} language="jsx" />
      </Tab.Container>
      <Tab.Container defaultActiveKey="preview">
        <OrkestraComponentCard.Header light={false}>
          <p className="mb-0 mt-2">
            Or pass a Tooltip element to <code>OverlayTrigger</code> instead.
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body code={overlayTriggerCode} language="jsx" />
      </Tab.Container>
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Placement" light={false}>
        <p className="mb-0 mt-2">
          Use <code>placement</code> prop to set your <code>Tooltip</code>'s
          position.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={placementCode} language="jsx" />
    </OrkestraComponentCard>
  </>
);

export default Tooltips;

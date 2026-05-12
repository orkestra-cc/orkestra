
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import IconButton from 'components/common/IconButton';
import { reactBootstrapDocsUrl } from 'helpers/utils';
import ButtonGroup from './ButtonGroup';
import OrkestraCloseButton from 'components/common/OrkestraCloseButton';
import Flex from 'components/common/Flex';

const faconBtnsCode = `
<>
  <Button variant='orkestra-primary' className='me-2 mb-1'>Primary</Button>
  <Button variant='orkestra-success' className='me-2 mb-1'>Success</Button>
  <Button variant='orkestra-info' className='me-2 mb-1'>Info</Button>
  <Button variant='orkestra-warning' className='me-2 mb-1'>Warning</Button>
  <Button variant='orkestra-danger' className='me-2 mb-1'>Danger</Button>
  <Button variant='orkestra-default' className='me-2 mb-1'>Default</Button>
</>
`;
const solidBtnsCode = `
<>
  <Button variant='primary' className='me-2 mb-1'>Primary</Button>
  <Button variant='secondary ' className='me-2 mb-1'>Secondary</Button>
  <Button variant='success' className='me-2 mb-1'>Success</Button>
  <Button variant='info' className='me-2 mb-1'>Info</Button>
  <Button variant='warning' className='me-2 mb-1'>Warning</Button>
  <Button variant='danger' className='me-2 mb-1'>Danger</Button>
  <Button variant='light' className='me-2 mb-1'>Light</Button>
  <Button variant='dark' className='me-2 mb-1'>Dark</Button>
  <Button variant='link' className='me-2 mb-1'>Link</Button>
</>
`;
const outlineBtnsCode = `
<>
  <Button variant="outline-primary" className="mb-1">Primary</Button>{' '}
  <Button variant="outline-secondary" className="mb-1">Secondary</Button>{' '}
  <Button variant="outline-success" className="mb-1">Success</Button>{' '}
  <Button variant="outline-warning" className="mb-1">Warning</Button>{' '}
  <Button variant="outline-danger" className="mb-1">Danger</Button>{' '}
  <Button variant="outline-info" className="mb-1">Info</Button>{' '}
  <Button variant="outline-light" className="mb-1">Light</Button>{' '}
  <Button variant="outline-dark" className="mb-1">Dark</Button>
</>
`;
const btnSizesCode = `
<>
  <Button variant="secondary" size="sm">Small</Button>{' '}
  <Button variant="secondary">Regular</Button>{' '}
  <Button variant="secondary" size="lg">Large</Button>{' '}
</>
`;
const iconBtnCode = `
<>
  <IconButton
    className="me-2 mb-1"
    variant="orkestra-default"
    size="sm"
    icon="plus"
    transform="shrink-3"
  >
    Small
  </IconButton>
  <IconButton className="me-2 mb-1" variant="orkestra-default" icon="plus" transform="shrink-3">
    Regular
  </IconButton>
  <IconButton className="mb-1" variant="orkestra-default" size="lg" icon="plus" transform="shrink-3">
    Large
  </IconButton>
  <hr />
  <IconButton variant="primary" className="me-2 mb-1" icon="plus" transform="shrink-3">
    Regular
  </IconButton>
  <IconButton variant="outline-primary" className="mb-1" icon="plus" transform="shrink-3">
    Outline
  </IconButton>
  <hr />
  <IconButton variant="primary" icon="trash" className="mb-1" iconAlign="right" transform="shrink-3">
    Delete
  </IconButton>
</>
`;

const roundedBtnCode = `
<>
  <Button className="me-2" variant="orkestra-default" className="rounded-pill me-1 mb-1">
    Example
  </Button>
  <IconButton
    className="rounded-pill me-1 mb-1"
    variant="orkestra-default"
    icon="align-left"
    transform="shrink-3"
  >
    Icon Left
  </IconButton>
  <IconButton
    className="rounded-pill me-1 mb-1"
    variant="orkestra-default"
    icon="align-right"
    iconAlign="right"
    transform="shrink-3"
  >
    Icon Right
  </IconButton>
  <Button variant="outline-primary" className="rounded-pill me-1 mb-1">
    Outline
  </Button>
  <hr />
  <Button variant="orkestra-default" className="rounded-pill me-2 mb-1" size="sm">
    Capsule Small
  </Button>
  <Button variant="orkestra-default" className="rounded-pill me-2 mb-1">
    Capsule Regular
  </Button>
  <Button variant="orkestra-default" className="rounded-pill me-2 mb-1" size="lg">
    Capsule large
  </Button>
</>
`;

const blockBtnCode = `
<div className="d-grid gap-2">
  <Button variant="primary">
    Block button
  </Button>
  <Button variant="secondary">
    Block button
  </Button>
</div>
`;

const activeStateCode = `
<>
  <Button variant="primary" className='me-2' active>
    Primary button
  </Button>
  <Button variant="secondary" active>
    Button
  </Button>
</>
`;
const disableStateCode = `
<>
  <Button variant="primary" className='me-2' disabled>
    Primary button
  </Button>
  <Button variant="secondary" disabled>
    Button
  </Button>
</>
`;
const loadingStateCode = `
function LoadingButton() {
  const [isLoading, setLoading] = useState(false);

  useEffect(() => {
    if (isLoading) {
      setTimeout(() => {
        setLoading(false);
      }, 2000);
    }
  }, [isLoading]);

  const handleClick = () => setLoading(true);

  return (
    <Button variant="primary" disabled={isLoading} onClick={!isLoading ? handleClick : null}>
      {isLoading ? 'Loading…' : 'Click to load'}
    </Button>
  );
}`;

const closeBtnCode = `<>
  <CloseButton aria-label="Hide"/>
  <CloseButton disabled aria-label="Hide"/>
</>`;

const closeBtnWhiteCode = `<div className="bg-1000 p-3" data-bs-theme="light">
  <CloseButton variant="white" aria-label="Hide"/>
  <CloseButton variant="white" disabled aria-label="Hide"/>
</div>`;

const orkestraCloseBtnCode = `
<>
  <OrkestraCloseButton
    size='lg'
    className='me-2'
  />
  <OrkestraCloseButton
    noOutline
    className='me-2'
  />
  <OrkestraCloseButton
    size='sm'
  />
</>
`;

const Buttons = () => (
  <>
    <PageHeader
      title="Buttons"
      description="Use Orkestra custom button styles for actions in forms, dialogs, and more with support for multiple sizes, states, and more."
      className="mb-3"
    >
      <Button
        href={`${reactBootstrapDocsUrl}/docs/components/buttons`}
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Buttons on React Bootstrap
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Orkestra Buttons" light={false} />
      <OrkestraComponentCard.Body code={faconBtnsCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Solid Buttons" light={false} />
      <OrkestraComponentCard.Body code={solidBtnsCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Outline Buttons" light={false}>
        <p className="mb-0">
          For a lighter touch, Buttons also come in <code>outline-* </code>
          variants with no background color.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={outlineBtnsCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Close Button" light={false}>
        <p className="mt-2 mb-0">
          To ensure the maximum accessibility for Close Button components, it is
          recommended that you provide relevant text for screen readers. The
          example below provides an example of accessible usage of this
          component by way of the
          <code> aria-label </code> property.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={closeBtnCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header
        title="Close Buttons white variant"
        light={false}
      >
        <p className="mt-2 mb-0">
          Change the default dark color to white using{' '}
          <code>variant="white"</code>.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={closeBtnWhiteCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Orkestra Close Button" light={false}>
        <p className="mb-0">
          Orkestra close button is properly optimized for both light and dark
          mode.To use orkestra close button wrap{' '}
          <code> &lt;FlaconCloseButton&gt; </code> inside a{' '}
          <code> position-relative </code> element.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={orkestraCloseBtnCode}
        language="jsx"
        scope={{ OrkestraCloseButton, Flex }}
      />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Button Sizes" light={false}>
        <p>
          Fancy larger or smaller buttons? Add <code>size="lg"</code>,
          <code>size="sm"</code> for additional sizes.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={btnSizesCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Icon Buttons" light={false} />
      <OrkestraComponentCard.Body
        code={iconBtnCode}
        language="jsx"
        scope={{ FontAwesomeIcon, IconButton }}
      ></OrkestraComponentCard.Body>
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Capsule Buttons" light={false} />
      <OrkestraComponentCard.Body
        code={roundedBtnCode}
        language="jsx"
        scope={{ FontAwesomeIcon, IconButton }}
      />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Block buttons" light={false}>
        <p className="mb-0">
          Create responsive stacks of full-width, “block buttons” like those in
          Bootstrap 4 with a mix of our display and gap utilities.{' '}
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={blockBtnCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Active state" light={false}>
        <p className="mb-0">
          To set a button's active state simply set the component's{' '}
          <code> active</code> prop.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={activeStateCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Disabled state" light={false}>
        <p className="mb-0">
          Make buttons look inactive by adding the <code>disabled</code> prop
          to.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={disableStateCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Button loading state" light={false}>
        <p className="mb-0">
          When activating an asynchronous action from a button it is a good UX
          pattern to give the user feedback as to the loading state, this can
          easily be done by updating your <code>&lt;Button/&gt;</code>s props
          from a state change like below.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={loadingStateCode} language="jsx" />
    </OrkestraComponentCard>
    <ButtonGroup />
  </>
);

export default Buttons;

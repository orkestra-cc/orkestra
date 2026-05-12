
import { Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import PageHeader from 'components/common/PageHeader';
import OrkestraEditor from 'components/common/OrkestraEditor';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import { useAppContext } from 'providers/AppProvider';

const comboCode = `settings:{
  // ...rest
  navbarPosition:'combo'
}`;

const ComboNavbar = () => {
  const {
    config: { navbarPosition },
    setConfig
  } = useAppContext();

  return (
    <>
      <PageHeader
        title="Combo Nav"
        description="Combo Nav is an additional layout system of Orkestra where you can place both Navbar Top and Navbar Vertical in a same page."
        className="mb-3"
      >
        <Button
          onClick={() =>
            setConfig(
              'navbarPosition',
              navbarPosition === 'vertical' ? 'combo' : 'vertical'
            )
          }
          variant="link"
          size="sm"
          className="ps-0"
        >
          Toggle Combo Nav
          <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
        </Button>
      </PageHeader>

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="How to" noPreview />
        <OrkestraComponentCard.Body>
          <p>
            Combo layout uses Orkestra's{' '}
            <a href="/components/navs-and-tabs/vertical-navbar" target="_blank">
              Navbar vertical{' '}
            </a>
            and{' '}
            <a href="/components/navs-and-tabs/top-navbar" target="_blank">
              Navbar top
            </a>
            .
          </p>
          <p>
            To enable <strong> Combo Nav </strong>
            clear your browser's localstorage then from your project directory
            go to,
            <code>src/config.js</code> and set
            <code> navbarPosition:'combo' </code> of <code>settings</code>{' '}
            object.
          </p>
          <OrkestraEditor code={comboCode} language="js" hidePreview />
        </OrkestraComponentCard.Body>
      </OrkestraComponentCard>

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Behaviors" noPreview />
        <OrkestraComponentCard.Body>
          <p>
            For responsive collapsing pass{' '}
            <code>{`expand = {'sm | md | lg | xl'}`}</code> prop to
            React-Bootstrap's <code>Navbar</code> component.
          </p>
        </OrkestraComponentCard.Body>
      </OrkestraComponentCard>
    </>
  );
};

export default ComboNavbar;


import { Button, Card } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import PageHeader from 'components/common/PageHeader';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import { reactBootstrapDocsUrl } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';

const NavbarDoubleTop = () => {
  const {
    config: { navbarPosition },
    setConfig
  } = useAppContext();

  return (
    <>
      <PageHeader
        title="Navbar Double Top"
        description="Navbar Double Top is a different user friendly layout system in Orkestra. You can start developing with Navbar Double Top layout with the starter page."
        className="mb-3"
      >
        <Button
          onClick={() =>
            setConfig(
              'navbarPosition',
              navbarPosition === 'vertical' ? 'double-top' : 'vertical'
            )
          }
          variant="link"
          size="sm"
          className="ps-0"
        >
          Toggle Navbar Double Top
          <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
        </Button>
      </PageHeader>
      <Card className="mb-3">
        <OrkestraCardHeader title="Supported Content" light={false} />
        <Card.Body className="bg-body-tertiary">
          <p>
            Orkestra Navbar Double Top support all of
            <a href={`${reactBootstrapDocsUrl}/docs/components/navbar/`}>
              {' '}
              React-Bootstrap Navbar{' '}
            </a>
            components. <code>Navbar</code>, <code>Navbar.Toggle</code>,{' '}
            <code>Navbar.Brand</code>,<code>Navbar.Collapse</code>,
            <code>Nav</code> all of those sub-components are used in Navbar
            Double Top.
          </p>
        </Card.Body>
      </Card>
      <Card className="mb-3">
        <OrkestraCardHeader title="Behaviors" light={false} />
        <Card.Body className="bg-body-tertiary">
          <p>
            Orkestra Navbar Double Top uses
            <a href={`${reactBootstrapDocsUrl}/docs/components/navbar/`}>
              {' '}
              React-Bootstrap Navbar{' '}
            </a>
            responsive behaviors and all other behavior they support. The
            dropdown menu display onClick by default on react-bootstrap. Orkestra
            navbar top dropdown menu display on hover. To achieve this behavior,
            we use react <code>onMouseOver</code> Event and{' '}
            <code>onMouseLeave</code> event at{' '}
            <code>src/components/navbar/NavbarDropdown.js</code> jsx tag.
          </p>
        </Card.Body>
      </Card>
      <Card className="mb-3">
        <OrkestraCardHeader title="Color Schemes" light={false} />
        <Card.Body className="bg-body-tertiary">
          <p>
            Changing the color of Orkestra Navbar Double Top is very easy. Orkestra
            uses React-Bootstrap's default <code> variant='light' </code> for
            navabr component. You can use other background-color utilitie with{' '}
            <code>bg</code> prop to update the Navbar. Learn more about
            React-Bootstrap Navbar{' '}
            <a
              href={`${reactBootstrapDocsUrl}/docs/components/navbar/#navbars-colors`}
              target="_blank"
              rel="noopener noreferrer"
            >
              Color Schemes.
            </a>
          </p>
        </Card.Body>
      </Card>
    </>
  );
};

export default NavbarDoubleTop;

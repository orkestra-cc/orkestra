import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import PageHeader from 'components/common/PageHeader';
import GoogleMap from 'components/map/GoogleMap';

import { Button, Card } from 'react-bootstrap';

const exampleCode = `<GoogleMap
  initialCenter={{
    lat: 48.8583736,
    lng: 2.2922926
  }}
  mapStyle="Default"
  darkStyle="Cobalt"
  className="vh-50"
>
  <h5 className="text-700">Eiffel Tower</h5>
  <p className="fs-10 mb-0">
    Gustave Eiffel's iconic, wrought-iron 1889 tower,
    <br /> with steps and elevators to observation decks.
  </p>
</GoogleMap>`;
const colorScehemCode = `<GoogleMap
  initialCenter={{
    lat: 48.8583736,
    lng: 2.2922926
  }}
  mapStyle="AssassianCreed"
  darkStyle="Cobalt"
  className="vh-50"
/>`;

const GoogleMapExample = () => {
  return (
    <>
      <PageHeader
        title="React Google Maps Api"
        description="A declarative React Google Maps Api component using React, lazy-loading dependencies, current-location finder and a test-driven approach by the Fullstack React team."
        className="mb-3"
      >
        <Button
          href="https://react-google-maps-api-docs.netlify.app/"
          target="_blank"
          variant="link"
          size="sm"
          className="ps-0"
        >
          React Google Maps Api Documentation
          <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
        </Button>
      </PageHeader>

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Example" />
        <OrkestraComponentCard.Body
          code={exampleCode}
          scope={{ GoogleMap }}
          language="jsx"
        />
      </OrkestraComponentCard>

      <Card className="mb-3">
        <Card.Header>
          <h5 className="mb-0">Map color schemes</h5>
        </Card.Header>
        <Card.Body className="bg-body-tertiary">
          <p>
            Pass any of the following value as <code>mapStyle</code> prop to{' '}
            <code>GoogleMap</code> component for different color scheme:
          </p>
          <ul>
            <li>
              <code>Default</code>
            </li>
            <li>
              <code>Gray</code>
            </li>
            <li>
              <code>Midnight</code>
            </li>
            <li>
              <code>Hopper</code>
            </li>
            <li>
              <code>Beard</code>
            </li>
            <li>
              <code>AssassianCreed</code>
            </li>
            <li>
              <code>SubtleGray</code>
            </li>
            <li>
              <code>Tripitty</code>
            </li>
          </ul>
        </Card.Body>
      </Card>

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Example">
          <p className="mt-2 mb-0">
            With <code>AssassianCreed</code> scheme
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body
          code={colorScehemCode}
          scope={{ GoogleMap }}
          language="jsx"
        />
      </OrkestraComponentCard>
    </>
  );
};

export default GoogleMapExample;


import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import CountUp from 'react-countup';

const exampleCode = `<Row className="g-3">
  <Col xl xs={6} md={4} className="text-center">
    <h4 className="fs-5 fw-medium text-700">
      <CountUp end={36487} duration={2.75} separator="," />
    </h4>
    <h6>(Default)</h6>
  </Col>
  <Col xl xs={6} md={4} className="text-center">
    <h4 className="fs-5 fw-medium text-700">
      <CountUp end={23542} duration={10} separator="," />
    </h4>
    <h6>(Duration 10s)</h6>
  </Col>
  <Col xl xs={6} md={4} className="text-center">
    <h4 className="fs-5 fw-medium text-700">
      <CountUp end={36487} duration={2.75} />
    </h4>
    <h6>(No Separator)</h6>
  </Col>
  <Col xl xs={6} md={4} className="text-center">
    <h4 className="fs-5 fw-medium text-700">
      <CountUp end={36487} duration={2.75} separator=" " />
    </h4>
    <h6>(Space)</h6>
  </Col>
  <Col xl xs={6} md={4} className="text-center">
    <h4 className="fs-5 fw-medium text-700">
      <CountUp end={36.487} duration={2.75} suffix="k" decimals={2} />
    </h4>
    <h6>(Suffix)</h6>
  </Col>
  <Col xl xs={6} md={4} className="text-center">
    <h4 className="fs-5 fw-medium text-700">
      <CountUp end={36487} duration={2.75} prefix="$" separator="," />
    </h4>
    <h6>(Prefix)</h6>
  </Col>
</Row>`;

const Countup = () => (
  <>
    <PageHeader
      title="React CountUp"
      description="A configurable React component wrapper around CountUp.js."
      className="mb-3"
    >
      <Button
        href="https://github.com/glennreyes/react-countup"
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        React CountUp Documentation
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Example" light={false} />
      <OrkestraComponentCard.Body
        code={exampleCode}
        scope={{ CountUp }}
        language="jsx"
      />
    </OrkestraComponentCard>
  </>
);

export default Countup;

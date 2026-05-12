
import { Button, Row, Col } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import { reactBootstrapDocsUrl } from 'helpers/utils';

const exampleCode = `
  <Pagination>
    <Pagination.Prev >Previous</Pagination.Prev>
    <Pagination.Item active>1</Pagination.Item>
    <Pagination.Item>2</Pagination.Item>
    <Pagination.Item>3</Pagination.Item>
    <Pagination.Next>Next</Pagination.Next>
  </Pagination>
`;
const iconExampleCode = `
  <Pagination>
    <Pagination.First />
    <Pagination.Item>1</Pagination.Item>
    <Pagination.Item active>2</Pagination.Item>
    <Pagination.Item>3</Pagination.Item>
    <Pagination.Last />
  </Pagination>
`;
const sizingCode = `
<>
  <Pagination size='lg'>
    <Pagination.Item active>1</Pagination.Item>
    <Pagination.Item>2</Pagination.Item>
    <Pagination.Item>3</Pagination.Item>
  </Pagination>
  <Pagination>
    <Pagination.Item active>1</Pagination.Item>
    <Pagination.Item>2</Pagination.Item>
    <Pagination.Item>3</Pagination.Item>
  </Pagination>
  <Pagination size='sm'>
    <Pagination.Item active>1</Pagination.Item>
    <Pagination.Item>2</Pagination.Item>
    <Pagination.Item>3</Pagination.Item>
  </Pagination>
</>
`;

const moreOptionsCode = `
<Pagination>
  <Pagination.First />
  <Pagination.Prev />
  <Pagination.Item>{1}</Pagination.Item>
  
  <Pagination.Item active>{2}</Pagination.Item>
  <Pagination.Item>{3}</Pagination.Item>
  <Pagination.Item>{4}</Pagination.Item>

  <Pagination.Ellipsis />
  <Pagination.Item>{20}</Pagination.Item>
  <Pagination.Next />
  <Pagination.Last />
</Pagination>
`;

const Pagin = () => (
  <>
    <PageHeader
      title="Pagination"
      description="Documentation and examples for showing pagination to indicate a series of related content exists across multiple pages."
      className="mb-3"
    >
      <Button
        href={`${reactBootstrapDocsUrl}/docs/components/pagination`}
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Pagination on React Bootstrap
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>
    <Row className="mb-3 g-3">
      <Col lg={6}>
        <OrkestraComponentCard noGuttersBottom>
          <OrkestraComponentCard.Header title="Example with text" light={false} />
          <OrkestraComponentCard.Body code={exampleCode} language="jsx" />
        </OrkestraComponentCard>
      </Col>
      <Col lg={6}>
        <OrkestraComponentCard noGuttersBottom>
          <OrkestraComponentCard.Header title="Example with Icon" light={false} />
          <OrkestraComponentCard.Body code={iconExampleCode} language="jsx" />
        </OrkestraComponentCard>
      </Col>
    </Row>
    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Sizing" light={false}>
        <p className="mb-0 mt-2">
          Fancy larger or smaller pagination? Add <code> size='lg' </code> or{' '}
          <code> size='sm' </code> props for additional sizes.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={sizingCode} language="jsx" />
    </OrkestraComponentCard>
    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="More Options" light={false}>
        <p className="mb-0 mt-2">
          For building more complex pagination UI, there are few convenient
          sub-components for adding "First", "Previous", "Next", and "Last"
          buttons, as well as an <code>Ellipsis</code> item for indicating
          previous or continuing results.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={moreOptionsCode} language="jsx" />
    </OrkestraComponentCard>
  </>
);

export default Pagin;

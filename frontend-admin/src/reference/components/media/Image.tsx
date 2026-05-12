
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import { reactBootstrapDocsUrl } from 'helpers/utils';
import user1 from 'assets/img/team/1.jpg';
import user2 from 'assets/img/team/2.jpg';
import user3 from 'assets/img/team/3.jpg';
import user24 from 'assets/img/team/24.jpg';
import user25 from 'assets/img/team/25.jpg';
import generic3 from 'assets/img/generic/9.jpg';
import Flex from 'components/common/Flex';

const shapeCode = `
<Container>
  <Row>
    <Col xs={6} md={4}>
      <Image src={user1} height='200px' width='200px' />
    </Col>
    <Col xs={6} md={4}>
      <Image src={user2} roundedCircle height='200px' width='200px'  />
    </Col>
    <Col xs={6} md={4}>
      <Image src={user3} thumbnail height='200px' width='200px'  />
    </Col>
  </Row>
</Container>
`;

const fluidCode = `
<Image src={generic3} fluid />
`;
const aligningCode = `
  <Flex justifyContent="between">
    <Image src={user24} rounded className="w-25" />
    <Image src={user25} rounded className="w-25" />
  </Flex>
`;

const Images = () => (
  <>
    <PageHeader
      title="Images"
      description="Documentation and examples for opting images into responsive behavior (so they never become larger than their parent elements) and add lightweight styles to them—all via classes."
      className="mb-3"
    >
      <Button
        href={`${reactBootstrapDocsUrl}/docs/components/images`}
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Images on React Bootstrap
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Shape" light={false}>
        <p className="mt-2 mb-0">
          Use the <code>rounded</code>, <code>roundedCircle</code> and{' '}
          <code>thumbnail</code> props to customise the image.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={shapeCode}
        scope={{ user1, user2, user3 }}
        language="jsx"
      />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Fluid" light={false}>
        <p className="mt-2 mb-0">
          Use the <code>fluid</code> to scale image nicely to the parent
          element.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={fluidCode}
        scope={{ generic3 }}
        language="jsx"
      />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Aligning images" light={false} />
      <OrkestraComponentCard.Body
        code={aligningCode}
        scope={{ user24, user25, Flex }}
        language="jsx"
      />
    </OrkestraComponentCard>
  </>
);

export default Images;

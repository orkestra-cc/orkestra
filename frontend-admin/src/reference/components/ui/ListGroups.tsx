
import { Button, Row, Col } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import SubtleBadge from 'components/common/SubtleBadge';
import { reactBootstrapDocsUrl } from 'helpers/utils';
import Flex from 'components/common/Flex';
import { Link } from 'react-router';

const exampleCode = `<ListGroup>
  <ListGroup.Item>News Feed</ListGroup.Item>
  <ListGroup.Item>Messages</ListGroup.Item>
  <ListGroup.Item>Events</ListGroup.Item>
  <ListGroup.Item>Groups</ListGroup.Item>
  <ListGroup.Item>Pages</ListGroup.Item>
</ListGroup>`;

const activeExampleCode = `<ListGroup as="ul">
  <ListGroup.Item as="li" active>
    News Feed
  </ListGroup.Item>
  <ListGroup.Item as="li">Messages</ListGroup.Item>
  <ListGroup.Item as="li" disabled>
    Events
  </ListGroup.Item>
  <ListGroup.Item as="li">Groups</ListGroup.Item>
  <ListGroup.Item as="li">Pages</ListGroup.Item>
</ListGroup>`;

const actionableCode = `function ActionableListItems() {
  const alertClicked = () => {
    alert('You clicked the third ListGroup.Item');
  };

  return (
    <ListGroup defaultActiveKey="#link1">
      <ListGroup.Item action href="#link1">
        Link 1
      </ListGroup.Item>
      <ListGroup.Item action href="#link2" disabled>
        Link 2
      </ListGroup.Item>
      <ListGroup.Item action onClick={alertClicked}>
        This one is a button
      </ListGroup.Item>
      <ListGroup.Item action href="#link3">
        Link 3
      </ListGroup.Item>
      <ListGroup.Item action href="#link4">
        Link 4
      </ListGroup.Item>
    </ListGroup>
  );
}`;

const flushCode = `<ListGroup variant="flush">
  <ListGroup.Item>Cras justo odio</ListGroup.Item>
  <ListGroup.Item>Dapibus ac facilisis in</ListGroup.Item>
  <ListGroup.Item>Morbi leo risus</ListGroup.Item>
  <ListGroup.Item>Porta ac consectetur ac</ListGroup.Item>
</ListGroup>`;

const badgeCode = `<ListGroup>
  <ListGroup.Item>
    <Flex justifyContent="between" alignItems="center">
      Messages
      <SubtleBadge pill bg='primary' className='me-2'>14</SubtleBadge>
    </Flex>
  </ListGroup.Item>
  <ListGroup.Item>
    <Flex justifyContent="between" alignItems="center">
      Events
      <SubtleBadge pill bg='primary' className='me-2'>2</SubtleBadge>
    </Flex>
  </ListGroup.Item>
  <ListGroup.Item>
    <Flex justifyContent="between" alignItems="center">
      Groups
      <SubtleBadge pill bg='primary' className='me-2'>1</SubtleBadge>
    </Flex>
  </ListGroup.Item>
  <ListGroup.Item>
    <Flex justifyContent="between" alignItems="center">
      Pages
      <SubtleBadge pill bg='primary' className='me-2'>9</SubtleBadge>
    </Flex>
  </ListGroup.Item>
  <ListGroup.Item>
    <Flex justifyContent="between" alignItems="center">
      Notifications
      <SubtleBadge pill bg='primary' className='me-2'>7</SubtleBadge>
    </Flex>
  </ListGroup.Item>
  <ListGroup.Item>
    <Flex justifyContent="between" alignItems="center">
      Others
      <SubtleBadge pill bg='primary' className='me-2'>11</SubtleBadge>
    </Flex>
  </ListGroup.Item>
</ListGroup>`;

const horizontalCode = `<ListGroup horizontal>
  <ListGroup.Item>This</ListGroup.Item>
  <ListGroup.Item>ListGroup</ListGroup.Item>
  <ListGroup.Item>renders</ListGroup.Item>
  <ListGroup.Item>horizontally!</ListGroup.Item>
</ListGroup>`;

const responsiveHorizontalCode = `['sm', 'md', 'lg', 'xl', 'xxl'].map((breakpoint, idx) => (
  <ListGroup horizontal={breakpoint} className="my-2" key={idx}>
    <ListGroup.Item>This ListGroup</ListGroup.Item>
    <ListGroup.Item>renders horizontally</ListGroup.Item>
    <ListGroup.Item>on {breakpoint}</ListGroup.Item>
    <ListGroup.Item>and above!</ListGroup.Item>
  </ListGroup>
));`;

const listGroupBackgroundCode = `<ListGroup>
  <ListGroup.Item className='py-3'>Example with background</ListGroup.Item>
  <ListGroup.Item className='py-3' variant="primary">A simple Primary list group item</ListGroup.Item>
  <ListGroup.Item className='py-3' variant="secondary">A simple Secondary list group item</ListGroup.Item>
  <ListGroup.Item className='py-3' variant="success">A simple Success list group item</ListGroup.Item>
  <ListGroup.Item className='py-3' variant="danger">A simple Danger list group item</ListGroup.Item>
  <ListGroup.Item className='py-3' variant="warning">A simple Warning list group item</ListGroup.Item>
  <ListGroup.Item className='py-3' variant="info">A simple Info list group item</ListGroup.Item>
  <ListGroup.Item className='py-3' variant="light">A simple Light list group item</ListGroup.Item>
  <ListGroup.Item className='py-3' variant="dark">A simple Dark list group item</ListGroup.Item>
</ListGroup>`;

const linkCode = `<ListGroup>
  <ListGroup.Item className='p-3 p-sm-4' data-bs-theme="light" as={Link} to='#!' action active>
    <Flex justifyContent="between">
      <h5 className="mb-1 text-white">List group · Bootstrap</h5>
      
      <small>3 days ago</small>
    </Flex>
    <p className="mb-1">
      The most basic list group is an unordered list with list items and the proper classes.
      Build upon it with the options that follow, or with your own CSS as needed.{' '}
    </p>
    <small> The most basic list group</small>
  </ListGroup.Item>
  <ListGroup.Item className='p-3 p-sm-4' as={Link} to='#!' action>
    <Flex justifyContent="between">
      <h5 className="mb-1">What is list group?</h5>
      <small className="text-muted">3 days ago</small>
    </Flex>
    <p className="mb-1">
      Creating List Groups with Bootstrap. The list groups are very useful and flexible
      component for displaying lists of elements in a beautiful manner.
    </p>
    <small className="text-muted">Donec id elit non mi porta.</small>
  </ListGroup.Item>
  <ListGroup.Item className='p-3 p-sm-4' as={Link} to='#!' action>
    <Flex justifyContent="between">
      <h5 className="mb-1">What is ordered list?</h5>
      <small>3 days ago</small>
    </Flex>
    <p className="mb-1">
      An ordered list typically is a numbered list of items. HTML 3.0 gives you the ability to
      control the sequence number - to continue where the previous list left off, or to start at
      a particular number.
    </p>
    <small className="text-muted">An ordered list</small>
  </ListGroup.Item>
</ListGroup>`;

const ListGroups = () => (
  <>
    <PageHeader
      title="List Group"
      description="List groups are a flexible and powerful component for displaying a series of content. Modify and extend them to support just about any content within."
      className="mb-3"
    >
      <Button
        href={`${reactBootstrapDocsUrl}/docs/components/list-group`}
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        List groups on React Bootstrap
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <Row className="g-3 mb-3">
      <Col lg={6}>
        <OrkestraComponentCard noGuttersBottom>
          <OrkestraComponentCard.Header title="Example" light={false} />
          <OrkestraComponentCard.Body code={exampleCode} language="jsx" />
        </OrkestraComponentCard>
      </Col>
      <Col lg={6}>
        <OrkestraComponentCard noGuttersBottom>
          <OrkestraComponentCard.Header title="Active items" light={false} />
          <OrkestraComponentCard.Body code={activeExampleCode} language="jsx" />
        </OrkestraComponentCard>
      </Col>
    </Row>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Actionable items" light={false}>
        <p className="mt-2 mb-2">
          Toggle the <code>action</code> prop to create <em>actionable</em> list
          group items, with disabled, hover and active styles. List item actions
          will render a <code>&lt;button&gt;</code> or <code>&lt;a&gt;</code>{' '}
          (depending on the presence of an <code>href</code>) by default but can
          be overridden by setting the <code>as</code> prop as usual.
        </p>
        <p className="mb-0">
          List items <code>actions</code> are distinct from plain items to
          ensure that click or tap affordances aren't applied to non-interactive
          items.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={actionableCode} language="jsx" />
    </OrkestraComponentCard>

    <Row className="g-3 mb-3">
      <Col lg={6}>
        <OrkestraComponentCard noGuttersBottom>
          <OrkestraComponentCard.Header title="List group flush" light={false}>
            <p className="mt-2 mb-0">
              Add the <code>flush</code> variant to remove outer borders and
              rounded corners to render list group items edge-to-edge in a
              parent container such as a <code>Card</code>.
            </p>
          </OrkestraComponentCard.Header>
          <OrkestraComponentCard.Body code={flushCode} language="jsx" />
        </OrkestraComponentCard>
      </Col>
      <Col lg={6}>
        <OrkestraComponentCard noGuttersBottom>
          <OrkestraComponentCard.Header
            title="List group with badge"
            light={false}
          />
          <OrkestraComponentCard.Body
            code={badgeCode}
            language="jsx"
            scope={{ Flex, SubtleBadge }}
          />
        </OrkestraComponentCard>
      </Col>
    </Row>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Horizontal List Group" light={false}>
        <p className="mt-2 mb-0">
          Use the <code>horizontal</code> prop to make the ListGroup render
          horizontally. Currently
          <strong>
            horizontal list groups cannot be combined with flush list groups.
          </strong>
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={horizontalCode} language="jsx" />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header
        title="Responsive Horizontal List Group"
        light={false}
      >
        <p className="mb-0 mt-2">
          There are responsive variants to <code>horizontal</code>: setting it
          to
          <code> sm | md | lg | xl | xxl </code> makes the list group horizontal
          starting at that breakpoint’s <code>min-width</code>.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={responsiveHorizontalCode}
        language="jsx"
      />
    </OrkestraComponentCard>

    <Row className="g-3 mb-3">
      <Col lg={6}>
        <OrkestraComponentCard noGuttersBottom>
          <OrkestraComponentCard.Header
            title="List Group Background"
            light={false}
          />
          <OrkestraComponentCard.Body
            code={listGroupBackgroundCode}
            language="jsx"
          />
        </OrkestraComponentCard>
      </Col>
      <Col lg={6}>
        <OrkestraComponentCard noGuttersBottom>
          <OrkestraComponentCard.Header
            title="List Group With Link"
            light={false}
          />
          <OrkestraComponentCard.Body
            code={linkCode}
            language="jsx"
            scope={{ Flex, Link }}
          />
        </OrkestraComponentCard>
      </Col>
    </Row>
  </>
);

export default ListGroups;

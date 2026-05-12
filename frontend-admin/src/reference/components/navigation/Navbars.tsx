
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import { reactBootstrapDocsUrl } from 'helpers/utils';

const navbarCode = `
  <>
    <Navbar bg="dark" variant="dark" expand="lg" data-bs-theme="dark" className="mb-3">
      <Navbar.Brand href="#">Navbar</Navbar.Brand>
      <Navbar.Toggle aria-controls="navbarScroll" />
      <Navbar.Collapse id="navbarScroll">
        <Nav className="me-auto my-2 my-lg-0">
          <Nav.Link href="#action1">Home</Nav.Link>
          <Nav.Link href="#action2">Link</Nav.Link>
          <NavDropdown title="Dropdown" id="navbarScrollingDropdown">
            <div className='py-2 bg-body-tertiary dark__bg-1000 rounded-3'>
              <NavDropdown.Item href="#action3">Action</NavDropdown.Item>
              <NavDropdown.Item href="#action4">Another action</NavDropdown.Item>
              <NavDropdown.Divider />
              <NavDropdown.Item href="#action5">Something else here</NavDropdown.Item>
            </div>
          </NavDropdown>
          <Nav.Link href="#" disabled>
            Disabled
          </Nav.Link>
        </Nav>
        <Form className="d-flex dark">
          <FormControl
            type="search"
            placeholder="Search"
            className="me-2"
            aria-label="Search"
          />
          <Button variant="light">Search</Button>
        </Form>
      </Navbar.Collapse>
    </Navbar>
    <Navbar bg="primary" variant="light" expand="lg" data-bs-theme="dark" className="mb-3">
      <Navbar.Brand href="#">Navbar</Navbar.Brand>
      <Navbar.Toggle aria-controls="navbarScroll" />
      <Navbar.Collapse id="navbarScroll">
        <Nav className="me-auto my-2 my-lg-0">
          <Nav.Link href="#action1">Home</Nav.Link>
          <Nav.Link href="#action2">Link</Nav.Link>
          <NavDropdown title="Dropdown" id="navbarScrollingDropdown">
            <div className='py-2 bg-body-tertiary dark__bg-1000 rounded-3'>
              <NavDropdown.Item href="#action3">Action</NavDropdown.Item>
              <NavDropdown.Item href="#action4">Another action</NavDropdown.Item>
              <NavDropdown.Divider />
              <NavDropdown.Item href="#action5">Something else here</NavDropdown.Item>
            </div>
          </NavDropdown>
          <Nav.Link href="#" disabled>
            Disabled
          </Nav.Link>
        </Nav>
        <Form className="d-flex dark">
          <FormControl
            type="search"
            placeholder="Search"
            className="me-2"
            aria-label="Search"
          />
          <Button variant="light">Search</Button>
        </Form>
      </Navbar.Collapse>
    </Navbar>
    <Navbar bg="primary-subtle" variant="light" expand="lg" data-bs-theme="light">
      <Navbar.Brand href="#">Navbar</Navbar.Brand>
      <Navbar.Toggle aria-controls="navbarScroll" />
      <Navbar.Collapse id="navbarScroll">
        <Nav className="me-auto my-2 my-lg-0">
          <Nav.Link href="#action1">Home</Nav.Link>
          <Nav.Link href="#action2">Link</Nav.Link>
          <NavDropdown title="Dropdown" id="navbarScrollingDropdown">
            <div className='py-2 bg-body-tertiary dark__bg-1000 rounded-3'>
              <NavDropdown.Item href="#action3">Action</NavDropdown.Item>
              <NavDropdown.Item href="#action4">Another action</NavDropdown.Item>
              <NavDropdown.Divider />
              <NavDropdown.Item href="#action5">Something else here</NavDropdown.Item>
            </div>
          </NavDropdown>
          <Nav.Link href="#" disabled>
            Disabled
          </Nav.Link>
        </Nav>
        <Form className="d-flex dark">
          <FormControl
            type="search"
            placeholder="Search"
            className="me-2"
            aria-label="Search"
          />
          <Button variant="light">Search</Button>
        </Form>
      </Navbar.Collapse>
    </Navbar>
  </>
`;

const Navbars = () => (
  <>
    <PageHeader
      title="Navbar"
      description="A powerful, responsive navigation header, the navbar. Includes support for branding, navigation, and more"
      className="mb-3"
    >
      <Button
        href={`${reactBootstrapDocsUrl}/docs/components/navbar`}
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Navbar on React Bootstrap
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Color schemes" light={false} />
      <OrkestraComponentCard.Body code={navbarCode} language="jsx" />
    </OrkestraComponentCard>
  </>
);

export default Navbars;

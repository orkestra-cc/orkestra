import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

import { Col, Row, Button } from 'react-bootstrap';
import { Link } from 'react-router';
import { useAppContext } from 'providers/AppProvider';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import PageHeader from 'components/common/PageHeader';
import { themeVariants } from 'config';
import paths from 'routes/paths';

const lightCode = `<Card bg='100' data-bs-theme="light">
  <Card.Body>
    <p className="mb-0 text-700">
      <b>This element will retain it's color if you switch between light and dark mode.</b>
    </p>
  </Card.Body>
</Card>`;

const darkCode = `<Card bg='100' data-bs-theme="dark">
  <Card.Body>
    <p className="mb-0 text-700">
      <b>This element will retain it's color if you switch between light and dark mode.</b>
    </p>
  </Card.Body>
</Card>`;

const dark__Code = `<Card bg='light' className="dark__bg-primary">
  <Card.Body>
    <p className="mb-0 dark__text-success">
      <span className="fw-bold">
        This element will retain it's color if you switch between light and dark mode.
      </span>
    </p>
  </Card.Body>
</Card>`;

const darkModeExampleCode = `function DarkModeExample(){
  const {
    config: { isDark, theme },
    changeTheme
  } = useAppContext();
  return (
    <Row className="g-3">
      <Col sm={6} lg={4}>
        <h5 className="fs-9 mb-2">Checkbox </h5>
        <Form.Check
          type="checkbox"
          id="themeToggleCheckBox"
          label="Dark mode"
          onChange={e => changeTheme(e.target.checked ? 'dark' : 'light')}
          checked={isDark}
        />
      </Col>
      <Col sm={6} lg={4}>
        <h5 className="fs-9 mb-2">Switch Input</h5>
        <Form.Check
          type="switch"
          id="themeToggleSwitch"
          label="Dark mode"
          onChange={e => changeTheme(e.target.checked ? 'dark' : 'light')}
          checked={isDark}
        />
      </Col>
      <Col sm={6} lg={4}>
        <h5 className="fs-9 mb-2">Custom Icon</h5>
        <div className="px-0 theme-control-toggle">
          <OverlayTrigger
            placement="right"
            overlay={
              <Tooltip style={{ position: 'fixed' }} id="hi">{isDark ? 'Switch to light theme' : 'Switch to dark theme'}</Tooltip>
            }
          >
            <div className="theme-control-toggle-label" onClick={() => changeTheme(isDark ? 'light' : 'dark')}>
              <FontAwesomeIcon
                icon={isDark ? 'sun' : 'moon'}
                transform="shrink-7"
                className="fs-5"
              />
            </div>
          </OverlayTrigger>
        </div>
      </Col>
      <Col sm={6} lg={4}>
        <h5 className="fs-9 mb-2">Radio Button</h5>
        <Form.Group>
          <Form.Check
            inline
            type="radio"
            id="lightRadioButton"
            label="Light"
            name="themeToggleRadio"
            checked={theme === "light"}
            onChange={() => changeTheme("light")}
          />
          <Form.Check
            inline
            type="radio"
            id="darkRadioButton"
            label="Dark"
            name="themeToggleRadio"
            checked={theme === "dark"}
            onChange={() => changeTheme("dark")}
          />
          <Form.Check
            inline
            type="radio"
            id="autoRadioButton"
            label="Auto"
            name="themeToggleRadio"
            checked={theme === "auto"}
            onChange={() => changeTheme("auto")}
          />
        </Form.Group>
      </Col>
      <Col sm={6} lg={4}>
        <h5 className="fs-9 mb-2">Dropdown</h5>
        <Dropdown
          navbar={true}
          as="div"
          onSelect={colorMode => changeTheme(colorMode)}
          className="theme-control-dropdown"
        >
          <Dropdown.Toggle
            bsPrefix="toggle"
            as={Button}
            variant="orkestra-default"
            className="btn-sm dropdown-caret-none"
          >
            <FontAwesomeIcon
              icon={
                theme === 'light' ? 'sun' : theme === 'dark' ? 'moon' : 'adjust'
              }
            />
          </Dropdown.Toggle>
    
          <Dropdown.Menu className="dropdown-caret py-0 mt-2">
            <div className="bg-white rounded-2 py-2 dark__bg-1000">
              {themeVariants.map(colorMode => (
                <Dropdown.Item
                  as={Link}
                  key={colorMode}
                  to="#"
                  active={theme === colorMode}
                  eventKey={colorMode}
                  className="d-flex align-items-center gap-2"
                >
                  <FontAwesomeIcon
                    icon={
                      colorMode === 'light'
                        ? 'sun'
                        : colorMode === 'dark'
                        ? 'moon'
                        : 'adjust'
                    }
                  />
                  {colorMode.charAt(0).toUpperCase() + colorMode.slice(1)}
                  {theme === colorMode && (
                    <FontAwesomeIcon icon="check" className="ms-auto text-600" />
                  )}
                </Dropdown.Item>
              ))}
            </div>
          </Dropdown.Menu>
        </Dropdown>
      </Col>
    </Row>
  );
};`;

const DarkMode = () => (
  <>
    <PageHeader title="Dark Mode" className="mb-3">
      <p className="mt-2 mb-0">
        It’s effortless to switch Dark Mode in Orkestra-React. You can enable Dark
        Mode by default or create a Dark/Light switch if you want. To set the
        default mode "Dark", please see the{' '}
        <Link to={paths.configuration}>configuration page</Link>.
      </p>
    </PageHeader>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Toggle Dark Mode" light={false}>
        <p className="mt-2 mb-0">
          Toggling dark mode is very easy in Orkestra-React. You can toggle dark
          or light mode by using checkbox, radio input, switch input and custom
          icon component.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={darkModeExampleCode}
        scope={{ useAppContext, FontAwesomeIcon, Button, Link, themeVariants }}
        language="jsx"
      />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header
        title="Modify Dark colors"
        light={false}
        noPreview
      />
      <OrkestraComponentCard.Body>
        <p className="mb-0">
          You can find all the variables used to create the dark mode in
          <code> src/assets/scss/theme/root/_dark.scss </code> file. If you want
          to override a variable, copy that variable to{' '}
          <code> src/assets/scss/_user-variables.scss </code> file and update it
          as you see fit. After changing the variable build your scss again.
        </p>
      </OrkestraComponentCard.Body>
    </OrkestraComponentCard>

    <OrkestraComponentCard noGuttersBottom>
      <OrkestraComponentCard.Header
        title="Using the Dark className"
        light={false}
        noPreview
      />
      <OrkestraComponentCard.Body>
        <h6 className="fs-9">
          You can keep a style constant regardless of current (light or dark)
          mode
        </h6>
        <p>
          If you want a component to retain it’s color (light or dark) as it is
          regardless of the current mode, you can use the following attributes -
        </p>
        <p>
          <code> data-bs-theme="light" </code> - It will keep the color light
          even if the current mode is dark
        </p>
        <p>
          <code> data-bs-theme="dark" </code> - It will keep the color dark even
          if the current mode is light
        </p>

        <Row>
          <Col>
            <OrkestraComponentCard>
              <OrkestraComponentCard.Header light={false} />
              <OrkestraComponentCard.Body code={lightCode} language="jsx" />
            </OrkestraComponentCard>
          </Col>
          <Col>
            <OrkestraComponentCard>
              <OrkestraComponentCard.Header light={false} />
              <OrkestraComponentCard.Body code={darkCode} language="jsx" />
            </OrkestraComponentCard>
          </Col>
        </Row>

        <h6 className="fs-9 mt-4">
          Override Background and Text color only for dark mode
        </h6>

        <p>
          If you want to use a different text color or background color rather
          than the default dark theme color for any element, you can use the
          special "dark" classeNames:
        </p>

        <ul>
          <li>
            <code>dark__bg-* </code>
          </li>
          <li>
            <code>dark__text-* </code>
          </li>
        </ul>

        <p>The following element illustrates the example:</p>

        <OrkestraComponentCard>
          <OrkestraComponentCard.Header light={false} />
          <OrkestraComponentCard.Body code={dark__Code} language="jsx" />
        </OrkestraComponentCard>
      </OrkestraComponentCard.Body>
    </OrkestraComponentCard>
  </>
);

export default DarkMode;

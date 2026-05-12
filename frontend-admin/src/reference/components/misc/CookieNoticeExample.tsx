import { useState } from 'react';
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import Flex from 'components/common/Flex';
import cookieImg from 'assets/img/icons/cookie-1.png';
import CookieNotice from 'components/common/CookieNotice';

const exampleCode = `<>
  <Button
    size="sm"
    onClick={() => setShow(!show)}
  >
    Toggle Notification
  </Button>

  <CookieNotice show={show} setShow={setShow} autohide delay={3000}>
    <Card>
      <Card.Body>
        <Flex>
          <div className="pe-3">
            <img src={cookieImg} alt="cookie" width={40} />
          </div>
          <div className="">
            <p>
              We use third party cookies to personalize content, ads
              and analyze site traffic.
            </p>
            <Button
              size="sm"
              variant="orkestra-primary"
              className="me-3"
              onClick={() => setShow(!show)}
            >
              Okay
            </Button>
          </div>
        </Flex>
      </Card.Body>
    </Card>
  </CookieNotice>
</>`;

const CookieNoticeExample = () => {
  const [show, setShow] = useState(false);
  return (
    <>
      <PageHeader
        title="Cookie Notice"
        description="Orkestra React has a built-in notification component that allows users to notify cookie uses on the website."
        className="mb-3"
      >
        <Button
          variant="link"
          size="sm"
          className="ps-0"
          onClick={() => setShow(!show)}
        >
          Toggle Notification
          <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
        </Button>
      </PageHeader>

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Example" light={false}>
          <p className="mb-0 mt-2">
            Orkestra React's Cookie Notice uses React-Bootstrap's "Toast" feature
            to show the notice content. <code>.notice </code>class is
            responsible for showing the notice as a popup. You can pass any
            React-Bootstrap's "Toast" props to <code>CookieNotice</code>{' '}
            component. See{' '}
            <a
              href="https://react-bootstrap.github.io/components/toasts/"
              target="_blank"
              rel="noreferrer"
            >
              Toasts documentation on React-Bootstrap.
            </a>
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body
          code={exampleCode}
          scope={{ CookieNotice, show, setShow, Flex, cookieImg }}
          language="jsx"
        />
      </OrkestraComponentCard>
    </>
  );
};

export default CookieNoticeExample;

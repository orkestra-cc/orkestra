import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';
import Avatar from 'components/common/Avatar';
import Flex from 'components/common/Flex';
import IconButton from 'components/common/IconButton';
import { messages } from 'data/support-desk/ticketsPreviewData';
import {
  Button,
  Card,
  Col,
  OverlayTrigger,
  Row,
  Tooltip
} from 'react-bootstrap';
import { Link } from 'react-router';
import { useState } from 'react';
import Compose from 'reference/app-examples/email/compose/Compose';
import paths from 'routes/paths';

interface FileProps {
  icon: any;
  title: string;
  action: string;
  actionIcon: any;
  className?: string;
}

const File = ({ icon, title, action, actionIcon, className }: FileProps) => {
  return (
    <div
      className={classNames(
        'border p-2 rounded-3 d-flex bg-white dark__bg-1000 fs-10',
        className
      )}
    >
      <FontAwesomeIcon icon={icon} className="fs-8" />
      <span className="ms-2 me-3">{title}</span>
      <OverlayTrigger
        placement="right"
        overlay={<Tooltip style={{ position: 'fixed' }}>{action}</Tooltip>}
      >
        <a href="#!" className="text-300 ms-auto">
          <FontAwesomeIcon icon={actionIcon} />
        </a>
      </OverlayTrigger>
    </div>
  );
};

interface MailProps {
  message: any;
  index: number;
}

const Mail = ({ message, index }: MailProps) => {
  return (
    <div>
      {/* mail header */}
      <div className="d-md-flex d-xl-inline-block d-xxl-flex align-items-center justify-content-between mb-x1">
        <Flex alignItems="center" className="gap-2">
          <Link to={paths.contactDetails}>
            <Avatar src={message.img} size="2xl" />
          </Link>
          <p className="mb-0">
            <Link
              to={paths.contactDetails}
              className="fw-semibold mb-0 text-800"
            >
              {message.sender}
            </Link>
            <span className="fs-11 text-800 fw-normal mx-2">
              {message.type}
            </span>
            <a
              className="mb-0 fs-10 d-block text-500"
              href={`mailto:${message.email}`}
            >
              {message.email}
            </a>
          </p>
        </Flex>
        <p className="mb-0 fs-11 fs-sm-10 fw-semibold mt-2 mt-md-0 mt-xl-2 mt-xxl-0 ms-5">
          {message.date}
          <span className="mx-1">|</span>
          <span className="fst-italic">{message.time}</span>
          <FontAwesomeIcon icon="star" className="ms-2 text-warning" />
        </p>
      </div>
      {/* mail body */}
      <div
        className={
          index !== 0 && index !== messages.length - 1
            ? 'mb-5 pb-5 border-bottom'
            : ''
        }
      >
        <h6 className="mb-3 fw-semibold text-1000">{message.subject}</h6>
        <p>{message.salutation}</p>
        <p>{message.message}</p>
        <p>{message.conclusion}</p>
        <p className="mb-0">{message.closing}</p>
        <p className="mb-0">{message.sender}</p>
        {message.attached && (
          <div className="p-x1 bg-body-tertiary rounded-3 mt-3">
            <div className="d-inline-flex flex-column">
              <File
                title="broken_tv_solve.jpg (873kb)"
                icon={['far', 'image']}
                action="Download"
                actionIcon="arrow-down"
              />
              <File
                title="broken_tv_solve.zip (342kb)"
                icon="file-archive"
                action="Download"
                actionIcon="arrow-down"
              />
            </div>
            <hr className="my-x1" />
            <Row className="flex-between-center gx-4 gy-2">
              <Col xs="auto">
                <p className="fs-10 text-1000 mb-sm-0 font-sans-serif fw-medium mb-0">
                  <FontAwesomeIcon icon="link" className="me-2" />2 files
                  attached
                </p>
              </Col>
              <Col xs="auto">
                <IconButton
                  icon="file-download"
                  variant="falcon-default"
                  size="sm"
                >
                  <span className="ms-1">Download all</span>
                </IconButton>
              </Col>
            </Row>
          </div>
        )}
      </div>
      {index === 0 && (
        <div className="my-5 position-relative text-center">
          <hr className="position-absolute top-50 border-300 w-100 my-0" />
          <span className="position-relative bg-white dark__bg-card-dark px-3 z-1">
            <Button
              size="sm"
              variant="outline-secondary"
              className="rounded-pill border-300 px-lg-5"
            >
              Conversions 5+
            </Button>
          </span>
        </div>
      )}
    </div>
  );
};


const Conversation = () => {
  const [showForm, setShowForm] = useState(false);
  return (
    <Card className="mt-3">
      <Card.Header className="bg-body-tertiary">
        <h5 className="mb-0">
          <FontAwesomeIcon icon="envelope" className="me-2" />
          Received a broken TV
        </h5>
      </Card.Header>
      <Card.Body>
        {messages.map((message: any, index: number) => (
          <Mail key={index} message={message} index={index} />
        ))}
      </Card.Body>
      {showForm && (
        <Compose
          setShowForm={setShowForm}
          title="Reply"
          bodyClassName="shadow-none"
        />
      )}
      {!showForm && (
        <Card.Footer className="bg-body-tertiary">
          <IconButton
            size="sm"
            variant="falcon-default"
            icon="reply"
            onClick={() => setShowForm(true)}
          >
            <span className="d-none d-sm-inline-block ms-1">Reply</span>
          </IconButton>
          <IconButton
            size="sm"
            variant="falcon-default"
            icon="location-arrow"
            className="mx-2"
            onClick={() => setShowForm(true)}
          >
            <span className="d-none d-sm-inline-block ms-1">Forward</span>
          </IconButton>
          <IconButton
            size="sm"
            variant="falcon-default"
            icon="file-alt"
            onClick={() => setShowForm(true)}
          >
            <span className="d-none d-sm-inline-block ms-1">Add Note</span>
          </IconButton>
        </Card.Footer>
      )}
    </Card>
  );
};

export default Conversation;

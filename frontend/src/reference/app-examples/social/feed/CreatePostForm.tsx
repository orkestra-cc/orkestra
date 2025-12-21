import { useState } from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import calendar from 'assets/img/icons/spot-illustrations/calendar.svg';
import image from 'assets/img/icons/spot-illustrations/image.svg';
import location from 'assets/img/icons/spot-illustrations/location.svg';
import team3 from 'assets/img/team/3.jpg';
import classNames from 'classnames';
import Flex from 'components/common/Flex';
import { useFeedContext } from 'providers/FeedProvider';
import { Button, Col, Dropdown, Form, Row } from 'react-bootstrap';
import { v4 as uuid } from 'uuid';
import { IconName } from '@fortawesome/fontawesome-svg-core';

const CreatePostForm = () => {
  const { feedDispatch } = useFeedContext();
  const [privacy, setPrivacy] = useState('public');
  const [status, setStatus] = useState('');

  const handleSubmit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();

    const feed: any = {
      id: uuid(),
      user: {
        name: 'Rebecca Marry',
        avatarSrc: team3,
        time: 'Just now',
        location: 'Dalby',
        status: 'status-online',
        privacy
      },
      content: { status },
      details: { countLCS: { like: 0, share: 0 } }
    };

    !!status && feedDispatch({ type: 'ADD', payload: feed });

    setStatus('');
  };

  return (
    <Form onSubmit={handleSubmit}>
      <Form.Control
        as="textarea"
        rows={4}
        placeholder="What do you want to talk about?"
        className="shadow-none rounded-0 resize-none border-y-0 px-x1 border-200"
        value={status}
        onChange={e => setStatus(e.target.value)}
      />
      <Form.Group
        as={Flex}
        alignItems="center"
        className="ps-x1 border border-200"
        controlId="hashtag"
      >
        <Form.Label className="text-nowrap mb-0 me-2">
          <FontAwesomeIcon icon="plus" className="me-1 fs-11" />
          <span className="fw-medium fs-10">Add hashtag</span>
        </Form.Label>
        <Form.Control
          type="text"
          placeholder="Help the right person to see"
          className="border-0 fs-10  shadow-none"
        />
      </Form.Group>

      <Row className="g-0 mt-3 px-x1 pb-3 justify-content-between">
        <Col>
          <PostButton image={image} title="Image" />
          <PostButton image={calendar} title="Event" />
          <PostButton image={location} title="Check In" />
        </Col>
        <Col xs="auto">
          <Dropdown className="d-inline-block me-1">
            <Dropdown.Toggle
              variant="none"
              size="sm"
              className="px-1 shadow-none"
            >
              <FontAwesomeIcon
                icon={classNames({
                  users: privacy === 'friends',
                  lock: privacy === 'private',
                  'globe-americas': privacy === 'public'
                }) as IconName}
              />
            </Dropdown.Toggle>
            <Dropdown.Menu className="py-2" align="end">
              <Dropdown.Item href="#!" onClick={() => setPrivacy('public')}>
                Public
              </Dropdown.Item>
              <Dropdown.Item href="#!" onClick={() => setPrivacy('private')}>
                Private
              </Dropdown.Item>
              <Dropdown.Item href="#!" onClick={() => setPrivacy('friends')}>
                Friends
              </Dropdown.Item>
            </Dropdown.Menu>
          </Dropdown>
          <Button size="sm" className="px-4 px-sm-5" type="submit">
            Share
          </Button>
        </Col>
      </Row>
    </Form>
  );
};

interface PostButtonProps {
  image: string;
  title: string;
}

const PostButton = ({ image, title }: PostButtonProps) => (
  <Button
    variant="tertiary"
    size="sm"
    className="rounded-pill shadow-none d-inline-flex align-items-center fs-10 mb-0 me-1"
  >
    <img src={image} alt="" width={17} />
    <span className="ms-2 d-none d-md-inline-block">{title}</span>
  </Button>
);

export default CreatePostForm;

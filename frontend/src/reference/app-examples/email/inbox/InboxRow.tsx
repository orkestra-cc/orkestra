import { useState } from 'react';
import {
  Button,
  ButtonGroup,
  Col,
  Form,
  OverlayTrigger,
  Row,
  Tooltip
} from 'react-bootstrap';
import Flex from 'components/common/Flex';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import classNames from 'classnames';
import Avatar from 'components/common/Avatar';
import { Link } from 'react-router';
import EmailAttachment from './EmailAttachment';
import SubtleBadge from 'components/common/SubtleBadge';
import { toast } from 'react-toastify';
import paths from 'routes/paths';
import { useEmailContext } from 'providers/EmailProvider';

interface ActionButtonProps {
  tooltip: string;
  icon: IconProp;
  handleClick: () => void;
  variant?: string;
}

const ActionButton = ({ tooltip, icon, handleClick, variant = 'tertiary' }: ActionButtonProps) => (
  <OverlayTrigger
    overlay={<Tooltip style={{ position: 'fixed' }}>{tooltip}</Tooltip>}
  >
    <Button variant={variant} onClick={handleClick} className="hover-bg-200">
      <FontAwesomeIcon icon={icon} />
    </Button>
  </OverlayTrigger>
);

interface EmailData {
  id: number | string;
  img?: string;
  read: boolean;
  star?: boolean;
  time?: string;
  title?: string;
  user?: string;
  description?: string;
  badge?: string;
  attachments?: any[];
  [key: string]: any;
}

interface InboxRowProps {
  email: EmailData;
  isSelectedItem: (id: number | string) => boolean;
  toggleSelectedItem: (id: number | string) => void;
}

const InboxRow = ({ email, isSelectedItem, toggleSelectedItem }: InboxRowProps) => {
  const {
    id,
    img,
    read,
    star,
    time,
    title,
    user,
    description,
    badge,
    attachments
  } = email;

  const { emailDispatch } = useEmailContext();

  const [marked, setMarked] = useState(star);

  const handleActionButtonClick = (type: 'ARCHIVE' | 'DELETE' | 'READ' | 'SNOOZE') => {
    emailDispatch({
      type,
      payload: [id]
    });
    let action = '';
    switch (type) {
      case 'ARCHIVE':
        action = 'archived';
        break;
      case 'DELETE':
        action = 'deleted';
        break;
      case 'READ':
        action = read ? 'unread' : 'read';
        break;
      case 'SNOOZE':
        action = 'snoozed';
        break;

      default:
        break;
    }

    toast.success(
      <div className="flex-1 py-2">
        Conversation marked as {action}
      </div>, {
      theme: 'colored'
    });
  };

  return (
    <Row
      className={classNames(
        'border-bottom border-200 hover-actions-trigger hover-shadow py-2 px-1 mx-0 align-items-center',
        {
          'bg-body-tertiary': read,
          'bg-white dark__bg-dark': !read
        }
      )}
    >
      <ButtonGroup
        size="sm"
        className="hover-actions end-0 me-3 email-row-actions"
        style={{ width: '10rem' }}
      >
        <ActionButton
          tooltip="Archive"
          icon={'archive' as IconProp}
          handleClick={() => handleActionButtonClick('ARCHIVE')}
        />
        <ActionButton
          tooltip="Delete"
          icon={'trash-alt' as IconProp}
          handleClick={() => handleActionButtonClick('DELETE')}
        />
        <ActionButton
          tooltip={read ? 'Mark as unread' : 'Mark as read'}
          icon={(read ? 'envelope' : 'envelope-open') as IconProp}
          handleClick={() => handleActionButtonClick('READ')}
        />
        <ActionButton
          tooltip="Snooze"
          icon={'clock' as IconProp}
          handleClick={() => handleActionButtonClick('SNOOZE')}
        />
      </ButtonGroup>
      <Col xs="auto" className="d-none d-sm-block align-self-start">
        <Flex alignItems="center">
          <Form.Check
            type="checkbox"
            id="inboxBulkSelect"
            className="form-check mb-0 fs-9"
            checked={isSelectedItem(id)}
            onChange={() => toggleSelectedItem(id)}
          />
          <FontAwesomeIcon
            onClick={() => setMarked(!marked)}
            icon={marked ? 'star' : ['far', 'star']}
            // transform="down-2"
            className={classNames(
              'ms-1',
              { 'text-warning': marked, 'text-300': !marked },
              'cursor-pointer'
            )}
          />
        </Flex>
      </Col>
      <Col xs md={9} xxl={10}>
        <Row>
          <Col md={4} xl={3} xxl={2} className="ps-md-0 mb-1 mb-md-0">
            <Flex className="position-relative">
              <Avatar src={img} size="s" rounded="circle" />
              <div className="flex-1 ms-2">
                <Link
                  to={paths.emailDetail}
                  className={classNames('stretched-link inbox-link', {
                    'fw-bold': !read
                  })}
                >
                  {user}
                </Link>
                {!!badge && (
                  <SubtleBadge bg="success" className="ms-2">
                    {badge}
                  </SubtleBadge>
                )}
              </div>
            </Flex>
          </Col>
          <Col>
            <Link className="d-block inbox-link" to={paths.emailDetail}>
              <span className={classNames({ 'fw-bold': !read })}>{title}</span>
              <span className="mx-1">&ndash;</span>
              <span>{description}</span>
            </Link>
            {attachments?.map(attachment => (
              <EmailAttachment attachment={attachment} key={attachment.id} />
            ))}
          </Col>
        </Row>
      </Col>
      <Col
        xs="auto"
        as={Flex}
        direction="column"
        justifyContent="between"
        className="ms-auto align-self-start"
      >
        <span className={classNames({ 'fw-bold': !read })}>{time}</span>
      </Col>
    </Row>
  );
};

export default InboxRow;

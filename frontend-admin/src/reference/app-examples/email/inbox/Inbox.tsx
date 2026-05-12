
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconName } from '@fortawesome/fontawesome-svg-core';
import Flex from 'components/common/Flex';
import IconButton from 'components/common/IconButton';
import {
  Alert,
  Button,
  ButtonGroup,
  Card,
  Col,
  Form,
  OverlayTrigger,
  Row,
  Tooltip
} from 'react-bootstrap';
import { Link } from 'react-router';
import classNames from 'classnames';
import InboxRow from './InboxRow';
import useBulkSelect from 'hooks/ui/useBulkSelect';
import InboxFilterDropdown from './InboxFilterDropdown';
import InboxFooter from './InboxFooter';
import usePagination from 'hooks/ui/usePagination';
import paths from 'routes/paths';
import EmailProvider, { useEmailContext } from 'providers/EmailProvider';

// Unused interfaces - removed to clean up code

interface InboxTitleProps {
  className?: string;
  children: React.ReactNode;
  [key: string]: any;
}

const InboxTitle: React.FC<InboxTitleProps> = ({ className, children, ...rest }) => (
  <h5
    className={classNames(
      className,
      'fs-9 px-3 pt-3 pb-2 mb-0 border-bottom border-200'
    )}
    {...rest}
  >
    {children}
  </h5>
);

interface ActionButtonProps {
  tooltip: string;
  icon: IconName;
  handleClick: () => void;
  variant?: string;
}

const ActionButton: React.FC<ActionButtonProps> = ({
  tooltip,
  icon,
  handleClick,
  variant = 'orkestra-default'
}) => (
  <OverlayTrigger
    overlay={<Tooltip style={{ position: 'fixed' }}>{tooltip}</Tooltip>}
  >
    <Button variant={variant} onClick={handleClick} className="shdaow-none">
      <FontAwesomeIcon icon={icon} />
    </Button>
  </OverlayTrigger>
);

const InboxElement: React.FC = () => {
  const {
    emailState: { emails },
    emailDispatch
  } = useEmailContext();
  const {
    paginationState: {
      data: paginatedEmails,
      totalItems,
      canNextPage,
      canPreviousPage,
      from,
      to
    },
    nextPage,
    prevPage
  } = usePagination(emails, 10);

  const unreadEmails = paginatedEmails.filter(email => !email.read);
  const otherEmails = paginatedEmails.filter(email => email.read);
  const emailIds = emails.map(email => email.id);

  const {
    selectedItems,
    isSelectedItem,
    isAllSelected,
    isIndeterminate,
    toggleSelectedItem,
    toggleIsAllSelected
  } = useBulkSelect(emailIds);

  const handleActionButtonClick = (type: 'ARCHIVE' | 'DELETE' | 'READ' | 'SNOOZE') => {
    emailDispatch({
      type,
      payload: selectedItems
    });
    toggleIsAllSelected();
  };

  return (
    <Card>
      <Card.Header>
        <Row className="align-items-center">
          <Col as={Flex} className="align-items-center">
            <Form.Check
              type="checkbox"
              id="inboxBulkSelect"
              className="mb-0 d-none d-sm-block form-check"
              checked={isAllSelected || isIndeterminate}
              onChange={() => toggleIsAllSelected()}
            />

            {selectedItems.length === 0 && (
              <>
                <IconButton
                  variant="orkestra-default"
                  size="sm"
                  className="ms-sm-1"
                  icon="redo"
                  onClick={() => {
                    emailDispatch({ type: 'RESET' });
                  }}
                />
                <InboxFilterDropdown />
              </>
            )}
            {selectedItems.length > 0 && (
              <div className="ms-1">
                <ButtonGroup size="sm">
                  <ActionButton
                    tooltip="Archive"
                    icon={'archive' as IconName}
                    handleClick={() => handleActionButtonClick('ARCHIVE')}
                  />
                  <ActionButton
                    tooltip="Delete"
                    icon={'trash-alt' as IconName}
                    handleClick={() => handleActionButtonClick('DELETE')}
                  />
                  <ActionButton
                    tooltip="Mark as unread"
                    icon={'envelope' as IconName}
                    handleClick={() => handleActionButtonClick('READ')}
                  />
                  <ActionButton
                    tooltip="Snooze"
                    icon={'clock' as IconName}
                    handleClick={() => handleActionButtonClick('SNOOZE')}
                  />
                </ButtonGroup>
              </div>
            )}
          </Col>
          <Col xs="auto">
            <IconButton
              variant="orkestra-primary"
              size="sm"
              icon="plus"
              as={Link}
              to={paths.emailCompose}
              transform="shrink-3"
            >
              Compose
            </IconButton>
          </Col>
        </Row>
      </Card.Header>
      <Card.Body className="fs-10 border-top border-200 p-0">
        {paginatedEmails.length > 0 ? (
          <>
            {unreadEmails.length > 0 && <InboxTitle>Unread</InboxTitle>}
            {unreadEmails.map(email => (
              <InboxRow
                email={email}
                key={email.id}
                isSelectedItem={isSelectedItem}
                toggleSelectedItem={toggleSelectedItem}
              />
            ))}
            <InboxTitle>Everything else</InboxTitle>
            {otherEmails.map(email => (
              <InboxRow
                email={email}
                key={email.id}
                isSelectedItem={isSelectedItem}
                toggleSelectedItem={toggleSelectedItem}
              />
            ))}
          </>
        ) : (
          <Alert variant="info" className="mb-0 rounded-0">
            <h5 className="alert-heading">Inbox empty!</h5>
            <hr />
            <p className="mb-0">Emails will be shown here automatically.</p>
          </Alert>
        )}
      </Card.Body>
      <InboxFooter
        totalItems={totalItems}
        from={from}
        to={to}
        canNextPage={canNextPage}
        canPreviousPage={canPreviousPage}
        nextPage={nextPage}
        prevPage={prevPage}
      />
    </Card>
  );
};

const Inbox: React.FC = () => {
  return (
    <EmailProvider>
      <InboxElement />
    </EmailProvider>
  );
};

export default Inbox;

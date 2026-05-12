import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import Flex from 'components/common/Flex';
import IconButton from 'components/common/IconButton';
import { Card, Dropdown } from 'react-bootstrap';
import { useNavigate } from 'react-router';

interface ContactDetailsHeaderProps {
  handleShow: () => void;
}

const ContactDetailsHeader = ({ handleShow }: ContactDetailsHeaderProps) => {
  const navigate = useNavigate();
  return (
    <Card className="mb-3">
      <Card.Header className="d-flex flex-between-center">
        <IconButton
          onClick={() => navigate(-1)}
          variant="orkestra-default"
          size="sm"
          icon="arrow-left"
        />
        <Flex>
          <IconButton
            onClick={handleShow}
            variant="orkestra-default"
            size="sm"
            icon="tasks"
            transform="shrink-2"
            iconAlign="middle"
            className="d-xl-none"
          >
            <span className="ms-1">To-do</span>
          </IconButton>
          <div
            className="bg-300 mx-3 d-xl-none"
            style={{ width: '1px', height: '29px' }}
          ></div>
          <IconButton
            variant="orkestra-default"
            size="sm"
            icon="edit"
            transform="shrink-2"
            iconAlign="middle"
            className="me-2"
          >
            <span className="d-none d-xl-inline-block ms-1">Edit</span>
          </IconButton>
          <IconButton
            variant="orkestra-default"
            size="sm"
            icon="sync-alt"
            iconAlign="middle"
            className="d-none d-sm-block"
          >
            <span className="d-none d-xl-inline-block ms-1">
              Convert to Agent
            </span>
          </IconButton>
          <IconButton
            variant="orkestra-default"
            size="sm"
            icon="lock"
            iconAlign="middle"
            className="d-none d-sm-block mx-2"
          >
            <span className="d-none d-xl-inline-block ms-1">
              Send Activation Email
            </span>
          </IconButton>
          <IconButton
            variant="orkestra-default"
            size="sm"
            icon="trash-alt"
            iconAlign="middle"
            className="d-none d-sm-block me-2"
          >
            <span className="d-none d-xl-inline-block ms-1">Delete</span>
          </IconButton>
          <IconButton
            variant="orkestra-default"
            size="sm"
            icon="key"
            iconAlign="middle"
            className="d-none d-sm-block me-2"
          >
            <span className="d-none d-xl-inline-block ms-1">
              Change Password
            </span>
          </IconButton>
          <Dropdown align="end" className="btn-reveal-trigger d-inline-block">
            <Dropdown.Toggle split variant="orkestra-default" size="sm">
              <FontAwesomeIcon icon="ellipsis-v" className="fs-11" />
            </Dropdown.Toggle>

            <Dropdown.Menu className="border py-0">
              <div className="py-2">
                <Dropdown.Item>View</Dropdown.Item>
                <Dropdown.Item>Export</Dropdown.Item>
                <Dropdown.Item className="d-sm-none">
                  Convert to Agent
                </Dropdown.Item>
                <Dropdown.Item className="d-sm-none">
                  Send Activation Email
                </Dropdown.Item>
                <Dropdown.Item className="d-sm-none">Delete</Dropdown.Item>
                <Dropdown.Item className="d-sm-none">
                  Change Password
                </Dropdown.Item>
                <Dropdown.Divider />
                <Dropdown.Item className="text-danger">Remove</Dropdown.Item>
              </div>
            </Dropdown.Menu>
          </Dropdown>
        </Flex>
      </Card.Header>
    </Card>
  );
};

export default ContactDetailsHeader;

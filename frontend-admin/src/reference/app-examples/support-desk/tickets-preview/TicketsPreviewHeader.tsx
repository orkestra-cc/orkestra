import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import Flex from 'components/common/Flex';
import IconButton from 'components/common/IconButton';

import { Card, Dropdown } from 'react-bootstrap';
import { useNavigate } from 'react-router';

const TicketsPreviewHeader = () => {
  const navigate = useNavigate();
  return (
    <Card>
      <Card.Header className="d-flex flex-between-center">
        <IconButton
          onClick={() => navigate(-1)}
          variant="orkestra-default"
          size="sm"
          icon="arrow-left"
        />
        <Flex>
          <IconButton
            variant="orkestra-default"
            size="sm"
            icon="object-ungroup"
            transform="shrink-2"
            iconAlign="middle"
          >
            <span className="d-none d-md-inline-block ms-1">Merge</span>
          </IconButton>
          <IconButton
            variant="orkestra-default"
            size="sm"
            icon="check"
            transform="shrink-2"
            iconAlign="middle"
            className="mx-2"
          >
            <span className="d-none d-md-inline-block ms-1">close</span>
          </IconButton>
          <IconButton
            variant="orkestra-default"
            size="sm"
            icon="ban"
            transform="shrink-2"
            iconAlign="middle"
          >
            <span className="d-none d-md-inline-block ms-1">Ban visitor</span>
          </IconButton>
          <IconButton
            variant="orkestra-default"
            size="sm"
            icon="trash-alt"
            transform="shrink-2"
            iconAlign="middle"
            className="ms-2 d-none d-sm-block"
          >
            <span className="d-none d-md-inline-block ms-1">Delete</span>
          </IconButton>
          <Dropdown
            align="end"
            className="btn-reveal-trigger d-inline-block ms-2"
          >
            <Dropdown.Toggle split variant="orkestra-default" size="sm">
              <FontAwesomeIcon icon="ellipsis-v" className="fs-11" />
            </Dropdown.Toggle>

            <Dropdown.Menu className="border py-0">
              <div className="py-2">
                <Dropdown.Item>View</Dropdown.Item>
                <Dropdown.Item>Export</Dropdown.Item>
                <Dropdown.Item className="d-sm-none">Delete</Dropdown.Item>
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

export default TicketsPreviewHeader;

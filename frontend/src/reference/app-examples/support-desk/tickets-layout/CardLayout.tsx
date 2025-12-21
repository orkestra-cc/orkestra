
import { Col, Row } from 'react-bootstrap';
import { Link } from 'react-router';
import paths from 'routes/paths';
import Priority from './Priority';
import AgentSelect from './AgentSelect';
import Avatar from 'components/common/Avatar';
import SubtleBadge from 'components/common/SubtleBadge';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import IndeterminateCheckbox from 'components/common/advance-table/IndeterminateCheckbox';

const CardLayout = () => {
  const { getRowModel } = useAdvanceTableContext();

  return (
    <div className="d-flex flex-column gap-3">
      {getRowModel().rows.map((row: any, index: number) => {
        const { name, date, status, subject, avatar, agent, priority } =
          row.original;
        const {
          getIsSelected,
          getCanSelect,
          getIsSomeSelected,
          getToggleSelectedHandler
        } = row;

        return (
          <div
            key={index}
            className="bg-white dark__bg-1100 d-md-flex d-xl-inline-block d-xxl-flex align-items-center p-x1 rounded-3 shadow-sm card-view-height w-100"
          >
            <div className="d-flex align-items-start align-items-sm-center">
              <IndeterminateCheckbox
                className="form-check mb-0"
                {...{
                  checked: getIsSelected(),
                  disabled: !getCanSelect(),
                  indeterminate: getIsSomeSelected(),
                  onChange: getToggleSelectedHandler()
                }}
              />
              <Link to={paths.contactDetails} className="d-none d-sm-block">
                {avatar.img ? (
                  <Avatar src={avatar.img} size="3xl" />
                ) : (
                  <Avatar size="3xl" name={avatar.name} />
                )}
              </Link>
              <div className="ms-1 ms-sm-3">
                <p className="fw-semibold mb-3 mb-sm-2">
                  <Link to={paths.ticketsPreview}>{subject}</Link>
                </p>
                <Row className="align-items-center gx-0 gy-2">
                  <Col xs="auto" className="me-2">
                    <h6 className="mb-0">
                      <Link
                        to={paths.contactDetails}
                        className="text-800 d-flex align-items-center gap-1"
                      >
                        <FontAwesomeIcon
                          icon="user"
                          transform="shrink-3 up-1"
                        />
                        <span>{name}</span>
                      </Link>
                    </h6>
                  </Col>
                  <Col xs="auto" className="lh-1 me-3">
                    <SubtleBadge bg={status.type}>{status.content}</SubtleBadge>
                  </Col>
                  <Col xs="auto">
                    <h6 className="mb-0 text-500">{date}</h6>
                  </Col>
                </Row>
              </div>
            </div>
            <div className="border-bottom mt-4 mb-x1"></div>
            <div className="d-flex justify-content-between ms-auto">
              <Priority
                title={priority.title}
                color={priority.color}
                data={priority.data}
              />
              <AgentSelect agent={agent} />
            </div>
          </div>
        );
      })}
    </div>
  );
};

export default CardLayout;

import Flex from 'components/common/Flex';
import SubtleBadge from 'components/common/SubtleBadge';
import { tickets } from 'data/support-desk/contactDetailsData';
import { Link } from 'react-router';
import { Form } from 'react-bootstrap';
import paths from 'routes/paths';

interface PrioritySelectProps {
  title: string;
  color: string;
  data: string;
}

const PrioritySelect = ({ title, color, data }: PrioritySelectProps) => {
  return (
    <div
      style={{ width: '7.5rem' }}
      className="d-flex align-items-center gap-2 ms-md-4 ms-xl-0 ms-xxl-4"
    >
      <div style={{ '--orkestra-circle-progress-bar': data } as React.CSSProperties}>
        <svg
          className="circle-progress-svg"
          width="26"
          height="26"
          viewBox="0 0 120 120"
        >
          <circle
            className="progress-bar-rail"
            cx="60"
            cy="60"
            r="54"
            fill="none"
            strokeLinecap="round"
            strokeWidth="12"
          ></circle>
          <circle
            className="progress-bar-top"
            cx="60"
            cy="60"
            r="54"
            fill="none"
            strokeLinecap="round"
            stroke={color}
            strokeWidth="12"
          ></circle>
        </svg>
      </div>
      <h6 className="mb-0 text-700">{title}</h6>
    </div>
  );
};

interface AgentSelectProps {
  agent: string;
  className?: string;
}

const AgentSelect = ({ agent, className }: AgentSelectProps) => {
  return (
    <Form.Select
      style={{ width: '9.375rem' }}
      className={className}
      size="sm"
      defaultValue={agent}
    >
      {['Select Agent', 'Anindya', 'Nowrin', 'Khalid', 'Shajeeb'].map(item => (
        <option key={item}>{item}</option>
      ))}
    </Form.Select>
  );
};


const Tickets = () => {
  return (
    <Flex direction="column" className="gap-3">
      {tickets.map((ticket: any, index: number) => {
        const { subject, status, priority, agent, date } = ticket;
        return (
          <div
            key={index}
            className="bg-white dark__bg-1100 p-x1 rounded-3 shadow-sm d-md-flex d-xl-inline-block d-xxl-flex align-items-center"
          >
            <div>
              <p className="fw-semibold">
                <Link to={paths.ticketsPreview}>{subject}</Link>
              </p>
              <Flex alignContent="center">
                <h6 className="mb-0 me-3 text-800 lh-base">{date}</h6>
                <SubtleBadge bg={status.type}>{status.content}</SubtleBadge>
              </Flex>
            </div>
            <div className="border-bottom mt-4 mb-x1"></div>
            <Flex justifyContent="between" className="ms-auto">
              <PrioritySelect
                title={priority.title}
                color={priority.color}
                data={priority.data}
              />
              <AgentSelect agent={agent} className="" />
            </Flex>
          </div>
        );
      })}
    </Flex>
  );
};

export default Tickets;

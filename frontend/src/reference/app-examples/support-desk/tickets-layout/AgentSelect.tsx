import { Form } from 'react-bootstrap';

interface AgentSelectProps {
  agent: string;
  className?: string;
}

export const AgentSelect = ({ agent, className = '' }: AgentSelectProps) => {
  return (
    <Form.Select
      style={{ width: '9.375rem' }}
      className={className}
      size="sm"
      defaultValue={agent}
    >
      {['Select Agent', 'Anindya', 'Nowrin', 'Khalid'].map(item => (
        <option key={item}>{item}</option>
      ))}
    </Form.Select>
  );
};

export default AgentSelect;

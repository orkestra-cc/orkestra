// Example sub-component co-located with the page that uses it.
//
// Co-located helpers (cards, charts, sub-tables) live next to the page
// they belong to — see `src/pages/billing/dashboard/RecentInvoices.tsx`
// for the convention. They are NOT promoted to `components/common/`
// unless they are used by more than one page.

import { Badge, Card } from 'react-bootstrap';
import { Link } from 'react-router-dom';
import type { Widget } from '../types';

interface Props {
  widget: Widget;
}

const ExampleCard: React.FC<Props> = ({ widget }) => {
  return (
    <Card className="h-100">
      <Card.Body>
        <Card.Title className="d-flex justify-content-between align-items-start">
          <span>{widget.name}</span>
          <Badge bg={widget.status === 'active' ? 'success' : 'secondary'}>
            {widget.status}
          </Badge>
        </Card.Title>
        <Card.Text className="text-muted small">
          {widget.description || 'No description.'}
        </Card.Text>
      </Card.Body>
      <Card.Footer className="bg-light">
        <Link to={`/widgets/${widget.uuid}`} className="stretched-link">
          View details
        </Link>
      </Card.Footer>
    </Card>
  );
};

export default ExampleCard;

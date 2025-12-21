import { Dropdown } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useKanbanContext } from 'providers/KanbanProvider';

interface KanbanColumnHeaderProps {
  id: string | number;
  title?: string;
  itemCount: number;
}

const KanbanColumnHeader = ({ id, title, itemCount }: KanbanColumnHeaderProps) => {
  const { removeKanbanColumn } = useKanbanContext();

  const handleRemoveColumn = () => {
    removeKanbanColumn(id.toString());
  };

  return (
    <div className="kanban-column-header">
      <h5 className="fs-9 mb-0">
        {title} <span className="text-500">({itemCount})</span>
      </h5>
      <Dropdown align="end" className="font-sans-serif btn-reveal-trigger">
        <Dropdown.Toggle variant="reveal" size="sm" className="py-0 px-2">
          <FontAwesomeIcon icon="ellipsis-h" />
        </Dropdown.Toggle>

        <Dropdown.Menu className="py-0">
          <Dropdown.Item href="#!">Add Card</Dropdown.Item>
          <Dropdown.Item href="#!">Edit</Dropdown.Item>
          <Dropdown.Item href="#!">Copy link</Dropdown.Item>
          <Dropdown.Divider />
          <Dropdown.Item
            onClick={handleRemoveColumn}
            href="#!"
            className="text-danger"
          >
            Remove
          </Dropdown.Item>
        </Dropdown.Menu>
      </Dropdown>
    </div>
  );
};

export default KanbanColumnHeader;

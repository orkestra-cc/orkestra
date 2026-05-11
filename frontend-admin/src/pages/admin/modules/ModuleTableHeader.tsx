import { Col, Form, Row } from 'react-bootstrap';

interface ModuleTableHeaderProps {
  title?: string;
  searchTerm: string;
  onSearchChange: (value: string) => void;
  categoryFilter: string;
  onCategoryChange: (value: string) => void;
  categoryOptions?: { value: string; label: string }[];
  hideCategoryFilter?: boolean;
  statusFilter: string;
  onStatusChange: (value: string) => void;
}

const defaultCategoryOptions = [
  { value: '', label: 'All Categories' },
  { value: 'core', label: 'Core' },
  { value: 'toggleable', label: 'Toggleable' },
  { value: 'external', label: 'External' },
];

const ModuleTableHeader: React.FC<ModuleTableHeaderProps> = ({
  title = 'Module Management',
  searchTerm,
  onSearchChange,
  categoryFilter,
  onCategoryChange,
  categoryOptions = defaultCategoryOptions,
  hideCategoryFilter = false,
  statusFilter,
  onStatusChange,
}) => {
  return (
    <Row className="align-items-center g-3">
      <Col xs="auto">
        <h5 className="mb-0">{title}</h5>
      </Col>
      <Col>
        <Form.Control
          type="search"
          placeholder="Search modules..."
          size="sm"
          autoComplete="off"
          value={searchTerm}
          onChange={(e) => onSearchChange(e.target.value)}
        />
      </Col>
      {!hideCategoryFilter && (
        <Col xs="auto">
          <Form.Select
            size="sm"
            value={categoryFilter}
            onChange={(e) => onCategoryChange(e.target.value)}
          >
            {categoryOptions.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </Form.Select>
        </Col>
      )}
      <Col xs="auto">
        <Form.Select
          size="sm"
          value={statusFilter}
          onChange={(e) => onStatusChange(e.target.value)}
        >
          <option value="">All Statuses</option>
          <option value="running">Running</option>
          <option value="failed">Failed</option>
          <option value="disabled">Disabled</option>
          <option value="stopped">Stopped</option>
        </Form.Select>
      </Col>
    </Row>
  );
};

export default ModuleTableHeader;

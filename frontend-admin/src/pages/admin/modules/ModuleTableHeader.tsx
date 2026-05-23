import { Col, Form, Row } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

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

const ModuleTableHeader: React.FC<ModuleTableHeaderProps> = ({
  title,
  searchTerm,
  onSearchChange,
  categoryFilter,
  onCategoryChange,
  categoryOptions,
  hideCategoryFilter = false,
  statusFilter,
  onStatusChange
}) => {
  const { t } = useTranslation();
  // Default category options are built from translations so the dropdown
  // tracks the active locale rather than being seeded at module load.
  const effectiveCategoryOptions = categoryOptions ?? [
    { value: '', label: t('adminModules.filters.allCategories') },
    { value: 'core', label: t('adminModules.filters.categoryCore') },
    {
      value: 'toggleable',
      label: t('adminModules.filters.categoryToggleable')
    },
    { value: 'external', label: t('adminModules.filters.categoryExternal') }
  ];
  const effectiveTitle = title ?? t('adminModules.pageTitle');
  return (
    <Row className="align-items-center g-3">
      <Col xs="auto">
        <h5 className="mb-0">{effectiveTitle}</h5>
      </Col>
      <Col>
        <Form.Control
          type="search"
          placeholder={t('adminModules.filters.searchPlaceholder')}
          size="sm"
          autoComplete="off"
          value={searchTerm}
          onChange={e => onSearchChange(e.target.value)}
        />
      </Col>
      {!hideCategoryFilter && (
        <Col xs="auto">
          <Form.Select
            size="sm"
            value={categoryFilter}
            onChange={e => onCategoryChange(e.target.value)}
          >
            {effectiveCategoryOptions.map(opt => (
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
          onChange={e => onStatusChange(e.target.value)}
        >
          <option value="">{t('adminModules.filters.allStatuses')}</option>
          <option value="running">
            {t('adminModules.filters.statusRunning')}
          </option>
          <option value="failed">
            {t('adminModules.filters.statusFailed')}
          </option>
          <option value="disabled">
            {t('adminModules.filters.statusDisabled')}
          </option>
          <option value="stopped">
            {t('adminModules.filters.statusStopped')}
          </option>
        </Form.Select>
      </Col>
    </Row>
  );
};

export default ModuleTableHeader;

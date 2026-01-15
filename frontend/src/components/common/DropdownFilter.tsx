import { Dropdown } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import Flex from './Flex';
import classNames from 'classnames';
import { IconProp } from '@fortawesome/fontawesome-svg-core';


interface DropdownItemFilterProps {
  filter: string;
  currentFilter: string;
  className?: string;
  children: React.ReactNode;
  onClick?: () => void;
}

const DropdownItemFilter = ({
  filter,
  currentFilter,
  className,
  children,
  onClick
}: DropdownItemFilterProps) => {
  return (
    <Dropdown.Item
      onClick={onClick}
      className={classNames(className, {
        active: filter === currentFilter
      })}
    >
      <Flex justifyContent="between" alignItems="center">
        {children}
        {filter === currentFilter && (
          <FontAwesomeIcon icon="check" transform="down-4 shrink-4" />
        )}
      </Flex>
    </Dropdown.Item>
  );
};

interface DropdownFilterProps {
  filters: string[];
  handleFilter: (filter: string) => void;
  currentFilter: string;
  icon: IconProp;
  className?: string;
  right?: boolean;
}

const DropdownFilter = ({
  filters,
  handleFilter,
  currentFilter,
  icon,
  className,
  right
}: DropdownFilterProps) => {
  return (
    <Dropdown
      className={classNames('font-sans-serif me-2', className)}
      align={right ? 'end' : undefined}
      style={{ '--falcon-dropdown-content': 'none' } as any}
    >
      <Dropdown.Toggle
        variant="falcon-default"
        className="text-600 dropdown-caret-none"
        size="sm"
      >
        {currentFilter && <span className="me-2">{currentFilter}</span>}
        <FontAwesomeIcon icon={icon} />
      </Dropdown.Toggle>

      <Dropdown.Menu className="border py-2">
        {filters.map((filter, index) => (
          <DropdownItemFilter
            currentFilter={currentFilter}
            onClick={() => {
              handleFilter(filter);
            }}
            filter={filter}
            className="text-capitalize"
            key={index}
          >
            {filter}
          </DropdownItemFilter>
        ))}
      </Dropdown.Menu>
    </Dropdown>
  );
};

export default DropdownFilter;

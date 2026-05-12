import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import Flex from 'components/common/Flex';
import { Dropdown } from 'react-bootstrap';
import { useAppContext } from 'providers/AppProvider';
import { useEmailContext } from 'providers/EmailProvider';

interface InboxFilterDropdownItemProps {
  active: boolean;
  children: React.ReactNode;
  onClick?: () => void;
}

const InboxFilterDropdownItem = ({ active, children, ...rest }: InboxFilterDropdownItemProps) => (
  <Dropdown.Item
    href="#!"
    as={Flex}
    justifyContent="between"
    className="text-capitalize cursor-pointer"
    {...rest}
  >
    {children}
    {active && <FontAwesomeIcon icon="check" transform="down-4 shrink-4" />}
  </Dropdown.Item>
);

const InboxFilterDropdown = () => {
  const {
    emailState: { filters, currentFilter },
    emailDispatch
  } = useEmailContext();

  const {
    config: { isRTL }
  } = useAppContext();

  const handleSelect = (filter: 'all' | 'unread' | 'star' | 'attachments' | 'archive' | 'snooze') => {
    emailDispatch({
      type: 'FILTER',
      payload: filter
    });
  };
  return (
    <Dropdown className="font-sans-serif">
      <Dropdown.Toggle
        variant="orkestra-default"
        size="sm"
        className="text-600 dropdown-caret-none ms-2"
      >
        <FontAwesomeIcon icon="sliders-h" />
      </Dropdown.Toggle>
      <Dropdown.Menu className="py-2" align={isRTL ? 'end' : 'start'}>
        {filters.map(filter => (
          <InboxFilterDropdownItem
            active={filter === currentFilter}
            key={filter}
            onClick={() => handleSelect(filter)}
          >
            {filter}
          </InboxFilterDropdownItem>
        ))}
      </Dropdown.Menu>
    </Dropdown>
  );
};

export default InboxFilterDropdown;

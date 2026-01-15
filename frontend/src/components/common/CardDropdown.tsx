
import { Dropdown } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { faEllipsisH } from '@fortawesome/free-solid-svg-icons';
import classNames from 'classnames';
import { useAppContext } from 'providers/AppProvider';

interface CardDropdownProps {
  btnRevealClass?: string;
  drop?: 'up' | 'down' | 'start' | 'end';
  children?: React.ReactNode;
  icon?: IconProp;
}

const CardDropdown: React.FC<CardDropdownProps> = ({
  btnRevealClass,
  drop,
  children,
  icon = faEllipsisH
}) => {
  const {
    config: { isRTL }
  } = useAppContext();

  return (
    <Dropdown
      className="font-sans-serif btn-reveal-trigger"
      align={isRTL ? 'start' : 'end'}
      drop={drop}
    >
      <Dropdown.Toggle
        variant="reveal"
        size="sm"
        data-boundary="viewport"
        className={classNames('text-600', btnRevealClass || 'btn-reveal')}
      >
        <FontAwesomeIcon icon={icon} className="fs-11" />
      </Dropdown.Toggle>
      <Dropdown.Menu className="border py-0">
        {children}
        {!children && (
          <div className="py-2">
            <Dropdown.Item>View</Dropdown.Item>
            <Dropdown.Item>Export</Dropdown.Item>
            <Dropdown.Divider />
            <Dropdown.Item className="text-danger">Remove</Dropdown.Item>
          </div>
        )}
      </Dropdown.Menu>
    </Dropdown>
  );
};

export default CardDropdown;

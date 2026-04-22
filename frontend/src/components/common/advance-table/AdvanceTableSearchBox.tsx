import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import { useState } from 'react';
import { Button, FormControl, InputGroup } from 'react-bootstrap';

interface AdvanceTableSearchBoxProps {
  placeholder?: string;
  className?: string;
}

const AdvanceTableSearchBox = ({ placeholder = 'Search...', className }: AdvanceTableSearchBoxProps) => {
  const { globalFilter, setGlobalFilter } = useAdvanceTableContext();
  const [value, setValue] = useState(globalFilter);

  const onChange = (val: string | undefined) => setGlobalFilter(val || undefined);

  return (
    <InputGroup className={classNames(className, 'position-relative')}>
      <FormControl
        value={value || ''}
        onChange={({ target: { value } }) => {
          setValue(value);
          onChange(value);
        }}
        size="sm"
        id="search"
        name="search"
        placeholder={placeholder}
        aria-label={placeholder}
        type="search"
        className="shadow-none"
      />
      <Button
        size="sm"
        variant="outline-secondary"
        className="border-300 hover-border-secondary"
      >
        <FontAwesomeIcon icon="search" className="fs-10" />
      </Button>
    </InputGroup>
  );
};

export default AdvanceTableSearchBox;

import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import { useState } from 'react';
import { Button, FormControl, InputGroup } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

interface AdvanceTableSearchBoxProps {
  placeholder?: string;
  className?: string;
}

const AdvanceTableSearchBox = ({
  placeholder,
  className
}: AdvanceTableSearchBoxProps) => {
  const { t } = useTranslation();
  const { globalFilter, setGlobalFilter } = useAdvanceTableContext();
  const [value, setValue] = useState(globalFilter);
  const effectivePlaceholder = placeholder ?? t('table.searchPlaceholder');

  const onChange = (val: string | undefined) =>
    setGlobalFilter(val || undefined);

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
        placeholder={effectivePlaceholder}
        aria-label={effectivePlaceholder}
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

import { forwardRef } from 'react';
import Select, { Props } from 'react-select';

interface MultiSelectProps extends Props {
  options: any[];
  placeholder?: string;
}

const MultiSelect = forwardRef<any, MultiSelectProps>(({ options, placeholder, ...rest }, ref) => {
  return (
    <Select
      ref={ref}
      closeMenuOnSelect={false}
      isMulti
      options={options}
      placeholder={placeholder}
      classNamePrefix="react-select"
      {...rest}
    />
  );
});

export default MultiSelect;

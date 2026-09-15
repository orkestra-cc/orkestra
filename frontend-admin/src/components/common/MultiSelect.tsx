import { forwardRef } from 'react';
import Select, { Props } from 'react-select';

interface MultiSelectProps extends Props {
  options: any[];
  placeholder?: string;
  /**
   * Opt-in Bootstrap `.form-control-sm` size parity — adds the
   * `react-select--sm` modifier class matched in
   * `assets/scss/theme/plugins/_react-select.scss` (min-height, font-size,
   * padding, dropdown-indicator sizing) against the theme's `-sm` input
   * tokens. Omit to keep the existing (unsized) rendering byte-identical —
   * every consumer besides the forms registrations filters toolbar relies
   * on that default staying untouched.
   */
  size?: 'sm';
}

const MultiSelect = forwardRef<any, MultiSelectProps>(
  ({ options, placeholder, size, className, ...rest }, ref) => {
    return (
      <Select
        ref={ref}
        closeMenuOnSelect={false}
        isMulti
        options={options}
        placeholder={placeholder}
        classNamePrefix="react-select"
        className={
          [size === 'sm' ? 'react-select--sm' : '', className]
            .filter(Boolean)
            .join(' ') || undefined
        }
        {...rest}
      />
    );
  }
);

export default MultiSelect;

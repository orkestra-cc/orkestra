// ToolbarCheck — a checkbox that actually lines up with the rest of a
// horizontal toolbar (selects, `sm` buttons, inputs).
//
// A stock `Form.Check` is a BLOCK with a floated input: the wrapper carries
// `padding-left: 24px` to clear the float, the input a `margin-top: 4px`
// tuned for a 24px line box, and the label takes `margin-bottom: 8px` from
// the theme. The wrapper ends up 32.8px tall against the 28.6px of the
// controls beside it — and since flex aligns the WRAPPER, its centre lands
// in place while the box itself sits ~4px off (the label ~3px). Adding just
// `mb-0` moves the problem one level down without solving it.
//
// Here the row is a flex container that centres its OWN children, using only
// Bootstrap utilities on React-Bootstrap's subcomponents: no custom CSS, no
// invented measurements. `ps-0` because that padding only existed for the
// float, which flex removes; `m-0` on the input because its 4px offset is
// tuned for a line box that is not there; `lh-1` on the label because its
// line box (23.2px around 13.3px text) is what misaligned it from the box.

import { Form } from 'react-bootstrap';
import classNames from 'classnames';

interface ToolbarCheckProps {
  /** Set on the input, and the target of the label's `htmlFor`. */
  id: string;
  label: React.ReactNode;
  checked: boolean;
  onChange: (checked: boolean) => void;
  /** Extra classes on the wrapper (the label inherits the colour). */
  className?: string;
  /** Classes on the label only — e.g. `text-truncate` in a fixed-width menu. */
  labelClassName?: string;
  /** `title` on the label: the full text when it is truncated. */
  labelTitle?: string;
}

const ToolbarCheck: React.FC<ToolbarCheckProps> = ({
  id,
  label,
  checked,
  onChange,
  className,
  labelClassName,
  labelTitle
}) => (
  <Form.Check
    className={classNames(
      'd-flex align-items-center gap-2 mb-0 ps-0',
      className
    )}
  >
    <Form.Check.Input
      type="checkbox"
      id={id}
      className="m-0 float-none flex-shrink-0"
      checked={checked}
      onChange={e => onChange(e.target.checked)}
    />
    <Form.Check.Label
      htmlFor={id}
      title={labelTitle}
      className={classNames('mb-0 lh-1', labelClassName)}
    >
      {label}
    </Form.Check.Label>
  </Form.Check>
);

export default ToolbarCheck;

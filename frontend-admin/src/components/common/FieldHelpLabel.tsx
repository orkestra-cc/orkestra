/**
 * FieldHelpLabel — a form field label with an inline "?" help badge.
 *
 * Renders a Bootstrap Form.Label followed by a TooltipBadge (far question-circle)
 * whose tooltip explains what the field means. Use it in any form or modal to
 * attach a hover (or keyboard-focus) explanation to a field.
 */
import { Form, OverlayTriggerProps } from 'react-bootstrap';
import TooltipBadge from 'components/common/TooltipBadge';

interface FieldHelpLabelProps {
  /** The visible field label. */
  label: string;
  /** The explanation shown in the "?" tooltip. */
  hint: string;
  /** Extra classes on the Form.Label (defaults to Bootstrap's label styling). */
  className?: string;
  /** Ties the label to a control id, when provided. */
  htmlFor?: string;
  /** Tooltip placement (default: top). */
  placement?: OverlayTriggerProps['placement'];
  /**
   * Render the label as a paragraph instead of a <label>: for a heading over
   * a group of controls that each carry their own accessible name.
   */
  as?: 'label' | 'p';
}

const FieldHelpLabel: React.FC<FieldHelpLabelProps> = ({
  label,
  hint,
  className,
  htmlFor,
  placement = 'top',
  as = 'label'
}) => (
  <Form.Label as={as} className={className} htmlFor={htmlFor}>
    {label}
    <TooltipBadge
      icon={['far', 'question-circle']}
      tooltip={hint}
      color="500"
      placement={placement}
    />
  </Form.Label>
);

export default FieldHelpLabel;

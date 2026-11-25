import { OverlayTrigger, Tooltip, OverlayTriggerProps } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

interface TooltipBadgeProps {
  placement?: OverlayTriggerProps['placement'];
  tooltip: string;
  icon: IconProp;
  color?: string;
}

const TooltipBadge = ({
  placement = 'top',
  tooltip,
  icon,
  color = 'primary'
}: TooltipBadgeProps) => {
  return (
    <OverlayTrigger
      placement={placement}
      overlay={<Tooltip style={{ position: 'fixed' }}>{tooltip}</Tooltip>}
    >
      {/*
        A focusable trigger, but deliberately NOT a <button> tag: this badge
        habitually sits *inside* another control's own <label> (Form.Check's
        `label` prop, FieldHelpLabel) — the actual field it describes is
        typically a SIBLING of that label, associated via htmlFor/id, never a
        descendant of it. dom-testing-library's getByLabelText resolves a
        label's `formControl` via `label.querySelectorAll('button, input, ...')`
        — a tag-name selector — so a real nested <button> gets MIS-attributed
        as if it were the field's own control, turning every getByLabelText
        query for that field into a "found multiple elements" false positive
        (the field's real control AND this badge both match). role="button" +
        tabIndex give the same accessible button semantics and keyboard focus
        (OverlayTrigger shows the tooltip on hover OR focus — no click/Enter
        activation to lose) without tripping that tag-name selector.
      */}
      <span
        role="button"
        tabIndex={0}
        className="btn btn-link p-0 border-0 align-baseline lh-1"
        aria-label={typeof tooltip === 'string' ? tooltip : undefined}
        // Pre-existing behaviour of the old bare <span> (not something the
        // button→span tag change introduced), but worth closing now that
        // this badge is focusable and gets clicked/spaced deliberately: a
        // <span> is not "interactive content" per the HTML label-activation
        // algorithm, so a click (or, once focused, a Space press — nothing
        // native gives a role="button" span its OWN Enter/Space activation,
        // but a browser can still route the resulting synthetic click up
        // through the label) forwards to whatever control the surrounding
        // <label> is bound to. Concretely: a "?" inside a switch's own
        // `<label htmlFor=...>` would, when clicked — or Space-pressed while
        // focused — silently flip the switch it's meant to explain.
        // preventDefault + stopPropagation swallow the gesture here instead.
        onClick={e => {
          e.preventDefault();
          e.stopPropagation();
        }}
        onKeyDown={e => {
          if (e.key === ' ') {
            e.preventDefault();
            e.stopPropagation();
          }
        }}
      >
        <FontAwesomeIcon
          icon={icon}
          transform="shrink-2"
          className={`text-${color} ms-1`}
        />
      </span>
    </OverlayTrigger>
  );
};

export default TooltipBadge;

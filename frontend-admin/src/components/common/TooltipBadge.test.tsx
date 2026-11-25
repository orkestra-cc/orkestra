import { it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import TooltipBadge from './TooltipBadge';

const badge = () => (
  <TooltipBadge icon={['far', 'question-circle']} tooltip="Explanation" />
);

it('is a keyboard-reachable button named by its tooltip', async () => {
  render(badge());
  const trigger = screen.getByRole('button', { name: 'Explanation' });
  await userEvent.tab();
  expect(trigger).toHaveFocus();
});

it('opens the tooltip on hover', async () => {
  render(badge());
  await userEvent.hover(screen.getByRole('button', { name: 'Explanation' }));
  expect(await screen.findByRole('tooltip')).toHaveTextContent('Explanation');
});

it('opens the tooltip on keyboard focus', async () => {
  render(badge());
  await userEvent.tab();
  expect(await screen.findByRole('tooltip')).toHaveTextContent('Explanation');
});

// The badge routinely sits INSIDE the <label> of the field it explains
// (Form.Check's `label` prop, FieldHelpLabel). A <span> is not interactive
// content for the HTML label-activation algorithm, so without a guard a
// click (or Space) on the badge is forwarded to the label's control.
it('a click on the badge leaves the labelled checkbox unchanged', async () => {
  const onChange = vi.fn();
  render(
    <>
      <label htmlFor="cb-click">
        Text
        {badge()}
      </label>
      <input id="cb-click" type="checkbox" onChange={onChange} />
    </>
  );
  await userEvent.click(screen.getByRole('button', { name: 'Explanation' }));
  expect(onChange).not.toHaveBeenCalled();
  expect(screen.getByRole('checkbox')).not.toBeChecked();
});

it('Space on the focused badge leaves the labelled checkbox unchanged', async () => {
  const onChange = vi.fn();
  render(
    <>
      <label htmlFor="cb-space">
        Text
        {badge()}
      </label>
      <input id="cb-space" type="checkbox" onChange={onChange} />
    </>
  );
  screen.getByRole('button', { name: 'Explanation' }).focus();
  await userEvent.keyboard(' ');
  expect(onChange).not.toHaveBeenCalled();
  expect(screen.getByRole('checkbox')).not.toBeChecked();
});

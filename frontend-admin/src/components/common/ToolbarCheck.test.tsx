import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import ToolbarCheck from './ToolbarCheck';

describe('ToolbarCheck', () => {
  it('ties the label to the checkbox and reports the new checked state', async () => {
    const onChange = vi.fn();
    renderWithProviders(
      <ToolbarCheck
        id="show-archived"
        label="Show archived"
        checked={false}
        onChange={onChange}
      />
    );

    await userEvent.click(screen.getByText('Show archived'));

    expect(onChange).toHaveBeenCalledWith(true);
    expect(
      screen.getByRole('checkbox', { name: 'Show archived' })
    ).not.toBeChecked();
  });

  it('puts labelClassName and labelTitle on the label only', () => {
    renderWithProviders(
      <ToolbarCheck
        id="col-email"
        label="Email address"
        checked
        onChange={() => {}}
        className="extra"
        labelClassName="text-truncate"
        labelTitle="Email address"
      />
    );

    const label = screen.getByText('Email address');
    expect(label).toHaveClass('text-truncate');
    expect(label).toHaveAttribute('title', 'Email address');
    expect(label.parentElement).toHaveClass('d-flex', 'extra');
    expect(screen.getByRole('checkbox')).toBeChecked();
  });
});

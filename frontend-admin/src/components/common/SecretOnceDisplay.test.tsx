import { describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import SecretOnceDisplay from './SecretOnceDisplay';

describe('SecretOnceDisplay', () => {
  it('renders the label and the secret in a monospace block, plus the warning copy', () => {
    renderWithProviders(
      <SecretOnceDisplay
        label="Client secret"
        secret="s3cr3t-value-123"
        ack={false}
        onAckChange={vi.fn()}
      />
    );

    expect(screen.getByText('Client secret')).toBeInTheDocument();
    const secretBlock = screen.getByText('s3cr3t-value-123');
    expect(secretBlock).toBeInTheDocument();
    expect(secretBlock).toHaveClass('font-monospace');

    // Warning copy (common.secretOnce.* keys) is present.
    expect(
      screen.getByText(/save this now/i, { exact: false })
    ).toBeInTheDocument();
  });

  it('renders the secondary label/value above the secret when provided', () => {
    renderWithProviders(
      <SecretOnceDisplay
        label="Client secret"
        secret="s3cr3t-value-123"
        secondaryLabel="Client ID"
        secondaryValue="client-abc-789"
        ack={false}
        onAckChange={vi.fn()}
      />
    );

    expect(screen.getByText('Client ID')).toBeInTheDocument();
    expect(screen.getByText('client-abc-789')).toBeInTheDocument();
  });

  it('does not render a secondary block when secondaryLabel/secondaryValue are absent', () => {
    renderWithProviders(
      <SecretOnceDisplay
        label="Client secret"
        secret="s3cr3t-value-123"
        ack={false}
        onAckChange={vi.fn()}
      />
    );

    expect(screen.queryByText('Client ID')).not.toBeInTheDocument();
  });

  it('copies the secret to the clipboard when the copy button is clicked', async () => {
    const user = userEvent.setup();
    const writeText = vi
      .spyOn(navigator.clipboard, 'writeText')
      .mockResolvedValue(undefined);

    renderWithProviders(
      <SecretOnceDisplay
        label="Client secret"
        secret="s3cr3t-value-123"
        ack={false}
        onAckChange={vi.fn()}
      />
    );

    await user.click(screen.getByRole('button', { name: /copy/i }));

    expect(writeText).toHaveBeenCalledWith('s3cr3t-value-123');

    writeText.mockRestore();
  });

  it('fires onAckChange when the acknowledgement checkbox is toggled', async () => {
    const user = userEvent.setup();
    const onAckChange = vi.fn();

    renderWithProviders(
      <SecretOnceDisplay
        label="Client secret"
        secret="s3cr3t-value-123"
        ack={false}
        onAckChange={onAckChange}
      />
    );

    const checkbox = screen.getByRole('checkbox');
    expect(checkbox).not.toBeChecked();

    await user.click(checkbox);

    expect(onAckChange).toHaveBeenCalledWith(true);
  });

  it('reflects the controlled ack prop rather than keeping internal state', () => {
    renderWithProviders(
      <SecretOnceDisplay
        label="Client secret"
        secret="s3cr3t-value-123"
        ack={true}
        onAckChange={vi.fn()}
      />
    );

    expect(screen.getByRole('checkbox')).toBeChecked();
  });

  it('does not render a download button', () => {
    renderWithProviders(
      <SecretOnceDisplay
        label="Client secret"
        secret="s3cr3t-value-123"
        ack={false}
        onAckChange={vi.fn()}
      />
    );

    expect(
      screen.queryByRole('button', { name: /download/i })
    ).not.toBeInTheDocument();
  });
});

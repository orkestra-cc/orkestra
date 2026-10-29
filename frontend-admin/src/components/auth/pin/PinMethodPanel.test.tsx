import { describe, it, expect, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import type { PinMethodView } from 'store/api/pinApi';
import PinMethodPanel from './PinMethodPanel';

const baseView: PinMethodView = {
  available: true,
  set: false,
  length: 5,
  devices: [],
  requiresTrustedDevice: true
};

const noop = () => Promise.resolve();

describe('PinMethodPanel', () => {
  it('without a PIN offers only "Set PIN"', () => {
    renderWithProviders(
      <PinMethodPanel
        mode="admin"
        view={baseView}
        busy={false}
        onSet={noop}
        onRemove={noop}
        onIssueCode={() => Promise.resolve({ code: 'X', expiresAt: '' })}
        onRevokeDevice={noop}
      />
    );
    expect(
      screen.getByRole('button', { name: /^set pin$/i })
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /remove pin/i })).toBeNull();
    expect(
      screen.queryByRole('button', { name: /enrollment code/i })
    ).toBeNull();
    expect(screen.getByText(/no device enrolled/i)).toBeInTheDocument();
  });

  it('validates the length before calling onSet, then submits the PIN', async () => {
    const onSet = vi.fn(() => Promise.resolve());
    renderWithProviders(
      <PinMethodPanel
        mode="self"
        view={baseView}
        busy={false}
        onSet={onSet}
        onRemove={noop}
        onIssueCode={() => Promise.resolve({ code: 'X', expiresAt: '' })}
        onRevokeDevice={noop}
      />
    );
    fireEvent.click(screen.getByRole('button', { name: /^set pin$/i }));
    const [pin, confirm] = await screen.findAllByLabelText(/pin/i, {
      selector: 'input'
    });
    fireEvent.change(pin, { target: { value: '123' } });
    fireEvent.change(confirm, { target: { value: '123' } });
    fireEvent.click(screen.getByRole('button', { name: /save pin/i }));
    expect(await screen.findByText(/exactly 5 digits/i)).toBeInTheDocument();
    expect(onSet).not.toHaveBeenCalled();

    fireEvent.change(pin, { target: { value: '48213' } });
    fireEvent.change(confirm, { target: { value: '48213' } });
    fireEvent.click(screen.getByRole('button', { name: /save pin/i }));
    await waitFor(() => expect(onSet).toHaveBeenCalledWith('48213'));
  });

  it('shows a minted enrollment code once', async () => {
    renderWithProviders(
      <PinMethodPanel
        mode="admin"
        view={{ ...baseView, set: true, updatedAt: '2026-10-29T08:00:00Z' }}
        busy={false}
        onSet={noop}
        onRemove={noop}
        onIssueCode={() =>
          Promise.resolve({
            code: 'ABCD-EF23',
            expiresAt: '2026-10-29T08:15:00Z'
          })
        }
        onRevokeDevice={noop}
      />
    );
    fireEvent.click(screen.getByRole('button', { name: /enrollment code/i }));
    expect(await screen.findByTestId('pin-enrollment-code')).toHaveTextContent(
      'ABCD-EF23'
    );
  });

  it('lists devices and revokes one', async () => {
    const onRevokeDevice = vi.fn(() => Promise.resolve());
    renderWithProviders(
      <PinMethodPanel
        mode="admin"
        view={{
          ...baseView,
          set: true,
          devices: [
            {
              id: 'd1',
              deviceId: 'tab-1',
              deviceName: 'Tablet magazzino',
              platform: 'android',
              via: 'admin',
              createdAt: '2026-10-29T08:00:00Z'
            }
          ]
        }}
        busy={false}
        onSet={noop}
        onRemove={noop}
        onIssueCode={() => Promise.resolve({ code: 'X', expiresAt: '' })}
        onRevokeDevice={onRevokeDevice}
      />
    );
    expect(screen.getByText('Tablet magazzino')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /revoke/i }));
    await waitFor(() => expect(onRevokeDevice).toHaveBeenCalledWith('tab-1'));
  });

  it('says when the surface has the method off', () => {
    renderWithProviders(
      <PinMethodPanel
        mode="admin"
        view={{ ...baseView, available: false }}
        busy={false}
        onSet={noop}
        onRemove={noop}
        onIssueCode={() => Promise.resolve({ code: 'X', expiresAt: '' })}
        onRevokeDevice={noop}
      />
    );
    expect(
      screen.getByText(/switched off on this surface/i)
    ).toBeInTheDocument();
  });
});

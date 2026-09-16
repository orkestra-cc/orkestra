import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import ExportFormatMenu from './ExportFormatMenu';

const labels = {
  menu: 'Esporta',
  exporting: 'Esportazione…',
  csv: 'CSV',
  xlsx: 'Excel'
};

describe('ExportFormatMenu', () => {
  it('single scope: no section header, csv + xlsx items call onExport', async () => {
    const onExport = vi.fn().mockResolvedValue(undefined);
    renderWithProviders(
      <ExportFormatMenu
        scopes={[{ key: 'all', label: 'Tutto' }]}
        onExport={onExport}
        labels={labels}
      />
    );
    await userEvent.click(screen.getByRole('button', { name: 'Esporta' }));
    expect(screen.queryByText('Tutto')).toBeNull(); // no header for a lone scope
    await userEvent.click(screen.getByText('CSV'));
    expect(onExport).toHaveBeenCalledWith('all', 'csv');
  });

  it('two scopes: headers with counts, disabled scope disables its items', async () => {
    const onExport = vi.fn().mockResolvedValue(undefined);
    renderWithProviders(
      <ExportFormatMenu
        scopes={[
          { key: 'view', label: 'Vista corrente (0)', disabled: true },
          { key: 'all', label: 'Tutte (12)' }
        ]}
        onExport={onExport}
        labels={labels}
      />
    );
    await userEvent.click(screen.getByRole('button', { name: 'Esporta' }));
    expect(screen.getByText('Vista corrente (0)')).toBeInTheDocument();
    expect(screen.getByText('Tutte (12)')).toBeInTheDocument();
    const items = screen.getAllByRole('button', { name: 'Excel' });
    expect(items[0]).toHaveAttribute('aria-disabled', 'true');
    await userEvent.click(items[1]);
    expect(onExport).toHaveBeenCalledWith('all', 'xlsx');
  });

  it('shows the exporting label and disables the toggle while onExport is pending, and resets on rejection', async () => {
    let resolve!: () => void;
    const onExport = vi.fn(() => new Promise<void>(r => (resolve = r)));
    renderWithProviders(
      <ExportFormatMenu
        scopes={[{ key: 'all', label: 'Tutto' }]}
        onExport={onExport}
        labels={labels}
      />
    );
    await userEvent.click(screen.getByRole('button', { name: 'Esporta' }));
    await userEvent.click(screen.getByText('CSV'));
    expect(
      screen.getByRole('button', { name: 'Esportazione…' })
    ).toBeDisabled();
    resolve();
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Esporta' })).toBeEnabled()
    );
  });

  it('resets busy to the menu label even when onExport rejects, logging the rejection instead of swallowing it silently', async () => {
    // The primitive logs a rejecting onExport (import.meta.env.DEV is true
    // under Vitest) so a caller that forgot its own error handling leaves a
    // trace — assert on the spy rather than let it print, so test output
    // stays pristine while still proving the signal actually fires.
    const consoleError = vi
      .spyOn(console, 'error')
      .mockImplementation(() => {});

    let reject!: (err: unknown) => void;
    const rejectionError = new Error('export failed');
    const onExport = vi.fn(
      () =>
        new Promise<void>((_resolve, r) => {
          reject = r;
        })
    );
    renderWithProviders(
      <ExportFormatMenu
        scopes={[{ key: 'all', label: 'Tutto' }]}
        onExport={onExport}
        labels={labels}
      />
    );
    await userEvent.click(screen.getByRole('button', { name: 'Esporta' }));
    await userEvent.click(screen.getByText('CSV'));
    expect(
      screen.getByRole('button', { name: 'Esportazione…' })
    ).toBeDisabled();

    reject(rejectionError);
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Esporta' })).toBeEnabled()
    );

    expect(consoleError).toHaveBeenCalledTimes(1);
    expect(consoleError).toHaveBeenCalledWith(
      expect.stringContaining('[ExportFormatMenu]'),
      rejectionError
    );

    consoleError.mockRestore();
  });
});

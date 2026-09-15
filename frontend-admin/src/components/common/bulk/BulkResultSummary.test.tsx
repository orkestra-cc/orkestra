import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import BulkResultSummary from './BulkResultSummary';

const results = [
  { id: 'r1', ok: true, status: 'approved' },
  { id: 'r2', ok: false, error_code: 'x.state_conflict' },
  { id: 'r3', ok: false, error_code: 'x.transition_invalid' }
];

// The host's own map: known codes translate, anything else falls back.
const errorLabel = (code?: string) =>
  code === 'x.state_conflict'
    ? 'Row already decided'
    : code === 'x.transition_invalid'
      ? 'Transition not allowed'
      : undefined;

describe('BulkResultSummary', () => {
  it('stays on the page and reports counts plus the failed rows with their reason', () => {
    render(
      <BulkResultSummary
        results={results}
        onRetryFailed={vi.fn()}
        onDismiss={vi.fn()}
        errorLabel={errorLabel}
      />
    );
    // Not a toast: it lives in an announced region and never fades by itself.
    const region = screen.getByRole('status');
    expect(region).toHaveTextContent('1');
    expect(region).toHaveTextContent('2');
    expect(screen.getByText(/r2/)).toBeInTheDocument();
    expect(screen.getByText(/Row already decided/)).toBeInTheDocument();
    expect(screen.getByText(/r3/)).toBeInTheDocument();
    expect(screen.getByText(/Transition not allowed/)).toBeInTheDocument();
  });

  it('offers «Retry failed» only when there are failed rows', async () => {
    const onRetry = vi.fn();
    const { rerender } = render(
      <BulkResultSummary
        results={results}
        onRetryFailed={onRetry}
        onDismiss={vi.fn()}
        errorLabel={errorLabel}
      />
    );
    // Bilingual regex: i18n.ts boots the test env in English (`lng: 'en'`).
    await userEvent.click(
      screen.getByRole('button', { name: /riprova|retry/i })
    );
    expect(onRetry).toHaveBeenCalledWith(['r2', 'r3']);

    rerender(
      <BulkResultSummary
        results={[{ id: 'r1', ok: true }]}
        onRetryFailed={onRetry}
        onDismiss={vi.fn()}
        errorLabel={errorLabel}
      />
    );
    expect(
      screen.queryByRole('button', { name: /riprova|retry/i })
    ).not.toBeInTheDocument();
  });

  it('never shows a raw code or an empty reason for an unmapped error', () => {
    render(
      <BulkResultSummary
        results={[
          { id: 'r9', ok: false, error_code: 'zzz.unknown_code' },
          { id: 'r10', ok: false }
        ]}
        onRetryFailed={vi.fn()}
        onDismiss={vi.fn()}
        errorLabel={errorLabel}
      />
    );
    const region = screen.getByRole('status');
    expect(region).not.toHaveTextContent('zzz.unknown_code');
    // Both rows carry the generic fallback: the operator always reads a
    // translated reason.
    expect(
      screen.getAllByText(/operazione non riuscita|the operation failed/i)
    ).toHaveLength(2);
  });

  it('renders nothing without results', () => {
    const { container } = render(
      <BulkResultSummary
        results={[]}
        onRetryFailed={vi.fn()}
        onDismiss={vi.fn()}
        errorLabel={errorLabel}
      />
    );
    expect(container).toBeEmptyDOMElement();
  });
});

import { render, screen, fireEvent } from '@testing-library/react';
import { describe, expect, it, vi, beforeAll } from 'vitest';
import type { ConfigField } from 'store/api/moduleApi';
import i18n from '../../../../i18n';
import { RecordListField } from './RecordListField';
import { mintSlug } from './mintSlug';

const field = {
  key: 'email.profiles',
  label: 'Profiles',
  description: '',
  type: 'recordList',
  required: false,
  default: '',
  envVar: '',
  items: [{ key: 'host', label: 'Host', type: 'string', required: false }]
} as ConfigField;

const noop = () => undefined;

const renderField = (
  over: Partial<React.ComponentProps<typeof RecordListField>> = {}
) =>
  render(
    <RecordListField
      field={field}
      moduleName="demo"
      roster={[]}
      labels={{}}
      staged={[]}
      onCreate={vi.fn()}
      onStageRemove={vi.fn()}
      onUndoRemove={vi.fn()}
      renderElement={noop as never}
      {...over}
    />
  );

beforeAll(async () => {
  await i18n.changeLanguage('en');
});

describe('RecordListField', () => {
  it('renders one card per roster entry', () => {
    renderField({ roster: ['a', 'b'], labels: { a: 'Primary', b: 'Backup' } });
    expect(screen.getByText('Primary')).toBeInTheDocument();
    expect(screen.getByText('Backup')).toBeInTheDocument();
  });

  it('stages a removal instead of deleting immediately, and offers Undo', () => {
    const onStageRemove = vi.fn();
    const onUndoRemove = vi.fn();
    const { rerender } = render(
      <RecordListField
        field={field}
        moduleName="demo"
        roster={['a']}
        labels={{ a: 'Primary' }}
        staged={[]}
        onCreate={vi.fn()}
        onStageRemove={onStageRemove}
        onUndoRemove={onUndoRemove}
        renderElement={noop as never}
      />
    );
    fireEvent.click(screen.getByRole('button', { name: /remove/i }));
    expect(onStageRemove).toHaveBeenCalledWith('a');

    rerender(
      <RecordListField
        field={field}
        moduleName="demo"
        roster={['a']}
        labels={{ a: 'Primary' }}
        staged={['a']}
        onCreate={vi.fn()}
        onStageRemove={onStageRemove}
        onUndoRemove={onUndoRemove}
        renderElement={noop as never}
      />
    );
    // Still visible while staged — Undo needs something to point at.
    expect(screen.getByText('Primary')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /undo/i }));
    expect(onUndoRemove).toHaveBeenCalledWith('a');
  });

  it('previews the slug minted from the typed name', () => {
    renderField();
    fireEvent.click(screen.getByRole('button', { name: /add/i }));
    fireEvent.change(screen.getByLabelText(/name/i), {
      target: { value: 'MailUp SMTP+' }
    });
    expect(screen.getByText('mailup-smtp')).toBeInTheDocument();
  });

  it('refuses a name that mints nothing, and one already taken', () => {
    const onCreate = vi.fn();
    renderField({ roster: ['taken'], labels: { taken: 'Taken' }, onCreate });
    fireEvent.click(screen.getByRole('button', { name: /add/i }));
    const input = screen.getByLabelText(/name/i);

    fireEvent.change(input, { target: { value: '🙂' } });
    fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }));
    expect(onCreate).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: 'Taken' } });
    fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }));
    expect(onCreate).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: 'Fresh One' } });
    fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }));
    expect(onCreate).toHaveBeenCalledWith('fresh-one', 'Fresh One');
  });

  it('renders each element body through the supplied renderer', () => {
    const renderElement = vi.fn((slug: string) => <span>body-{slug}</span>);
    renderField({ roster: ['a'], labels: { a: 'A' }, renderElement });
    expect(screen.getByText('body-a')).toBeInTheDocument();
  });
});

// The preview the operator reads must be the slug the backend actually mints,
// or the key they were shown is not the key they get.
describe('mintSlug', () => {
  it('matches the Go algorithm', () => {
    expect(mintSlug('MailUp SMTP+')).toBe('mailup-smtp');
    expect(mintSlug('  Città  Aperta  ')).toBe('citta-aperta');
    expect(mintSlug('SES — bulk (2026)')).toBe('ses-bulk-2026');
    expect(mintSlug('already-a-slug')).toBe('already-a-slug');
    expect(mintSlug('___')).toBe('');
    expect(mintSlug('🙂')).toBe('');
  });

  it('truncates at 64 and never leaves a trailing dash', () => {
    const got = mintSlug('ab '.repeat(40));
    expect(got).toHaveLength(64);
    expect(got.endsWith('-')).toBe(false);
  });
});

import { describe, expect, it } from 'vitest';
import { isQuietLlmError, llmCodeMessage, llmErrorMessage } from './llmErrors';

const t = (key: string) =>
  ({
    'errors.llm.invalid_request': 'Some fields are invalid.',
    'errors.llm.not_found': 'Not found (translated).',
    'adminLlm.errors.generic': 'The operation failed.'
  })[key] ?? key;

describe('llmErrorMessage', () => {
  it('shows the written detail of a validation error instead of the generic line', () => {
    const detail =
      'The credential belongs to a different provider than the model.';
    expect(
      llmErrorMessage(t, { data: { code: 'llm.invalid_request', detail } })
    ).toBe(detail);
  });

  it('falls back to the translated copy when a validation error has no detail', () => {
    expect(llmErrorMessage(t, { data: { code: 'llm.invalid_request' } })).toBe(
      'Some fields are invalid.'
    );
  });

  it('keeps the translated copy for every other code', () => {
    expect(
      llmErrorMessage(t, {
        data: { code: 'llm.not_found', detail: 'English detail' }
      })
    ).toBe('Not found (translated).');
  });

  it('uses the detail, then the generic line, when the code has no copy', () => {
    expect(
      llmErrorMessage(t, { data: { code: 'llm.other', detail: 'Detail.' } })
    ).toBe('Detail.');
    expect(llmErrorMessage(t, {})).toBe('The operation failed.');
  });
});

describe('llmCodeMessage', () => {
  it('translates a bare code, falling back to the generic line', () => {
    expect(llmCodeMessage(t, 'llm.not_found')).toBe('Not found (translated).');
    expect(llmCodeMessage(t, 'llm.unknown')).toBe('The operation failed.');
    expect(llmCodeMessage(t, undefined)).toBe('The operation failed.');
  });
});

describe('isQuietLlmError', () => {
  it('is quiet for a caller cancel and a dismissed step-up only', () => {
    expect(isQuietLlmError({ data: { code: 'llm.request_canceled' } })).toBe(
      true
    );
    expect(isQuietLlmError({ data: { code: 'step_up_required' } })).toBe(true);
    expect(isQuietLlmError({ data: { code: 'llm.rate_limited' } })).toBe(false);
    expect(isQuietLlmError(undefined)).toBe(false);
  });
});

import { describe, expect, it } from 'vitest';
import { isReauthCancelled, retrievalErrorMessage } from './retrievalErrors';

const t = (key: string) =>
  ({
    'errors.retrieval.corpus_registered': 'Still registered (translated).',
    'adminRetrieval.errors.generic': 'The operation failed.'
  })[key] ?? key;

describe('retrievalErrorMessage', () => {
  it('maps a retrieval code to its translation', () => {
    expect(
      retrievalErrorMessage(t, {
        data: { code: 'retrieval.corpus_registered', detail: 'English' }
      })
    ).toBe('Still registered (translated).');
  });

  it('falls back to the backend detail, then to the generic line', () => {
    expect(
      retrievalErrorMessage(t, {
        data: { code: 'retrieval.unknown_new', detail: 'Written detail' }
      })
    ).toBe('Written detail');
    expect(retrievalErrorMessage(t, { data: {} })).toBe(
      'The operation failed.'
    );
    expect(retrievalErrorMessage(t, undefined)).toBe('The operation failed.');
  });
});

describe('isReauthCancelled', () => {
  it('is re-exported from the llm helpers', () => {
    expect(isReauthCancelled({ data: { code: 'step_up_required' } })).toBe(
      true
    );
    expect(isReauthCancelled({ data: { code: 'retrieval.internal' } })).toBe(
      false
    );
  });
});

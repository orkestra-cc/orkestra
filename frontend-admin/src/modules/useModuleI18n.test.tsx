import { render, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import i18n from '../i18n';
import { useModuleI18nInjection } from './useModuleI18n';

// The hook reads the live `moduleCatalog`, which ships empty in the
// core-only base. Mock it with a fake addon manifest that contributes an
// i18n bundle, then assert the namespace lands in i18next (ADR-0007).
// `vi.hoisted` makes the spy reachable from the hoisted `vi.mock` factory.
const { injectI18n } = vi.hoisted(() => ({
  injectI18n: vi.fn(async () => ({
    en: { greeting: 'Hello' },
    it: { greeting: 'Ciao' }
  }))
}));

vi.mock('modules', () => ({
  moduleCatalog: {
    widgets: { name: 'widgets', routes: () => [], injectI18n }
  }
}));

function Harness() {
  useModuleI18nInjection();
  return null;
}

describe('useModuleI18nInjection', () => {
  afterEach(() => {
    i18n.removeResourceBundle('en', 'widgets');
    i18n.removeResourceBundle('it', 'widgets');
    injectI18n.mockClear();
  });

  it('registers each catalogued addon bundle under its module namespace', async () => {
    render(<Harness />);

    await waitFor(() => {
      expect(i18n.hasResourceBundle('en', 'widgets')).toBe(true);
      expect(i18n.hasResourceBundle('it', 'widgets')).toBe(true);
    });

    expect(i18n.getResource('en', 'widgets', 'greeting')).toBe('Hello');
    expect(i18n.getResource('it', 'widgets', 'greeting')).toBe('Ciao');
  });

  it('does not re-inject across re-renders of the same tree', async () => {
    const { rerender } = render(<Harness />);
    await waitFor(() =>
      expect(i18n.hasResourceBundle('en', 'widgets')).toBe(true)
    );

    rerender(<Harness />);
    expect(injectI18n).toHaveBeenCalledTimes(1);
  });
});

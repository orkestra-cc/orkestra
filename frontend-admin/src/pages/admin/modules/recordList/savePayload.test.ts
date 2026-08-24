import { describe, expect, it } from 'vitest';
import { buildSavePayload } from './buildSavePayload';

describe('buildSavePayload', () => {
  it('sends created and removed slugs as explicit intent', () => {
    const out = buildSavePayload({
      config: { 'email.profiles.b.host': 'smtp.b' },
      secrets: {},
      created: { 'email.profiles': ['b'] },
      stagedRemovals: { 'email.profiles': ['a'] },
      revision: 7
    });
    expect(out.recordLists).toEqual([
      { field: 'email.profiles', create: ['b'], remove: ['a'] }
    ]);
    expect(out.revision).toBe(7);
    expect(out.config).toEqual({ 'email.profiles.b.host': 'smtp.b' });
  });

  it('omits the record-list block entirely when membership did not change', () => {
    const out = buildSavePayload({
      config: { 'email.profiles.a.host': 'x' },
      secrets: {},
      created: {},
      stagedRemovals: {},
      revision: 7
    });
    expect(out.recordLists).toBeUndefined();
    expect(out.revision).toBeUndefined();
  });

  it('sends the revision even when it is zero, for pre-feature documents', () => {
    const out = buildSavePayload({
      config: {},
      secrets: {},
      created: {},
      stagedRemovals: { 'email.profiles': ['a'] },
      revision: 0
    });
    expect(out.revision).toBe(0);
  });

  // The revision only guards removals. Sending it on a pure add would turn a
  // concurrent, perfectly compatible add by another operator into a 409 —
  // exactly the case the backend's retry exists to absorb.
  it('omits the revision on an add-only change', () => {
    const out = buildSavePayload({
      config: {},
      secrets: {},
      created: { 'email.profiles': ['b'] },
      stagedRemovals: {},
      revision: 7
    });
    expect(out.recordLists).toEqual([
      { field: 'email.profiles', create: ['b'], remove: [] }
    ]);
    expect(out.revision).toBeUndefined();
  });

  it('carries one entry per field that changed, and none for fields that did not', () => {
    const out = buildSavePayload({
      config: {},
      secrets: {},
      created: { 'a.list': ['x'], 'b.list': [] },
      stagedRemovals: { 'c.list': ['y'] },
      revision: 3
    });
    expect(out.recordLists?.map(m => m.field).sort()).toEqual([
      'a.list',
      'c.list'
    ]);
  });

  it('drops empty config and secrets blocks so a membership-only save sends neither', () => {
    const out = buildSavePayload({
      config: {},
      secrets: {},
      created: { 'email.profiles': ['b'] },
      stagedRemovals: {},
      revision: 1
    });
    expect(out.config).toBeUndefined();
    expect(out.secrets).toBeUndefined();
  });
});

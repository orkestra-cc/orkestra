import { describe, it, expect } from 'vitest';
import type { ConfigField } from 'store/api/moduleApi';
import {
  buildYupSchema,
  buildDefaults,
  collectDiff
} from './useModuleConfigForm';

const field = (over: Partial<ConfigField> & { key: string }): ConfigField => ({
  label: over.key,
  description: '',
  type: 'string',
  required: false,
  default: '',
  envVar: '',
  ...over
});

describe('buildDefaults', () => {
  it('prefers the stored value over the schema default', () => {
    const schema = [field({ key: 'a', default: 'fallback' })];
    expect(buildDefaults(schema, { a: 'stored' })).toEqual({ a: 'stored' });
  });

  it('falls back to the schema default when nothing is stored', () => {
    const schema = [field({ key: 'a', default: 'fallback' })];
    expect(buildDefaults(schema, {})).toEqual({ a: 'fallback' });
  });

  it('seeds secrets as empty regardless of stored state', () => {
    // A stored secret is never sent to the client — only secretStatus is. An
    // empty field means "keep what is stored"; pre-filling anything here would
    // either leak or overwrite with a placeholder.
    const schema = [field({ key: 's', type: 'secret', default: 'nope' })];
    expect(buildDefaults(schema, { s: 'nope' })).toEqual({ s: '' });
  });
});

describe('buildYupSchema', () => {
  const validate = async (
    schema: ConfigField[],
    values: Record<string, string>
  ) => {
    try {
      await buildYupSchema(schema).validate(values, { abortEarly: false });
      return [] as string[];
    } catch (err) {
      return (err as { errors: string[] }).errors;
    }
  };

  it('accepts a value inside declared bounds', async () => {
    const schema = [field({ key: 'n', type: 'int', min: 8, max: 128 })];
    expect(await validate(schema, { n: '12' })).toEqual([]);
  });

  it('rejects below min and above max', async () => {
    const schema = [field({ key: 'n', type: 'int', min: 8, max: 128 })];
    expect((await validate(schema, { n: '4' })).length).toBe(1);
    expect((await validate(schema, { n: '999' })).length).toBe(1);
  });

  it('rejects a required field left empty', async () => {
    const schema = [field({ key: 'r', required: true })];
    expect((await validate(schema, { r: '' })).length).toBe(1);
  });

  it('does NOT require a required field that is hidden', async () => {
    // The operator cannot see it, so demanding it makes the form unsavable
    // with no visible cause.
    const schema = [
      field({ key: 'on', type: 'bool', default: 'false' }),
      field({
        key: 'hidden',
        required: true,
        dependsOn: [{ key: 'on', in: ['true'] }]
      })
    ];
    expect(await validate(schema, { on: 'false', hidden: '' })).toEqual([]);
    expect((await validate(schema, { on: 'true', hidden: '' })).length).toBe(1);
  });

  it('rejects a value failing a declared pattern', async () => {
    const schema = [field({ key: 'p', pattern: '^[a-z]+$' })];
    expect((await validate(schema, { p: 'ABC' })).length).toBe(1);
    expect(await validate(schema, { p: 'abc' })).toEqual([]);
  });

  it('ignores an uncompilable pattern rather than throwing', async () => {
    const schema = [field({ key: 'p', pattern: '([' })];
    expect(await validate(schema, { p: 'anything' })).toEqual([]);
  });

  it('accepts every duration Go accepts', async () => {
    const schema = [field({ key: 'd', type: 'duration' })];
    for (const ok of ['30s', '1h30m', '500ms', '1.5h', '-5m', '0']) {
      expect(await validate(schema, { d: ok })).toEqual([]);
    }
    for (const bad of ['30 s', '15x', '1H', 'h']) {
      expect((await validate(schema, { d: bad })).length).toBe(1);
    }
  });

  it('treats an empty optional value as valid', async () => {
    const schema = [field({ key: 'n', type: 'int', min: 8 })];
    expect(await validate(schema, { n: '' })).toEqual([]);
  });
});

describe('collectDiff', () => {
  const schema = [
    field({ key: 'a' }),
    field({ key: 'b' }),
    field({ key: 's', type: 'secret' }),
    field({ key: 'on', type: 'bool', default: 'false' }),
    field({ key: 'hidden', dependsOn: [{ key: 'on', in: ['true'] }] })
  ];
  const defaults = { a: 'one', b: 'two', s: '', on: 'false', hidden: 'stale' };

  it('sends only changed non-secret keys', () => {
    const { config } = collectDiff(
      schema,
      { ...defaults, a: 'changed' },
      defaults
    );
    expect(config).toEqual({ a: 'changed' });
  });

  it('sends a secret only when non-empty', () => {
    expect(
      collectDiff(schema, { ...defaults, s: '' }, defaults).secrets
    ).toEqual({});
    expect(
      collectDiff(schema, { ...defaults, s: 'new' }, defaults).secrets
    ).toEqual({ s: 'new' });
  });

  it('never sends a hidden field, even when its value differs', () => {
    // Switching a provider off must not write back an edit to a field the
    // operator can no longer see — nor discard what is already stored.
    const { config } = collectDiff(
      schema,
      { ...defaults, hidden: 'edited' },
      defaults
    );
    expect(config).toEqual({});
  });

  it('sends a field that became visible and was then edited', () => {
    const values = { ...defaults, on: 'true', hidden: 'edited' };
    const { config } = collectDiff(schema, values, defaults);
    expect(config).toEqual({ on: 'true', hidden: 'edited' });
  });

  it('returns empty objects when nothing changed', () => {
    const { config, secrets } = collectDiff(schema, defaults, defaults);
    expect(config).toEqual({});
    expect(secrets).toEqual({});
  });
});

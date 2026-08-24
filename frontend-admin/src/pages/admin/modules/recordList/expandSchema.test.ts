import { describe, expect, it } from 'vitest';
import type { ConfigField } from 'store/api/moduleApi';
import { expandElement, expandSchema, rosterOf } from './expandSchema';

const schema = [
  { key: 'apiKey', label: 'API key', type: 'secret' },
  {
    key: 'email.profiles',
    label: 'Profiles',
    type: 'recordList',
    items: [
      {
        key: 'provider',
        label: 'Provider',
        type: 'enum',
        options: ['smtp', 'noop']
      },
      {
        key: 'host',
        label: 'Host',
        type: 'string',
        required: true,
        dependsOn: [{ key: 'provider', in: ['smtp'] }]
      },
      { key: 'password', label: 'Password', type: 'secret' }
    ]
  }
] as unknown as ConfigField[];

describe('rosterOf', () => {
  it('reads the roster in order', () => {
    expect(
      rosterOf({ 'email.profiles.__items': 'b,a' }, 'email.profiles')
    ).toEqual(['b', 'a']);
    expect(rosterOf({}, 'email.profiles')).toEqual([]);
  });

  it('ignores blank entries and surrounding whitespace', () => {
    expect(
      rosterOf({ 'email.profiles.__items': ' a , , b ' }, 'email.profiles')
    ).toEqual(['a', 'b']);
  });
});

describe('expandSchema', () => {
  it('replaces a record list with one concrete field per element', () => {
    const out = expandSchema(schema, { 'email.profiles.__items': 'a,b' });
    const keys = out.map(f => f.key);
    expect(keys).toContain('apiKey');
    expect(keys).not.toContain('email.profiles');
    expect(keys).toEqual(
      expect.arrayContaining([
        'email.profiles.a.__label',
        'email.profiles.a.host',
        'email.profiles.a.password',
        'email.profiles.b.host'
      ])
    );
  });

  it('drops the list entirely when the roster is empty', () => {
    const out = expandSchema(schema, {});
    expect(out.map(f => f.key)).toEqual(['apiKey']);
  });

  it('carries the sub-field type and required flag onto the concrete field', () => {
    const out = expandSchema(schema, { 'email.profiles.__items': 'a' });
    const host = out.find(f => f.key === 'email.profiles.a.host');
    expect(host).toMatchObject({ type: 'string', required: true });
    const pw = out.find(f => f.key === 'email.profiles.a.password');
    expect(pw).toMatchObject({ type: 'secret' });
  });

  // A sub-field's condition names a SIBLING sub-key ('provider'), but after
  // expansion the values map is keyed by the full expanded key. Left
  // unrewritten the condition resolves against a key that never exists, and
  // every conditional field inside an element is invisible forever.
  it('rewrites an item condition onto its own element', () => {
    const out = expandSchema(schema, { 'email.profiles.__items': 'a,b' });
    const hostA = out.find(f => f.key === 'email.profiles.a.host');
    expect(hostA?.dependsOn).toEqual([
      { key: 'email.profiles.a.provider', in: ['smtp'] }
    ]);
    const hostB = out.find(f => f.key === 'email.profiles.b.host');
    expect(hostB?.dependsOn).toEqual([
      { key: 'email.profiles.b.provider', in: ['smtp'] }
    ]);
  });

  it('preserves roster order across elements', () => {
    const out = expandSchema(schema, { 'email.profiles.__items': 'b,a' });
    const keys = out.map(f => f.key).filter(k => k.endsWith('.__label'));
    expect(keys).toEqual([
      'email.profiles.b.__label',
      'email.profiles.a.__label'
    ]);
  });
});

describe('expandElement', () => {
  it('returns just that element, label first', () => {
    const list = schema[1];
    const out = expandElement(list, 'a');
    expect(out.map(f => f.key)).toEqual([
      'email.profiles.a.__label',
      'email.profiles.a.provider',
      'email.profiles.a.host',
      'email.profiles.a.password'
    ]);
  });
});

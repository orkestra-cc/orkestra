import { describe, it, expect } from 'vitest';
import type { ConfigField } from 'store/api/moduleApi';
import {
  buildYupSchema,
  buildDefaults,
  buildFieldNames,
  collectDiff,
  fieldNameOf,
  toSchemaValues
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

// The real `notification` schema, verbatim from
// backend/internal/core/notification/module.go — 11 of 11 keys dotted, which
// is the module the reported bug made impossible to configure. Used here (and
// nowhere else) so the unit layer is pinned to production data rather than to
// a fixture someone might "simplify" back into dot-free keys.
const NOTIFICATION_SCHEMA: ConfigField[] = [
  field({ key: 'email.provider', required: true, default: 'noop' }),
  field({ key: 'email.from_address' }),
  field({ key: 'email.from_name', default: 'Orkestra' }),
  field({ key: 'email.reply_to' }),
  field({ key: 'email.smtp.host' }),
  field({ key: 'email.smtp.port', type: 'int', default: '587' }),
  field({ key: 'email.smtp.username' }),
  field({ key: 'email.smtp.password', type: 'secret' }),
  field({ key: 'email.smtp.tls_mode', default: 'starttls' }),
  field({ key: 'app.name', default: 'Orkestra' }),
  field({ key: 'app.support_email' })
];

describe('buildFieldNames', () => {
  // react-hook-form parses "." as a path separator, so `register('a.b')`
  // writes to {a:{b}} while every consumer here reads the flat 'a.b'. This
  // mapping is what keeps RHF from ever seeing one.
  it('strips every character react-hook-form would read as path syntax', () => {
    const names = buildFieldNames([
      field({ key: 'email.smtp.host' }),
      field({ key: 'list[0]' }),
      field({ key: 'a,b' }),
      field({ key: "quote'd" }),
      field({ key: 'pipe|d' })
    ]);
    // RHF's own `isKey` is /^\w*$/ — a name that matches is treated as one
    // literal property instead of parsed as a path, by both get and set.
    for (const name of names.values()) {
      expect(name).toMatch(/^\w+$/);
    }
    expect(names.get('email.smtp.host')).toBe('email_smtp_host');
  });

  it('keeps a key that is already safe untouched', () => {
    // Every module migrated before this fix (auth is camelCase) must keep the
    // exact names it already registers, or its dirty tracking silently resets.
    const names = buildFieldNames([
      field({ key: 'minLength' }),
      field({ key: 'already_safe' })
    ]);
    expect(names.get('minLength')).toBe('minLength');
    expect(names.get('already_safe')).toBe('already_safe');
  });

  it('never collapses two distinct keys onto one name', () => {
    // Sanitising alone is not enough: 'a.b' and 'a_b' both sanitise to 'a_b'.
    // Two fields sharing a register name would make one shadow the other's
    // value, dirty state and validation error.
    const schema = [
      field({ key: 'a.b' }),
      field({ key: 'a_b' }),
      field({ key: 'a-b' }),
      field({ key: 'a_b_2' })
    ];
    const names = buildFieldNames(schema);
    expect(new Set(names.values()).size).toBe(schema.length);
  });

  it('is a deterministic function of the schema', () => {
    // Two callers deriving it independently must agree, which is what makes
    // the argument optional everywhere without becoming a correctness trap.
    expect([...buildFieldNames(NOTIFICATION_SCHEMA)]).toEqual([
      ...buildFieldNames(NOTIFICATION_SCHEMA)
    ]);
  });

  it("maps notification's real 11 keys to 11 distinct safe names", () => {
    // Pinned to production data, not to a fixture: this is the module the bug
    // made unconfigurable, and it is the shape a 12th key would have to keep.
    const names = buildFieldNames(NOTIFICATION_SCHEMA);
    expect(names.size).toBe(11);
    expect(new Set(names.values()).size).toBe(11);
    for (const name of names.values()) expect(name).toMatch(/^\w+$/);
  });

  it('sanitises punctuation char-for-char rather than dropping it', () => {
    // Pins what the `|| 'field'` fallback does NOT cover: the replace is
    // char-for-char, so punctuation becomes underscores and never collapses
    // to ''. Reading the fallback as "guards all-punctuation keys" would
    // make it look dead and invite its deletion.
    expect(buildFieldNames([field({ key: '...' })]).get('...')).toBe('___');
  });

  it('gives an empty key a usable name — the one case the fallback guards', () => {
    // `register('')` is accepted by RHF as a field that renders and can never
    // be dirtied, so an empty key must not produce an empty name.
    expect(buildFieldNames([field({ key: '' })]).get('')).toBe('field');
  });
});

describe('fieldNameOf', () => {
  it('throws, naming the key, rather than degrading to the raw key', () => {
    // The whole point: `fieldNames.get(key) ?? key` would hand a dotted key
    // straight back to react-hook-form and reinstate the original bug with
    // no error, no type error and no failing test.
    const names = buildFieldNames([field({ key: 'email.smtp.host' })]);
    expect(fieldNameOf(names, 'email.smtp.host')).toBe('email_smtp_host');
    expect(() => fieldNameOf(names, 'app.name')).toThrow(/"app\.name"/);
  });

  it('surfaces a schema/fieldNames mismatch instead of silently mis-saving', () => {
    // The only way this fires in practice: a caller pairs a map built from
    // one schema with a different schema beside it.
    const stale = buildFieldNames([field({ key: 'old.key' })]);
    expect(() =>
      toSchemaValues([field({ key: 'new.key' })], {}, stale)
    ).toThrow(/different schema/);
  });
});

describe('toSchemaValues', () => {
  it('re-keys register names back to schema keys', () => {
    const schema = [field({ key: 'email.smtp.host' })];
    expect(toSchemaValues(schema, { email_smtp_host: 'mail.test' })).toEqual({
      'email.smtp.host': 'mail.test'
    });
  });

  it('reads a missing name as empty, which isFieldVisible treats as absent', () => {
    expect(toSchemaValues([field({ key: 'a.b' })], {})).toEqual({ 'a.b': '' });
  });
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

  it('seeds a stored empty string as empty, not the schema default', () => {
    // UpdateConfig writes configValues[key] || '' when an operator clears a
    // field, so a stored '' is a real persisted value, not "nothing stored".
    // Falling back to the default here would show a value the database does
    // not hold, and because the form wouldn't be dirty, the mismatch would
    // never get corrected by a save.
    const schema = [field({ key: 'a', default: 'fallback' })];
    expect(buildDefaults(schema, { a: '' })).toEqual({ a: '' });
  });

  it('still collapses a stored empty bool to the schema default', () => {
    // A switch has no blank state, so bool keeps the previous behavior.
    const schema = [field({ key: 'on', type: 'bool', default: 'true' })];
    expect(buildDefaults(schema, { on: '' })).toEqual({ on: 'true' });
  });

  it('reads stored values by schema key but seeds the form by register name', () => {
    // The two keyings meet here: `configValues` comes off the wire keyed by
    // the backend's own key, the result seeds react-hook-form and must
    // therefore be keyed by a name RHF will not parse as a path.
    const seeded = buildDefaults(NOTIFICATION_SCHEMA, {
      'email.smtp.host': 'mail.internal',
      'app.name': 'Acme'
    });
    expect(seeded.email_smtp_host).toBe('mail.internal');
    expect(seeded.app_name).toBe('Acme');
    // Untouched keys still fall back to their declared defaults, and the
    // secret still seeds empty.
    expect(seeded.email_smtp_port).toBe('587');
    expect(seeded.email_smtp_password).toBe('');
    // No schema key leaks into the form object — one of those handed to RHF
    // is the whole bug.
    expect(Object.keys(seeded).some(k => k.includes('.'))).toBe(false);
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

  it('accepts every duration the backend parser accepts, including bare days', async () => {
    // The contract is `utils.ParseDuration`, not `time.ParseDuration`: it is
    // everything Go takes PLUS a bare `<number>d`. Omitting the day suffix
    // here made `sessionAbsoluteTTL: 89d` (ADR-0017's documented maximum,
    // and a value auth's own `Pattern` declares legal) impossible to save
    // from /admin/modules — this resolver runs before the schema pattern, so
    // it is the binding gate.
    const schema = [field({ key: 'd', type: 'duration' })];
    for (const ok of [
      '30s',
      '1h30m',
      '500ms',
      '1.5h',
      '-5m',
      '0',
      // Bare days — the alternative this regex used to be missing.
      '30d',
      '89d',
      '0.5d',
      '1d',
      '-7d'
    ]) {
      expect(await validate(schema, { d: ok })).toEqual([]);
    }
    for (const bad of [
      '30 s',
      '15x',
      '1H',
      'h',
      'd',
      // Compound day forms stay REJECTED. `utils.ParseDuration` special-cases
      // only a bare `<number>d` — `strings.CutSuffix` then `ParseFloat` — so
      // `1d12h` is rejected by the backend too. The asymmetry with Go's own
      // parser is deliberate ("either parses exactly or is rejected"); accepting
      // it here would let the UI offer a value the server refuses.
      '1d12h',
      '12h1d'
    ]) {
      expect((await validate(schema, { d: bad })).length).toBe(1);
    }
  });

  it('treats an empty optional value as valid', async () => {
    const schema = [field({ key: 'n', type: 'int', min: 8 })];
    expect(await validate(schema, { n: '' })).toEqual([]);
  });

  it('keys the yup shape by register name and still resolves a dotted dependsOn', async () => {
    // The resolver is handed register-name-keyed values, but `dependsOn`
    // targets are schema keys — so the rule has to re-key before asking
    // isFieldVisible anything. Get that wrong and every gated field looks
    // permanently hidden, which silently disables its validation too.
    const schema = [
      field({ key: 'email.provider', type: 'enum', options: ['noop', 'smtp'] }),
      field({
        key: 'email.smtp.host',
        required: true,
        dependsOn: [{ key: 'email.provider', in: ['smtp'] }]
      })
    ];
    expect(
      await validate(schema, { email_provider: 'noop', email_smtp_host: '' })
    ).toEqual([]);
    expect(
      (await validate(schema, { email_provider: 'smtp', email_smtp_host: '' }))
        .length
    ).toBe(1);
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

  it('round-trips a stored empty string without lying: no edit means no diff', () => {
    // buildDefaults must seed the same '' that is stored, or the form would
    // be dirty against a value the operator never touched — and collectDiff
    // must then see no change and not resend it.
    const stringSchema = [field({ key: 'a', default: 'fallback' })];
    const stored = { a: '' };
    const seeded = buildDefaults(stringSchema, stored);
    expect(seeded).toEqual({ a: '' });
    expect(collectDiff(stringSchema, seeded, seeded).config).toEqual({});
  });

  it('sends a dotted key under its real schema key, not its register name', () => {
    // The end of the chain the reported bug broke: an edit to
    // `email.smtp.host` has to reach the wire under exactly that string. The
    // register name is a form-layer detail and the backend has never heard
    // of it — a payload keyed `email_smtp_host` would be silently stored as
    // a brand-new setting and the real one left untouched.
    const seeded = buildDefaults(NOTIFICATION_SCHEMA, {
      'email.smtp.host': 'old.example.com'
    });
    const edited = { ...seeded, email_smtp_host: 'new.example.com' };
    const { config, secrets } = collectDiff(
      NOTIFICATION_SCHEMA,
      edited,
      seeded
    );
    expect(config).toEqual({ 'email.smtp.host': 'new.example.com' });
    expect(secrets).toEqual({});
  });

  it('sends a dotted secret key under its real schema key too', () => {
    const seeded = buildDefaults(NOTIFICATION_SCHEMA, {});
    const edited = { ...seeded, email_smtp_password: 'hunter2' };
    const { config, secrets } = collectDiff(
      NOTIFICATION_SCHEMA,
      edited,
      seeded
    );
    expect(secrets).toEqual({ 'email.smtp.password': 'hunter2' });
    expect(config).toEqual({});
  });

  it('still excludes a hidden dotted field in both directions', () => {
    // The diff-not-replace behaviour that fixed the production secret-wipe
    // has to survive the re-keying: an edit to a field the operator can no
    // longer see is neither written back nor used to clear what is stored.
    const schema = [
      field({
        key: 'email.provider',
        type: 'enum',
        default: 'noop',
        options: ['noop', 'smtp']
      }),
      field({
        key: 'email.smtp.host',
        dependsOn: [{ key: 'email.provider', in: ['smtp'] }]
      })
    ];
    const seeded = buildDefaults(schema, {});
    expect(
      collectDiff(schema, { ...seeded, email_smtp_host: 'edited' }, seeded)
        .config
    ).toEqual({});
    // ...and reappears once the gating enum selects smtp.
    expect(
      collectDiff(
        schema,
        { ...seeded, email_provider: 'smtp', email_smtp_host: 'edited' },
        seeded
      ).config
    ).toEqual({ 'email.provider': 'smtp', 'email.smtp.host': 'edited' });
  });
});

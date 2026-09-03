import { describe, it, expect } from 'vitest';
import type { ConfigField } from 'store/api/moduleApi';
import { buildFieldNames } from './useModuleConfigForm';
import { expandSchema } from './recordList/expandSchema';
import { captureDraftFromDirty, resolveDraft } from './configDraft';

/**
 * A record list whose element keys COLLIDE after `buildFieldNames`
 * sanitisation: slug `a-b` + sub `c` and slug `a` + sub `b_c` both sanitise
 * to `email_profiles_a_b_c`, so one of them takes the base name and the
 * other a numeric suffix — decided purely by roster order. Reorder the
 * roster and the suffix moves to the other field.
 */
const schema = [
  {
    key: 'email.profiles',
    label: 'Delivery profiles',
    description: '',
    type: 'recordList',
    required: false,
    default: '',
    envVar: '',
    items: [
      { key: 'c', label: 'C', type: 'string', required: false },
      { key: 'b_c', label: 'B C', type: 'secret', required: false }
    ]
  }
] as unknown as ConfigField[];

const namesFor = (roster: string): ReadonlyMap<string, string> => {
  const values = { 'email.profiles.__items': roster };
  return buildFieldNames([...schema, ...expandSchema(schema, values)]);
};

describe('configDraft', () => {
  it('keys a captured draft by the schema key, not the react-hook-form name', () => {
    const before = namesFor('a-b,a');
    const after = namesFor('a,a-b');

    // The collision is real and the suffix genuinely moves between the two
    // rosters — this is the whole reason the draft cannot be name-keyed.
    expect(before.get('email.profiles.a-b.c')).toBe('email_profiles_a_b_c');
    expect(before.get('email.profiles.a.b_c')).toBe('email_profiles_a_b_c_2');
    expect(after.get('email.profiles.a.b_c')).toBe('email_profiles_a_b_c');
    expect(after.get('email.profiles.a-b.c')).toBe('email_profiles_a_b_c_2');

    const editedSecret = before.get('email.profiles.a.b_c')!;
    const editedHost = before.get('email.profiles.a.c')!;
    const entries = captureDraftFromDirty(
      { [editedSecret]: true, [editedHost]: true },
      { [editedSecret]: 'typed-secret', [editedHost]: 'mine' },
      before,
      new Set([editedSecret]),
      new Set()
    );
    expect(entries).toEqual([
      { key: 'email.profiles.a.b_c', value: 'typed-secret', secret: true },
      { key: 'email.profiles.a.c', value: 'mine', secret: false }
    ]);

    const { apply, dropped } = resolveDraft(entries, after, {});
    expect(dropped).toBe(0);
    // Both land on the fields the operator actually edited, under their NEW
    // names. Keying by name would have put the secret on
    // `email_profiles_a_b_c_2` — which after the reload is the neighbour
    // element's `email.profiles.a-b.c`.
    expect(apply).toEqual([
      [after.get('email.profiles.a.b_c'), 'typed-secret'],
      [after.get('email.profiles.a.c'), 'mine']
    ]);
    expect(apply.map(([name]) => name)).not.toContain(
      after.get('email.profiles.a-b.c')
    );
  });

  it('skips fields that are not dirty, belong to a pending create, or have no schema key', () => {
    const names = namesFor('a');
    const host = names.get('email.profiles.a.c')!;
    const label = names.get('email.profiles.a.__label')!;
    const entries = captureDraftFromDirty(
      { [host]: true, [label]: true, orphan: true, untouched: false },
      { [host]: 'h', [label]: 'A', orphan: 'x', untouched: 'y' },
      names,
      new Set(),
      new Set([label])
    );
    expect(entries).toEqual([
      { key: 'email.profiles.a.c', value: 'h', secret: false }
    ]);
  });

  it('drops a secret typed and then cleared — there is nothing to re-send', () => {
    const names = namesFor('a');
    const secret = names.get('email.profiles.a.b_c')!;
    expect(
      captureDraftFromDirty(
        { [secret]: true },
        { [secret]: '' },
        names,
        new Set([secret]),
        new Set()
      )
    ).toEqual([]);
  });

  it('counts an entry whose schema key is gone as dropped, by identity', () => {
    const before = namesFor('a,gone');
    const entries = captureDraftFromDirty(
      { [before.get('email.profiles.gone.c')!]: true },
      { [before.get('email.profiles.gone.c')!]: 'edit' },
      before,
      new Set(),
      new Set()
    );
    const { apply, dropped } = resolveDraft(entries, namesFor('a'), {});
    expect(apply).toEqual([]);
    expect(dropped).toBe(1);
  });

  it('re-applies a non-secret edit only while it still differs from the new baseline', () => {
    const names = namesFor('a');
    const host = names.get('email.profiles.a.c')!;
    const entries: Parameters<typeof resolveDraft>[0] = [
      { key: 'email.profiles.a.c', value: 'mine', secret: false }
    ];
    // The other writer already made the same change: not a change any more.
    expect(resolveDraft(entries, names, { [host]: 'mine' }).apply).toEqual([]);
    // An intentional clear to '' against a non-empty baseline IS a change.
    expect(
      resolveDraft(
        [{ key: 'email.profiles.a.c', value: '', secret: false }],
        names,
        { [host]: 'theirs' }
      ).apply
    ).toEqual([[host, '']]);
    // A typed secret is always a change: its baseline is never echoed.
    expect(
      resolveDraft(
        [{ key: 'email.profiles.a.b_c', value: 's', secret: true }],
        names,
        {}
      ).apply
    ).toEqual([[names.get('email.profiles.a.b_c'), 's']]);
  });
});

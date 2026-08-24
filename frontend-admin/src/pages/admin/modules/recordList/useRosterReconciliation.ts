import { useEffect, useRef } from 'react';
import type { UseFormReturn } from 'react-hook-form';
import type { ConfigFormValues } from '../useModuleConfigForm';

/**
 * Keeps react-hook-form's registry in step with a roster that moves.
 *
 * Regenerating the schema is not enough. RHF keeps registrations, dirty and
 * touched state, errors, and the values of fields that have disappeared, and
 * new `defaultValues` are not applied to an already-initialised form. So when
 * the roster changes we seed what appeared and unregister what left — and
 * touch nothing belonging to a surviving element, whose edits must be
 * preserved. Without the unregister, a removed element's values would still
 * be in `getValues()` and would ride along in the next save diff, writing
 * keys back for an element that no longer exists.
 */
export const useRosterReconciliation = (
  form: UseFormReturn<ConfigFormValues>,
  names: string[],
  defaults: ConfigFormValues,
  /**
   * Values for fields that appeared *because the operator created them* —
   * today, a new element's label. Seeded dirty, because they are an unsaved
   * edit and the save diff is what carries them to the backend.
   *
   * Seeded here rather than at the click that created the element: the
   * register name is only knowable once the roster has grown and the name map
   * has been rebuilt around it, which happens on the render after that click.
   * Reaching for the name any earlier reads a map that does not have it yet.
   */
  seeds: ConfigFormValues = {}
) => {
  // Joined rather than compared by identity: `names` is a fresh array every
  // render, so an identity dep would run this effect on every keystroke and
  // re-seed fields the operator is typing into.
  const signature = names.join(',');
  const previous = useRef<string[]>(names);

  useEffect(() => {
    const before = new Set(previous.current);
    const after = new Set(names);
    for (const name of names) {
      if (before.has(name)) continue;
      const seeded = Object.prototype.hasOwnProperty.call(seeds, name);
      form.setValue(name, seeded ? seeds[name] : (defaults[name] ?? ''), {
        shouldDirty: seeded
      });
    }
    for (const name of previous.current) {
      if (!after.has(name)) form.unregister(name);
    }
    previous.current = names;
    // `defaults` is rebuilt every render and `form` is stable; keying on the
    // roster signature is what makes this fire exactly when membership moves.
  }, [signature]);
};

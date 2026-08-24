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
  defaults: ConfigFormValues
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
      if (!before.has(name)) {
        form.setValue(name, defaults[name] ?? '', { shouldDirty: false });
      }
    }
    for (const name of previous.current) {
      if (!after.has(name)) form.unregister(name);
    }
    previous.current = names;
    // `defaults` is rebuilt every render and `form` is stable; keying on the
    // roster signature is what makes this fire exactly when membership moves.
  }, [signature]);
};

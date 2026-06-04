import { useEffect, useState } from 'react';

// useDebounce returns a debounced copy of `value` that only updates after
// `value` has stopped changing for `delayMs`. Used to throttle
// server-side search queries while the operator is still typing.
export function useDebounce<T>(value: T, delayMs = 300): T {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const handle = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(handle);
  }, [value, delayMs]);

  return debounced;
}

export default useDebounce;

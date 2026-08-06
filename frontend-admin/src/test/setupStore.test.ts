import { describe, expect, it, vi } from 'vitest';

import { setupStore } from './render';

// Regression pin for a suite-wide flake, not a feature test.
//
// RTK's autoBatchEnhancer defaults to `type: 'raf'`, which captures
// `window.requestAnimationFrame` when the store is built and then — inside
// the frame callback — calls the *unqualified global* `cancelAnimationFrame`
// (redux-toolkit.modern.mjs, createRafWithFallbackTimer). happy-dom
// implements requestAnimationFrame on top of setImmediate, and vitest's
// happy-dom environment teardown does `keys.forEach(key => delete
// global[key])` on every window key. A frame callback that a test file
// queues just before it finishes therefore runs after its own globals are
// gone, the bare identifier no longer resolves, and the ReferenceError lands
// outside any test — which vitest counts as an unhandled error and exits 1
// even with every test green. Which files got hit varied per run.
//
// The test store opts into `type: 'tick'` (queueMicrotask) instead, so a
// notification can never outlive the environment that scheduled it. This
// asserts the store never reaches for an animation frame at all.
describe('setupStore', () => {
  it('does not schedule autobatched notifications on an animation frame', () => {
    // Must be installed before the store is built: RTK reads
    // window.requestAnimationFrame once, at enhancer-construction time.
    const raf = vi.spyOn(window, 'requestAnimationFrame');

    const store = setupStore();
    // `RTK_autoBatch` is the meta flag RTK Query stamps on the internal
    // subscription actions that make the enhancer queue a notification.
    store.dispatch({ type: 'test/autobatched', meta: { RTK_autoBatch: true } });

    expect(raf).not.toHaveBeenCalled();

    raf.mockRestore();
  });
});

// Step-up coordination between the RTK Query base query and the global
// StepUpModal. The base query calls requestStepUp() when it hits a 401
// with code="step_up_required"; the modal listens for open events via
// subscribe(), drives the user through /v1/auth/operator/mfa/verify, and calls
// completeStepUp(true) on success or completeStepUp(false) on cancel.
//
// Deliberately module-level state (not Redux) so the base query's async
// flow can await a plain Promise — converting through a slice would mean
// subscribing to the store from inside a fetch closure, which is awkward
// and adds nothing. Concurrent 401s share the same pending promise so a
// single verification replays every paused request.

type Listener = (open: boolean) => void;

const listeners = new Set<Listener>();
let pending: Promise<boolean> | null = null;
let resolver: ((verified: boolean) => void) | null = null;

/**
 * Open the step-up modal (or join the existing one) and return a promise
 * that resolves with true when the user completes MFA verification, or
 * false if they cancel.
 */
export function requestStepUp(): Promise<boolean> {
  if (!pending) {
    pending = new Promise<boolean>((res) => { resolver = res; });
    listeners.forEach((l) => l(true));
  }
  return pending;
}

/**
 * Called by StepUpModal after the user verifies (true) or cancels (false).
 * Resolves every waiting caller with the same outcome.
 */
export function completeStepUp(verified: boolean) {
  const r = resolver;
  resolver = null;
  pending = null;
  listeners.forEach((l) => l(false));
  r?.(verified);
}

/**
 * Subscribe to modal open/close events. Returns an unsubscribe function.
 */
export function subscribeStepUp(listener: Listener): () => void {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

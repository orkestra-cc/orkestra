import { describe, it, expect, vi, beforeEach } from 'vitest';
import { requestStepUp, completeStepUp, subscribeStepUp } from './stepUp';

// Each test must start with no outstanding pending promise. There's no
// public reset so we drain any leftover state by cancelling.
beforeEach(() => {
  completeStepUp(false);
});

describe('stepUp coordination', () => {
  it('opens a pending promise and notifies subscribers on request', () => {
    const events: boolean[] = [];
    const unsub = subscribeStepUp((open) => events.push(open));

    const p = requestStepUp();
    expect(events).toEqual([true]);

    completeStepUp(true);
    unsub();
    return expect(p).resolves.toBe(true);
  });

  it('returns false when the user cancels', async () => {
    const p = requestStepUp();
    completeStepUp(false);
    await expect(p).resolves.toBe(false);
  });

  it('joins concurrent waiters to the same outcome', async () => {
    const a = requestStepUp();
    const b = requestStepUp();
    const c = requestStepUp();

    completeStepUp(true);

    const [ra, rb, rc] = await Promise.all([a, b, c]);
    expect([ra, rb, rc]).toEqual([true, true, true]);
  });

  it('notifies close event to subscribers on completion', () => {
    const listener = vi.fn();
    const unsub = subscribeStepUp(listener);

    requestStepUp();
    completeStepUp(true);

    // Two events: open (true) then close (false).
    expect(listener.mock.calls).toEqual([[true], [false]]);
    unsub();
  });

  it('unsubscribe stops receiving events', () => {
    const listener = vi.fn();
    const unsub = subscribeStepUp(listener);
    unsub();

    requestStepUp();
    completeStepUp(false);
    expect(listener).not.toHaveBeenCalled();
  });

  it('permits a fresh cycle after one completes', async () => {
    const first = requestStepUp();
    completeStepUp(true);
    await expect(first).resolves.toBe(true);

    const second = requestStepUp();
    completeStepUp(false);
    await expect(second).resolves.toBe(false);
  });
});

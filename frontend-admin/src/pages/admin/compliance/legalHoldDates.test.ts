import { describe, it, expect } from 'vitest';
import {
  expiryDayToInstant,
  instantToExpiryDay,
  instantToReviewDay,
  reviewDayToInstant
} from './legalHoldDates';

// Instants are built from explicit local components, so the assertions hold in
// whatever zone the runner uses.
describe('legalHoldDates', () => {
  it('review starts the local day, expiry covers it to the next local midnight', () => {
    expect(reviewDayToInstant('2030-03-09')).toBe(
      new Date(2030, 2, 9).toISOString()
    );
    expect(expiryDayToInstant('2030-03-09')).toBe(
      new Date(2030, 2, 10).toISOString()
    );
  });
  it('crosses month and year boundaries', () => {
    expect(expiryDayToInstant('2030-12-31')).toBe(
      new Date(2031, 0, 1).toISOString()
    );
    expect(expiryDayToInstant('2030-01-31')).toBe(
      new Date(2030, 1, 1).toISOString()
    );
  });
  it('round-trips with the same local-zone rule', () => {
    expect(instantToExpiryDay(expiryDayToInstant('2030-12-31'))).toBe(
      '2030-12-31'
    );
    expect(instantToReviewDay(reviewDayToInstant('2030-12-31'))).toBe(
      '2030-12-31'
    );
  });
  it('treats missing or invalid instants as empty', () => {
    expect(instantToExpiryDay(undefined)).toBe('');
    expect(instantToReviewDay('nope')).toBe('');
  });
});

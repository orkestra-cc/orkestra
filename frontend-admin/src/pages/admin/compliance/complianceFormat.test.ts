import { describe, it, expect } from 'vitest';
import { erasureStatusColor } from './complianceFormat';

describe('erasureStatusColor', () => {
  it('colors an incomplete erasure as danger', () => {
    expect(erasureStatusColor('incomplete')).toBe('danger');
    expect(erasureStatusColor('closed_partial')).toBe('secondary');
    expect(erasureStatusColor('pending')).toBe('warning');
  });
});

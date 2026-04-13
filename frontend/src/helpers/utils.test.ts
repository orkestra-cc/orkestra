import { describe, it, expect } from 'vitest';
import {
  isIterableArray,
  numberFormatter,
  hexToRgb,
  capitalize,
  camelize,
  dashed,
  slugifyText,
  getSize,
  getPaginationArray,
  chunk,
  getPercentage,
  addId,
} from './utils';

describe('isIterableArray', () => {
  it('returns true for non-empty arrays', () => {
    expect(isIterableArray([1, 2, 3])).toBe(true);
  });

  it('returns false for empty arrays', () => {
    expect(isIterableArray([])).toBe(false);
  });

  it('returns false for non-arrays', () => {
    expect(isIterableArray('string')).toBe(false);
    expect(isIterableArray(null)).toBe(false);
    expect(isIterableArray(undefined)).toBe(false);
  });
});

describe('numberFormatter', () => {
  it('formats billions', () => {
    expect(numberFormatter(1_500_000_000)).toBe('1.50B');
  });

  it('formats millions', () => {
    expect(numberFormatter(2_500_000)).toBe('2.50M');
  });

  it('formats thousands', () => {
    expect(numberFormatter(1_500)).toBe('1.50K');
  });

  it('formats small numbers', () => {
    expect(numberFormatter(42)).toBe('42.00');
  });

  it('respects custom fixed decimals', () => {
    expect(numberFormatter(1_500, 0)).toBe('2K');
    expect(numberFormatter(1_500, 1)).toBe('1.5K');
  });
});

describe('hexToRgb', () => {
  it('converts 6-digit hex', () => {
    expect(hexToRgb('#2c7be5')).toEqual([44, 123, 229]);
  });

  it('converts 3-digit shorthand hex', () => {
    expect(hexToRgb('#fff')).toEqual([255, 255, 255]);
  });

  it('works without # prefix', () => {
    expect(hexToRgb('000000')).toEqual([0, 0, 0]);
  });

  it('returns null for invalid hex', () => {
    expect(hexToRgb('xyz')).toBeNull();
  });
});

describe('string utilities', () => {
  it('capitalize uppercases first letter and replaces dashes with spaces', () => {
    expect(capitalize('hello')).toBe('Hello');
    expect(capitalize('hello-world')).toBe('Hello world');
  });

  it('camelize converts to camelCase', () => {
    expect(camelize('hello world')).toBe('helloWorld');
    expect(camelize('Hello World')).toBe('helloWorld');
  });

  it('dashed converts to lowercase dashed', () => {
    expect(dashed('Hello World')).toBe('hello-world');
  });

  it('slugifyText creates URL-safe slugs', () => {
    expect(slugifyText('Hello World')).toBe('hello-world');
    expect(slugifyText('  Leading and trailing  ')).toBe('leading-and-trailing');
  });
});

describe('getSize', () => {
  it('formats bytes', () => {
    expect(getSize(500)).toBe('500 Byte');
  });

  it('formats kilobytes', () => {
    expect(getSize(2048)).toBe('2.00 KB');
  });

  it('formats megabytes', () => {
    expect(getSize(5 * 1024 * 1024)).toBe('5.00 MB');
  });
});

describe('getPaginationArray', () => {
  it('returns correct number of pages', () => {
    expect(getPaginationArray(50, 10)).toEqual([1, 2, 3, 4, 5]);
  });

  it('rounds up for partial pages', () => {
    expect(getPaginationArray(11, 5)).toEqual([1, 2, 3]);
  });
});

describe('chunk', () => {
  it('splits array into chunks', () => {
    expect(chunk([1, 2, 3, 4, 5], 2)).toEqual([[1, 2], [3, 4], [5]]);
  });

  it('returns empty array for zero chunk size', () => {
    expect(chunk([1, 2, 3], 0)).toEqual([]);
  });
});

describe('getPercentage', () => {
  it('calculates percentage', () => {
    expect(getPercentage(200, 50)).toBe(100);
    expect(getPercentage(100, 10)).toBe(10);
  });
});

describe('addId', () => {
  it('adds 1-based id field to items', () => {
    const items = [{ name: 'a' }, { name: 'b' }];
    const result = addId(items);
    expect(result[0]).toEqual({ id: 1, name: 'a' });
    expect(result[1]).toEqual({ id: 2, name: 'b' });
  });
});

import { describe, expect, it } from 'vitest';
import { hasDescription } from './taskDescription';

describe('hasDescription (#109)', () => {
  it('is false for empty or missing descriptions', () => {
    expect(hasDescription('')).toBe(false);
    expect(hasDescription(null)).toBe(false);
    expect(hasDescription(undefined)).toBe(false);
  });

  it('is false for whitespace and non-breaking spaces left by a cleared editor', () => {
    expect(hasDescription('   \n\n ')).toBe(false);
    expect(hasDescription('&nbsp;')).toBe(false);
    expect(hasDescription('\n&nbsp;\n \n')).toBe(false);
  });

  it('is true when the description has text', () => {
    expect(hasDescription('Some notes')).toBe(true);
    expect(hasDescription('# Heading')).toBe(true);
    expect(hasDescription('- item')).toBe(true);
  });
});

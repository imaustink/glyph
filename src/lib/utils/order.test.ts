import { describe, expect, it } from 'vitest';
import { nextOrder, orderBetween } from './order';

describe('nextOrder', () => {
  it('returns a number', () => {
    expect(typeof nextOrder()).toBe('number');
  });

  it('is strictly monotonic across successive same-tick calls', () => {
    const values = Array.from({ length: 100 }, () => nextOrder());
    for (let i = 1; i < values.length; i++) {
      expect(values[i]).toBeGreaterThan(values[i - 1]);
    }
  });

  it('is strictly monotonic across time', async () => {
    const a = nextOrder();
    await new Promise((r) => setTimeout(r, 2));
    const b = nextOrder();
    expect(b).toBeGreaterThan(a);
  });

  it('produces unique values for rapid calls', () => {
    const values = new Set(Array.from({ length: 100 }, () => nextOrder()));
    expect(values.size).toBe(100);
  });

  it('returns values larger than Date.now()', () => {
    // nextOrder() >= Date.now() * 1000, so always > Date.now()
    const order = nextOrder();
    expect(order).toBeGreaterThan(Date.now());
  });
});

describe('orderBetween', () => {
  it('returns midpoint between two values', () => {
    expect(orderBetween(100, 200)).toBe(150);
  });

  it('handles null before (starts from 0)', () => {
    expect(orderBetween(null, 200)).toBe(100);
  });

  it('handles null after (uses before + 2_000_000)', () => {
    const result = orderBetween(1000, null);
    expect(result).toBe(Math.floor((1000 + 1000 + 2_000_000) / 2));
  });

  it('handles both null (0 to 2_000_000)', () => {
    expect(orderBetween(null, null)).toBe(1_000_000);
  });

  it('returns an integer', () => {
    const result = orderBetween(1, 4);
    expect(Number.isInteger(result)).toBe(true);
  });
});

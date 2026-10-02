import { describe, it, expect } from 'vitest';
import { clampPopoverToViewport } from './popoverPosition';

const viewport = { width: 1000, height: 800 };

describe('clampPopoverToViewport', () => {
  it('opens below and left-aligned when there is room', () => {
    const trigger = { top: 100, bottom: 120, left: 200, right: 260 };
    const res = clampPopoverToViewport(trigger, { width: 220, height: 300 }, viewport);
    expect(res.placement).toBe('below');
    expect(res.top).toBe(126); // bottom + gap(6)
    expect(res.left).toBe(200); // aligned to trigger left
  });

  it('aligns the right edge when a left-aligned popover would overflow the right edge', () => {
    const trigger = { top: 100, bottom: 120, left: 900, right: 960 };
    const res = clampPopoverToViewport(trigger, { width: 220, height: 300 }, viewport);
    // Right-aligned: trigger.right - width = 960 - 220 = 740
    expect(res.left).toBe(740);
    expect(res.left + 220).toBeLessThanOrEqual(viewport.width);
  });

  it('never positions past the left edge (keeps the margin)', () => {
    const trigger = { top: 100, bottom: 120, left: -50, right: 10 };
    const res = clampPopoverToViewport(trigger, { width: 220, height: 300 }, viewport);
    expect(res.left).toBe(8); // clamped to margin
  });

  it('flips above when it would run off the bottom and there is more room above', () => {
    // Trigger near the bottom: little room below, lots above.
    const trigger = { top: 740, bottom: 760, left: 200, right: 260 };
    const res = clampPopoverToViewport(trigger, { width: 220, height: 300 }, viewport);
    expect(res.placement).toBe('above');
    // top = trigger.top - gap - min(height, maxHeight) = 740 - 6 - 300 = 434
    expect(res.top).toBe(434);
    expect(res.top).toBeGreaterThanOrEqual(8);
  });

  it('caps maxHeight to the space below so tall content scrolls instead of overflowing', () => {
    const trigger = { top: 100, bottom: 120, left: 200, right: 260 };
    const res = clampPopoverToViewport(trigger, { width: 220, height: 2000 }, viewport);
    expect(res.placement).toBe('below');
    // maxHeight = viewport.height - top - margin = 800 - 126 - 8 = 666
    expect(res.maxHeight).toBe(666);
    expect(res.top + res.maxHeight).toBeLessThanOrEqual(viewport.height);
  });

  it('caps maxHeight to the space above when flipped and content is taller than that space', () => {
    const trigger = { top: 740, bottom: 760, left: 200, right: 260 };
    const res = clampPopoverToViewport(trigger, { width: 220, height: 2000 }, viewport);
    expect(res.placement).toBe('above');
    // spaceAbove = 740 - 6 - 8 = 726
    expect(res.maxHeight).toBe(726);
    expect(res.top).toBe(8); // clamped to margin
  });

  it('stays below (not flipped) when neither side fits but below has more room', () => {
    // Trigger just above vertical center: below has slightly more space than above.
    const trigger = { top: 380, bottom: 400, left: 200, right: 260 };
    const res = clampPopoverToViewport(trigger, { width: 220, height: 1000 }, viewport);
    expect(res.placement).toBe('below');
  });
});

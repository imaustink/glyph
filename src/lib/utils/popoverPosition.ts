/**
 * Shared viewport-clamping math for floating UI (dropdowns / popovers).
 *
 * Positions a popover relative to its trigger while keeping it fully on
 * screen: aligned to the trigger's left edge, flipped to the right edge (and
 * finally clamped) when it would overflow horizontally, and opened below the
 * trigger — or flipped above — with a `maxHeight` so tall content scrolls
 * instead of spilling off the top or bottom of the viewport.
 */

export interface TriggerRect {
  top: number;
  bottom: number;
  left: number;
  right: number;
}

export interface Size {
  width: number;
  height: number;
}

export interface Viewport {
  width: number;
  height: number;
}

export interface PopoverPlacement {
  top: number;
  left: number;
  /** Max height the popover may occupy before it should scroll internally. */
  maxHeight: number;
  placement: 'below' | 'above';
}

export interface ClampOptions {
  /** Minimum gap to keep between the popover and the viewport edges. */
  margin?: number;
  /** Gap between the trigger and the popover along the vertical axis. */
  gap?: number;
}

export function clampPopoverToViewport(
  trigger: TriggerRect,
  size: Size,
  viewport: Viewport,
  opts: ClampOptions = {}
): PopoverPlacement {
  const margin = opts.margin ?? 8;
  const gap = opts.gap ?? 6;

  // ── Horizontal ────────────────────────────────────────────────────────────
  // Prefer aligning the popover's left edge to the trigger's left edge. If that
  // would overflow the right edge, align its right edge to the trigger's right
  // edge instead, then clamp within the viewport so it never leaves either side.
  let left = trigger.left;
  if (left + size.width > viewport.width - margin) {
    left = trigger.right - size.width;
  }
  const maxLeft = Math.max(margin, viewport.width - size.width - margin);
  left = Math.min(Math.max(margin, left), maxLeft);

  // ── Vertical ────────────────────────────────────────────────────────────────
  // Prefer opening below the trigger; flip above when it would run off the
  // bottom and there is more room above. Either way cap the height to the
  // available space so overflowing content scrolls rather than disappearing.
  const spaceBelow = viewport.height - trigger.bottom - gap - margin;
  const spaceAbove = trigger.top - gap - margin;

  let placement: 'below' | 'above';
  let top: number;
  let maxHeight: number;

  if (size.height <= spaceBelow || spaceBelow >= spaceAbove) {
    placement = 'below';
    top = trigger.bottom + gap;
    maxHeight = Math.max(0, viewport.height - top - margin);
  } else {
    placement = 'above';
    maxHeight = Math.max(0, spaceAbove);
    top = Math.max(margin, trigger.top - gap - Math.min(size.height, maxHeight));
  }

  return { top, left, maxHeight, placement };
}

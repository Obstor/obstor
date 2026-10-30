// Fixed popover position in viewport

export type Rect = { top: number; bottom: number; left: number; right: number };

const GAP = 6;
const MARGIN = 8;

const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(v, hi));

// Below trigger unless only above fits
export function placeMenu(
  trigger: Rect,
  panel: { width: number; height: number },
  viewport: { width: number; height: number },
  align: "start" | "end",
): { left: number; top: number } {
  const x = align === "start" ? trigger.left : trigger.right - panel.width;
  const below = trigger.bottom + GAP;
  const above = trigger.top - GAP - panel.height;
  const fitsBelow = below + panel.height <= viewport.height - MARGIN;
  const fitsAbove = above >= MARGIN;
  const y = !fitsBelow && fitsAbove ? above : below;
  return {
    left: clamp(x, MARGIN, viewport.width - panel.width - MARGIN),
    top: clamp(y, MARGIN, viewport.height - panel.height - MARGIN),
  };
}

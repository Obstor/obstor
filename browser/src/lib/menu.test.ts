// Run via node --experimental-strip-types src/lib/menu.test.ts
import assert from "node:assert/strict";
import { placeMenu } from "./menu.ts";

const desktop = { width: 1440, height: 1000 };
const phone = { width: 390, height: 844 };
const panel = { width: 176, height: 120 };
const at = (left: number, top: number, w = 36, h = 36) => ({
  left,
  right: left + w,
  top,
  bottom: top + h,
});

// Align matching edges, 6px below trigger
assert.deepEqual(placeMenu(at(400, 100), panel, desktop, "start"), { left: 400, top: 142 });
assert.deepEqual(placeMenu(at(400, 100), panel, desktop, "end"), { left: 260, top: 142 });

// Right edge stays 8px inside viewport
assert.deepEqual(placeMenu(at(340, 100), panel, phone, "start"), { left: 206, top: 142 });
assert.deepEqual(placeMenu(at(340, 100, 44, 44), panel, phone, "end"), { left: 206, top: 150 });

// Clamp left edge when end alignment overflows
assert.deepEqual(placeMenu(at(0, 100), panel, phone, "end"), { left: 8, top: 142 });

// Flip above if it only fits there
assert.deepEqual(placeMenu(at(300, 760), panel, phone, "end"), { left: 160, top: 634 });

// Exact fit below keeps position
assert.deepEqual(placeMenu(at(300, 674), panel, phone, "end"), { left: 160, top: 716 });

// Neither side fits, clamp below placement to bottom margin
const tall = { width: 176, height: 600 };
assert.deepEqual(placeMenu(at(300, 400), tall, phone, "end"), { left: 160, top: 236 });

// Oversized panel pins to top left margin
assert.deepEqual(placeMenu(at(300, 400), { width: 500, height: 900 }, phone, "start"), {
  left: 8,
  top: 8,
});

console.log("menu tests passed");

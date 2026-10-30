// Run via node --experimental-strip-types src/lib/pins.test.ts
import assert from "node:assert/strict";
import { parsePins, shortcutBuckets, togglePin } from "./pins.ts";

assert.deepEqual(parsePins(null), []);
assert.deepEqual(parsePins("not json"), []);
assert.deepEqual(parsePins('{"a":1}'), []);
assert.deepEqual(parsePins('["a", 2, "b"]'), ["a", "b"]);

const live = new Set(["a", "b"]);
assert.deepEqual(togglePin(["a", "gone"], "b", live), ["a", "b"]);
assert.deepEqual(togglePin(["a", "b"], "a", live), ["b"]);

const buckets = [
  "2026-01-01",
  "2026-06-01",
  "2026-03-01",
  "2026-02-01",
  "2026-05-01",
  "2026-04-01",
].map((d, i) => ({ name: `b${i}`, creationDate: `${d}T00:00:00Z` }));
const newest = ["b1", "b4", "b5", "b2", "b3"];
assert.deepEqual(shortcutBuckets(buckets, []), { pinned: false, names: newest });
assert.deepEqual(shortcutBuckets(buckets, ["gone"]), { pinned: false, names: newest });
assert.deepEqual(shortcutBuckets(buckets, ["b0", "gone"]), { pinned: true, names: ["b0"] });

console.log("pins tests passed");

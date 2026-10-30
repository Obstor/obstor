// Run via node --experimental-strip-types src/lib/attention.test.ts
import assert from "node:assert/strict";
import {
  type AttentionItem,
  BANNER_PREFIX,
  bannerState,
  bellLabel,
  markSeen,
  parseSeen,
  pruneSeen,
  SEEN_PREFIX,
  type Severity,
  seenKey,
  unseenItems,
} from "./attention.ts";

const item = (id: string, severity: Severity): AttentionItem => ({
  id,
  kind: id.split(":")[0],
  severity,
  title: `Title of ${id}`,
  detail: "",
  href: "",
});

assert.equal(SEEN_PREFIX, "obstor.seen.");
assert.equal(BANNER_PREFIX, "obstor.banner.");
assert.equal(seenKey("alice"), "obstor.seen.alice");

// Bad stored values become empty or cleaned lists
assert.deepEqual(parseSeen(null), []);
assert.deepEqual(parseSeen(""), []);
assert.deepEqual(parseSeen("{}"), []);
assert.deepEqual(parseSeen('"a"'), []);
assert.deepEqual(parseSeen("["), []);
assert.deepEqual(parseSeen('[1,"a","a"]'), ["a"]);
assert.deepEqual(parseSeen('["b","a","b",null]'), ["b", "a"]);

const drive = item("drive-offline:/mnt/d1", "critical");
const near = item("quota-near:alpha", "warning");
const stale = item("usage-stale", "info");
const items = [drive, near, stale];

assert.deepEqual(pruneSeen(["gone", "usage-stale", "quota-near:alpha"], items), [
  "usage-stale",
  "quota-near:alpha",
]);
assert.deepEqual(pruneSeen(["gone"], []), []);
assert.deepEqual(pruneSeen([], items), []);

assert.deepEqual(markSeen(["a", "b"], ["b", "c", "c"]), ["a", "b", "c"]);
assert.deepEqual(markSeen([], []), []);
assert.deepEqual(markSeen(["a"], []), ["a"]);

assert.deepEqual(
  unseenItems(items, ["quota-near:alpha"]).map((i) => i.id),
  ["drive-offline:/mnt/d1", "usage-stale"],
);
assert.deepEqual(unseenItems(items, []), items);
assert.deepEqual(
  unseenItems(items, ["drive-offline:/mnt/d1", "quota-near:alpha", "usage-stale"]),
  [],
);
assert.deepEqual(unseenItems([], ["x"]), []);

assert.equal(bellLabel(0), "Notifications");
assert.equal(bellLabel(1), "Notifications, 1 unread");
assert.equal(bellLabel(3), "Notifications, 3 unread");

// No critical item, no banner
assert.equal(bannerState([]), null);
assert.equal(bannerState([near, stale]), null);

// One critical item: no "more" link
assert.deepEqual(bannerState([near, drive, stale]), {
  first: drive,
  more: 0,
  key: "obstor.banner.drive-offline:/mnt/d1",
});

// Same critical set keeps key, first item follows server order
const node = item("node-offline:node2:9000", "critical");
const a = bannerState([node, near, drive]);
const b = bannerState([drive, stale, node]);
assert.ok(a && b);
assert.equal(a.key, b.key);
assert.equal(a.key, "obstor.banner.drive-offline:/mnt/d1,node-offline:node2:9000");
assert.equal(a.first.id, "node-offline:node2:9000");
assert.equal(b.first.id, "drive-offline:/mnt/d1");
assert.equal(a.more, 1);

// New critical ID or full quota changes key
const full = item("quota-full:alpha", "critical");
assert.notEqual(bannerState([node, drive, full])?.key, a.key);
assert.notEqual(bannerState([drive, full])?.key, bannerState([drive, near])?.key);

// Keep input order
const input = [node, drive];
bannerState(input);
assert.deepEqual(
  input.map((i) => i.id),
  ["node-offline:node2:9000", "drive-offline:/mnt/d1"],
);

console.log("attention tests passed");

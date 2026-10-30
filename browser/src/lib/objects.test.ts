// Run via node --experimental-strip-types src/lib/objects.test.ts
import assert from "node:assert/strict";
import {
  entryName,
  folderKey,
  hostPath,
  locationColumn,
  locationLabel,
  objectsHref,
  pageWindow,
  paginate,
  parentPrefix,
  prefixCrumbs,
  s3ErrorMessage,
  sortBuckets,
  sortEntries,
  uploadProgress,
} from "./objects.ts";

const e = (name: string, size = 0, modified = "2026-01-01T00:00:00Z") => ({ name, size, modified });
const rows = [
  e("b.txt", 5, "2026-03-01T00:00:00Z"),
  e("z/"),
  e("a.txt", 9, "2026-01-01T00:00:00Z"),
  e("m/"),
];
const names = (r: { name: string }[]) => r.map((x) => x.name);
assert.deepEqual(names(sortEntries(rows, "name", true)), ["m/", "z/", "a.txt", "b.txt"]);
assert.deepEqual(names(sortEntries(rows, "name", false)), ["z/", "m/", "b.txt", "a.txt"]);
assert.deepEqual(names(sortEntries(rows, "size", false)), ["m/", "z/", "a.txt", "b.txt"]);
assert.deepEqual(names(sortEntries(rows, "modified", true)), ["m/", "z/", "a.txt", "b.txt"]);
assert.deepEqual(names(sortEntries(rows, "modified", false)), ["m/", "z/", "b.txt", "a.txt"]);

const many = Array.from({ length: 120 }, (_, i) => i);
assert.deepEqual(paginate(many, 3, 50), { rows: many.slice(100), page: 3, pages: 3 });
assert.equal(paginate(many, 9, 50).page, 3);
assert.equal(paginate(many, 0, 50).page, 1);
assert.deepEqual(paginate([], 1, 50), { rows: [], page: 1, pages: 1 });
assert.deepEqual(pageWindow(1, 1), [1]);
assert.deepEqual(pageWindow(2, 3), [1, 2, 3]);
assert.deepEqual(pageWindow(1, 10), [1, 2, 3, 4, 5]);
assert.deepEqual(pageWindow(5, 10), [3, 4, 5, 6, 7]);
assert.deepEqual(pageWindow(10, 10), [6, 7, 8, 9, 10]);

assert.equal(parentPrefix("a/b/"), "a/");
assert.equal(parentPrefix("a/"), "");
assert.equal(parentPrefix(""), "");
assert.deepEqual(prefixCrumbs("a/b/"), [
  { name: "a", prefix: "a/" },
  { name: "b", prefix: "a/b/" },
]);
assert.equal(entryName("a/b/c.txt", "a/b/"), "c.txt");
assert.equal(entryName("a/b/", "a/"), "b");
assert.equal(objectsHref("my bucket"), "/buckets/my%20bucket");
assert.equal(objectsHref("b", "x y/#?%/"), "/buckets/b?prefix=x%20y%2F%23%3F%25%2F");
assert.equal(folderKey("a/", " /new/ "), "a/new/");
assert.equal(folderKey("", "///"), null);
assert.equal(
  s3ErrorMessage(
    "<Error><Code>XObstorAdminBucketQuotaExceeded</Code><Message>Bucket quota exceeded</Message></Error>",
    400,
  ),
  "Bucket quota exceeded",
);
assert.equal(s3ErrorMessage("", 502), "HTTP 502");

assert.equal(hostPath("http://127.0.0.1:9200/alpha"), "127.0.0.1:9200/alpha");
assert.equal(hostPath("https://s3.example.com/my%20bucket"), "s3.example.com/my%20bucket");
assert.equal(hostPath("localhost:9000/alpha"), "localhost:9000/alpha");

assert.equal(locationLabel("http://node1:9000/mnt/disk3"), "node1:9000/disk3");
assert.equal(locationLabel("https://node2:9000/mnt/disk4/"), "node2:9000/disk4");
assert.equal(locationLabel("http://node1:9000"), "node1:9000");
assert.equal(locationLabel("/private/tmp/obstor-console/ec1"), "ec1");
assert.equal(locationLabel("/"), "/");

// One FS drive repeats label, hide column
const fs = locationColumn(["a.txt", "b.txt"], {
  "a.txt": ["/data/fs"],
  "b.txt": ["/data/fs"],
});
assert.equal(fs.show, false);
assert.deepEqual(fs.cells["a.txt"], { label: "fs", title: "/data/fs" });

// Different erasure sets show column
const sets = locationColumn(["a.txt", "b.txt", "docs/"], {
  "a.txt": ["/ec/d1", "/ec/d2"],
  "b.txt": ["/ec/d3", "/ec/d4"],
});
assert.equal(sets.show, true);
assert.deepEqual(sets.cells["a.txt"], { label: "d1 +1", title: "/ec/d1, /ec/d2" });
assert.deepEqual(sets.cells["b.txt"], { label: "d3 +1", title: "/ec/d3, /ec/d4" });
// Folders have no location cell
assert.equal(sets.cells["docs/"], undefined);
// No endpoints means no cell
assert.equal(locationColumn(["x"], { x: [] }).cells.x, undefined);
assert.deepEqual(locationColumn([], {}), { show: false, cells: {} });
// Object.prototype names stay plain data
assert.equal(Object.hasOwn(locationColumn(["constructor"], {}).cells, "constructor"), false);
const proto = locationColumn(
  ["__proto__", "b"],
  Object.fromEntries([
    ["__proto__", ["/ec/d1"]],
    ["b", ["/ec/d5"]],
  ]),
);
assert.equal(proto.show, true);
assert.deepEqual(Object.getOwnPropertyDescriptor(proto.cells, "__proto__")?.value, {
  label: "d1",
  title: "/ec/d1",
});

const buckets = [
  { name: "beta", createdAt: "2026-02-01T00:00:00Z" },
  { name: "alpha", createdAt: "2026-03-01T00:00:00Z" },
  { name: "gamma", createdAt: "2026-02-01T00:00:00Z" },
];
assert.deepEqual(names(sortBuckets(buckets, "name", true)), ["alpha", "beta", "gamma"]);
assert.deepEqual(names(sortBuckets(buckets, "name", false)), ["gamma", "beta", "alpha"]);
assert.deepEqual(names(sortBuckets(buckets, "created", true)), ["beta", "gamma", "alpha"]);
assert.deepEqual(names(sortBuckets(buckets, "created", false)), ["alpha", "gamma", "beta"]);
assert.equal(buckets[0].name, "beta");

assert.deepEqual(uploadProgress(["done", "done", "uploading"]), {
  active: true,
  current: 3,
  total: 3,
});
assert.deepEqual(uploadProgress(["done", "failed", "queued", "queued"]), {
  active: true,
  current: 3,
  total: 4,
});
assert.deepEqual(uploadProgress(["done", "done"]), { active: false, current: 2, total: 2 });
assert.deepEqual(uploadProgress([]), { active: false, current: 0, total: 0 });

// Code unit name order, independent of locale
const nativeCompare = String.prototype.localeCompare;
String.prototype.localeCompare = function (this: string, that: string) {
  return nativeCompare.call(this, that, "da");
};
assert.deepEqual(
  sortBuckets(
    [
      { name: "zz", createdAt: "" },
      { name: "aa-x", createdAt: "" },
    ],
    "name",
    true,
  ).map((b) => b.name),
  ["aa-x", "zz"],
);
assert.deepEqual(
  names(
    sortEntries(
      [
        { name: "zz", size: 0, modified: "" },
        { name: "aa-x", size: 0, modified: "" },
      ],
      "name",
      true,
    ),
  ),
  ["aa-x", "zz"],
);
String.prototype.localeCompare = nativeCompare;

{
  const u = (name: string, size: number | null, objects: number | null) => ({
    name,
    createdAt: "2026-01-01T00:00:00Z",
    size,
    objects,
  });
  const usage = [u("a", 10, 1), u("b", null, null), u("c", 30, 5), u("d", 10, 2)];
  const before = names(usage);
  assert.deepEqual(names(sortBuckets(usage, "size", true)), ["a", "d", "c", "b"]);
  assert.deepEqual(names(sortBuckets(usage, "size", false)), ["c", "a", "d", "b"]);
  assert.deepEqual(names(sortBuckets(usage, "objects", false)), ["c", "d", "a", "b"]);
  assert.deepEqual(names(sortBuckets(usage, "objects", true)), ["a", "d", "c", "b"]);
  assert.deepEqual(names(usage), before);
  const unsized = [u("z", null, null), u("y", null, null), u("x", 1, 1)];
  assert.deepEqual(names(sortBuckets(unsized, "size", false)), ["x", "y", "z"]);
  assert.deepEqual(names(sortBuckets(unsized, "objects", true)), ["x", "y", "z"]);
  assert.deepEqual(names(unsized), ["z", "y", "x"]);
}

console.log("objects tests passed");

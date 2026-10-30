// Run via node --experimental-strip-types src/lib/chart.test.ts
import assert from "node:assert/strict";
import {
  byteTicks,
  capacityBreakdown,
  collectingFrom,
  countTicks,
  gaugeArc,
  getTimeRange,
  health,
  hourlyAverages,
  niceTicks,
  PLOT_H,
  plotScale,
  pointLabel,
  rawUsed,
  regionShares,
  sparkPath,
  sparkTimes,
  sparkValues,
  TIME_RANGES,
  timeLabels,
  traceY,
  usageNote,
} from "./chart.ts";

assert.deepEqual(niceTicks(200), [0, 50, 100, 150, 200]);
assert.deepEqual(niceTicks(7), [0, 2, 4, 6, 8]);
assert.deepEqual(niceTicks(1), [0, 0.5, 1]);
assert.deepEqual(niceTicks(0), [0]);
assert.deepEqual(niceTicks(Number.NaN), [0]);

const gb = 1024 ** 3;
assert.deepEqual(
  byteTicks(180 * gb).map((t) => t.label),
  ["0 B", "50 GB", "100 GB", "150 GB", "200 GB"],
);
assert.equal(byteTicks(180 * gb).at(-1)?.value, 200 * gb);
assert.deepEqual(
  byteTicks(1024 ** 2).map((t) => t.label),
  ["0 B", "0.5 MB", "1 MB"],
);
assert.deepEqual(
  byteTicks(13).map((t) => t.label),
  ["0 B", "5 B", "10 B", "15 B"],
);
assert.deepEqual(
  byteTicks(0).map((t) => t.label),
  ["0 B"],
);
assert.deepEqual(
  countTicks(1200).map((t) => t.label),
  ["0", "500", "1,000", "1,500"],
);

assert.deepEqual(plotScale([[0, 0], []], 10), { mode: "empty", domain: 10 });
assert.deepEqual(
  plotScale(
    [
      [0, 5],
      [0, 0],
    ],
    10,
  ),
  { mode: "trace", domain: 10 },
);
assert.deepEqual(plotScale([[1, 2, 30]], 10), { mode: "data", domain: 30 });
// Bytes axis uses real peak, no floor
assert.deepEqual(plotScale([[0, 13, 0]]), { mode: "trace", domain: 13 });
assert.deepEqual(plotScale([[200, 300, 400]]), { mode: "data", domain: 400 });
assert.deepEqual(plotScale([[0, 0]]), { mode: "empty", domain: 0 });

// Trace stubs use axis scale with 2 unit minimum
assert.equal(traceY(13, 15), PLOT_H - (13 / 15) * PLOT_H);
assert.equal(traceY(15, 15), 0);
assert.equal(traceY(1, 1024 ** 3), PLOT_H - 2);

assert.deepEqual(hourlyAverages([1, 2, 3], 24), [1, 2, 3]);
assert.equal(hourlyAverages(Array(168).fill(6), 168).length, 28);
assert.equal(hourlyAverages(Array(720).fill(1), 720).length, 30);
assert.deepEqual(hourlyAverages([], 24), []);

assert.equal(getTimeRange("1w").hours, 168);
assert.equal(getTimeRange(["1m"]).value, "24h");
assert.equal(getTimeRange(undefined), TIME_RANGES[0]);
assert.equal(getTimeRange("bogus").value, "24h");

const now = Date.UTC(2026, 9, 3, 12);
const day = timeLabels(24, now);
assert.equal(day.length, 7);
assert.equal(day.at(-1)?.label, "now");
for (const t of day.slice(0, -1)) assert.match(t.label, /^\d\d:00$/);
for (const t of timeLabels(720, now).slice(0, -1)) assert.match(t.label, /^[A-Z][a-z]{2} \d{1,2}$/);
assert.match(pointLabel(23, 24, 24, now), /^\d\d:00$/);
assert.match(pointLabel(0, 28, 168, now), /^[A-Z][a-z]{2} \d{1,2} \d\d:00$/);
const localNow = new Date(2026, 9, 3, 12).getTime();
assert.equal(pointLabel(0, 28, 168, localNow), "Sep 26 13:00");
assert.equal(pointLabel(27, 28, 168, localNow), "Oct 3 07:00");
assert.equal(pointLabel(0, 30, 720, localNow), "Sep 3 13:00");
assert.equal(pointLabel(23, 24, 24, localNow), "12:00");

assert.deepEqual(health([]), {
  nodesOnline: 0,
  nodesTotal: 0,
  nodesOffline: 0,
  drivesOnline: 0,
  drivesTotal: 0,
  drivesOffline: 0,
});
assert.deepEqual(
  health([
    { online: true, drivesOnline: 3, drivesTotal: 4 },
    { online: false, drivesOnline: 0, drivesTotal: 0 },
  ]),
  {
    nodesOnline: 1,
    nodesTotal: 2,
    nodesOffline: 1,
    drivesOnline: 3,
    drivesTotal: 4,
    drivesOffline: 1,
  },
);

// Compact y axis labels for phones
assert.deepEqual(
  byteTicks(180 * gb).map((t) => t.short),
  ["0", "50G", "100G", "150G", "200G"],
);
assert.deepEqual(
  byteTicks(1024 ** 2).map((t) => t.short),
  ["0", "0.5M", "1M"],
);
assert.deepEqual(
  byteTicks(600).map((t) => t.short),
  ["0", "200B", "400B", "600B"],
);
assert.deepEqual(
  countTicks(1200).map((t) => t.short),
  ["0", "500", "1K", "1.5K"],
);
assert.deepEqual(
  countTicks(2_500_000).map((t) => t.short),
  ["0", "1M", "2M", "3M"],
);

// No breakdown without total, segments sum to raw total
const mb = 1024 ** 2;
const sumRows = (c: { obstorRaw: number; other: number; free: number }) =>
  c.obstorRaw + c.other + c.free;
assert.equal(capacityBreakdown({ total: 0, free: 0, used: 0, parityRatio: 0 }), null);
assert.equal(capacityBreakdown({ total: -1, free: 0, used: 0, parityRatio: 1 }), null);
assert.equal(capacityBreakdown({ total: Number.NaN, free: 0, used: 0, parityRatio: 1 }), null);
assert.equal(
  capacityBreakdown({ total: Number.POSITIVE_INFINITY, free: 0, used: 0, parityRatio: 1 }),
  null,
);
const fsCap = capacityBreakdown({ total: 926 * gb, free: 220 * gb, used: 2 * mb, parityRatio: 1 });
assert.ok(fsCap);
assert.equal(fsCap.pct.toFixed(3), "0.762");
assert.equal(fsCap.obstorRaw, 2 * mb);
assert.equal(fsCap.other, 706 * gb - 2 * mb);
assert.equal(fsCap.showOther, true);
assert.equal(fsCap.free, 220 * gb);
assert.equal(sumRows(fsCap), fsCap.total);
const ecCap = capacityBreakdown({
  total: 1000 * gb,
  free: 400 * gb,
  used: 100 * gb,
  parityRatio: 2,
});
assert.ok(ecCap);
assert.equal(ecCap.pct, 0.6);
assert.equal(ecCap.obstorRaw, 200 * gb);
assert.equal(ecCap.other, 400 * gb);
assert.equal(sumRows(ecCap), ecCap.total);
const clamped = capacityBreakdown({
  total: 100 * gb,
  free: 40 * gb,
  used: 50 * gb,
  parityRatio: 2,
});
assert.ok(clamped);
assert.equal(clamped.obstorRaw, 60 * gb);
assert.equal(clamped.other, 0);
assert.equal(clamped.showOther, false);
assert.equal(sumRows(clamped), clamped.total);
const tiny = capacityBreakdown({
  total: 1000 * gb,
  free: 500 * gb,
  used: 498 * gb,
  parityRatio: 1,
});
assert.ok(tiny);
assert.equal(tiny.other, 2 * gb);
assert.equal(tiny.showOther, false);
const overFree = capacityBreakdown({ total: 100 * gb, free: 150 * gb, used: 0, parityRatio: 1 });
assert.ok(overFree);
assert.equal(overFree.pct, 0);
assert.equal(overFree.free, 100 * gb);
assert.equal(overFree.other, 0);
const noRatio = capacityBreakdown({
  total: 100 * gb,
  free: 50 * gb,
  used: 10 * gb,
  parityRatio: 0,
});
assert.ok(noRatio);
assert.equal(noRatio.obstorRaw, 10 * gb);

// 270 degree arc from 135 to 405
const at = (deg: number) => [
  76 + 68 * Math.cos((deg * Math.PI) / 180),
  76 + 68 * Math.sin((deg * Math.PI) / 180),
];
const near = (a: number, b: number) => Math.abs(a - b) < 0.01;
const full = gaugeArc(0, 1, 152, 16).match(/^M (\S+) (\S+) A 68 68 0 1 1 (\S+) (\S+)$/);
assert.ok(full, gaugeArc(0, 1, 152, 16));
const [sx, sy] = at(135);
const [ex, ey] = at(405);
assert.ok(near(Number(full[1]), sx) && near(Number(full[2]), sy), "arc starts at 135 degrees");
assert.ok(near(Number(full[3]), ex) && near(Number(full[4]), ey), "arc ends at 405 degrees");
assert.match(gaugeArc(0, 0.5, 152, 16), / A 68 68 0 0 1 /);
assert.equal(gaugeArc(0.5, 0.5, 152, 16), "");
assert.equal(gaugeArc(0.7, 0.2, 152, 16), "");
assert.equal(gaugeArc(Number.NaN, 1, 152, 16), "");

// Count points whose bucket starts before since
const halfPast = Date.UTC(2026, 9, 3, 12, 30);
assert.equal(collectingFrom("", 24, 24, halfPast), 0);
assert.equal(collectingFrom("not a date", 24, 24, halfPast), 0);
assert.equal(
  collectingFrom(new Date(halfPast - 3 * 3_600_000).toISOString(), 24, 24, halfPast),
  21,
);
assert.equal(collectingFrom("2026-10-03T09:00:00Z", 24, 24, halfPast), 20);
assert.equal(
  collectingFrom(new Date(halfPast - 30 * 86_400_000).toISOString(), 24, 24, halfPast),
  0,
);
assert.equal(collectingFrom("2026-10-02T06:30:00Z", 168, 28, halfPast), 23);
assert.equal(collectingFrom("2026-10-03T13:00:00Z", 24, 24, halfPast), 24);
assert.equal(collectingFrom("2026-10-03T09:00:00Z", 24, 0, halfPast), 0);

// Backend caption first, then unscanned, none after scan
assert.equal(
  usageNote(false, "2026-10-09T06:00:00Z"),
  "Usage scanning is not available in backend mode.",
);
assert.equal(usageNote(true, ""), "Waiting for the first usage scan.");
assert.equal(usageNote(undefined, ""), "Waiting for the first usage scan.");
assert.equal(usageNote(true, "2026-10-09T06:00:00Z"), undefined);
assert.equal(usageNote(true, undefined), undefined);

assert.equal(sparkPath([], 120, 40), null);
assert.equal(sparkPath([5], 120, 40), null);
assert.equal(sparkPath([0, 0, 0], 120, 40)?.line, "M 0 20 L 60 20 L 120 20");
assert.equal(sparkPath([7, 7], 120, 40)?.line, "M 0 20 L 120 20");
const rise = sparkPath([0, 10], 120, 40);
assert.equal(rise?.line, "M 0 38 L 120 2");
assert.equal(rise?.area, "M 0 38 L 120 2 L 120 40 L 0 40 Z");
assert.ok(rise?.area.endsWith(" L 120 40 L 0 40 Z"));
const dirty = sparkPath([Number.NaN, 10, -4], 120, 40);
assert.ok(dirty && !dirty.line.includes("NaN") && !dirty.area.includes("NaN"));
assert.deepEqual(dirty, sparkPath([0, 10, 0], 120, 40));
assert.equal(sparkPath([Number.POSITIVE_INFINITY, 10], 120, 40)?.line, "M 0 38 L 120 2");
assert.deepEqual(
  sparkPath([1, 2, 3], 100, 40)
    ?.line.split(/[ML]/)
    .filter((s) => s.trim())
    .map((s) => Number(s.trim().split(" ")[0])),
  [0, 50, 100],
);
assert.equal(sparkPath([0, 1, 3], 120, 40)?.line, "M 0 38 L 60 26 L 120 2");

assert.deepEqual(sparkValues([1, 2, 3], 9), [1, 2, 9]);
assert.deepEqual(sparkValues([1, 2, 3], null), [1, 2, 3]);
assert.deepEqual(sparkValues([], 9), []);
assert.deepEqual(sparkValues([1, 2], Number.NaN), [1, 2]);
const input = [1, 2, 3];
sparkValues(input, 9);
assert.deepEqual(input, [1, 2, 3]);
assert.notEqual(sparkValues(input, null), input);

assert.deepEqual(sparkTimes("2026-10-09T10:00:00Z", 3600, 3), [
  "2026-10-09T10:00:00.000Z",
  "2026-10-09T11:00:00.000Z",
  "2026-10-09T12:00:00.000Z",
]);
assert.deepEqual(sparkTimes("2026-10-01T00:00:00Z", 86400, 2), [
  "2026-10-01T00:00:00.000Z",
  "2026-10-02T00:00:00.000Z",
]);
assert.deepEqual(sparkTimes("", 3600, 3), []);
assert.deepEqual(sparkTimes("x", 3600, 2), []);
assert.deepEqual(sparkTimes("2026-10-09T00:00:00Z", 0, 2), []);

assert.deepEqual(rawUsed([100, 100, 120], [60, 40, 50]), [40, 60, 70]);
assert.deepEqual(rawUsed([10], [20]), [0]);
assert.equal(rawUsed(null, [1]), null);
assert.equal(rawUsed([1, 2], null), null);
assert.equal(rawUsed([1, 2], [1]), null);

assert.deepEqual(
  regionShares([
    { region: "us-west", total: 200, free: 50 },
    { region: "eu-central", total: 100, free: 100 },
    { region: "ap-south", total: 0, free: 0 },
    { region: "us-east", total: 100, free: 25 },
    { region: "af-south", total: 400, free: 100 },
  ]),
  [
    { region: "af-south", pct: 75 },
    { region: "ap-south", pct: 0 },
    { region: "eu-central", pct: 0 },
    { region: "us-east", pct: 75 },
  ],
);
assert.deepEqual(regionShares([{ region: "x", total: 100, free: 150 }]), [{ region: "x", pct: 0 }]);
assert.deepEqual(regionShares([]), []);

console.log("chart tests passed");

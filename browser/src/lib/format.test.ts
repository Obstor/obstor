// Run via node --experimental-strip-types src/lib/format.test.ts
import assert from "node:assert/strict";
import * as formatModule from "./format.ts";
import {
  commitLabel,
  formatClock,
  formatDay,
  formatSparkTime,
  formatStamp,
  formatUptime,
  humanCount,
  humanSize,
  isStale,
  modeLabel,
  plural,
  versionLabel,
} from "./format.ts";

assert.equal(humanSize(0), "0 B");
assert.equal(humanSize(-5), "0 B");
assert.equal(humanSize(Number.NaN), "0 B");
assert.equal(humanSize(512), "512 B");
assert.equal(humanSize(1023), "1023 B");
assert.equal(humanSize(342 * 1024), "342 KB");
assert.equal(humanSize(1024 ** 3), "1.0 GB");
assert.equal(humanSize(12.1 * 1024 ** 4), "12.1 TB");
// Rounding to next unit avoids 1024 or 100.0
assert.equal(humanSize(1023.96 * 1024), "1.0 MB");
assert.equal(humanSize(99.96 * 1024 ** 2), "100 MB");

assert.equal(humanCount(999), "999");
assert.equal(humanCount(1000), "1K");
assert.equal(humanCount(11_100), "11.1K");
assert.equal(humanCount(2_000_000), "2M");
assert.equal(humanCount(999_950), "1M");
assert.equal(humanCount(999_949_000), "999.9M");
assert.equal(humanCount(999_950_000), "1B");

assert.equal(plural(1, "region"), "1 region");
assert.equal(plural(2, "policy", "policies"), "2 policies");
assert.equal(plural(1500, "item"), "1,500 items");

// Explicit zones keep tests independent of machine
const morning = "2026-09-25T06:00:00Z";
const night = "2026-09-25T02:30:00Z";
assert.equal(formatDay(morning, "UTC"), "Sep 25, 2026");
assert.equal(formatDay(night, "UTC"), "Sep 25, 2026");
assert.equal(formatDay(night, "America/New_York"), "Sep 24, 2026");
assert.equal(formatDay(""), "");
assert.equal(formatClock(morning, "UTC"), "06:00");
assert.equal(formatClock(morning, "America/New_York"), "02:00");
assert.equal(formatClock("2026-09-25T00:05:00Z", "UTC"), "00:05");
assert.equal(formatClock("not a date", "UTC"), "");
assert.equal(formatStamp(morning, "UTC"), "Sep 25, 2026, 06:00");
assert.equal(formatStamp(night, "America/New_York"), "Sep 24, 2026, 22:30");
assert.equal(formatStamp(""), "");
// Daily points keep UTC date in every zone
assert.equal(
  formatSparkTime("2026-10-09T00:00:00.000Z", 86400, "America/Los_Angeles"),
  "Oct 9, 2026",
);
assert.equal(formatSparkTime(night, 3600, "America/New_York"), "Sep 24, 2026, 22:30");
assert.equal(formatSparkTime("", 86400), "");

const t = Date.parse(morning);
assert.equal(isStale(morning, t + 600_000, 600_000), false);
assert.equal(isStale(morning, t + 600_001, 600_000), true);
assert.equal(isStale("", t, 600_000), false);
assert.equal(isStale("not a date", t, 600_000), false);

assert.equal(versionLabel("1.2.3"), "v1.2.3");
assert.equal(versionLabel("v1.2.3"), "v1.2.3");
assert.equal(versionLabel("RELEASE.2026-10-09T12-00-00Z"), "RELEASE.2026-10-09T12-00-00Z");
assert.equal(versionLabel("DEVELOPMENT.GOGET"), "Dev build");
assert.equal(versionLabel("2026-10-09T12:00:00Z"), "Dev build");
assert.equal(versionLabel(""), "Dev build");
assert.equal(versionLabel("dev", true), "Production");
assert.equal(versionLabel("main", true), "Production");
assert.equal(versionLabel("DEVELOPMENT.GOGET", true), "Production");
assert.equal(versionLabel("", true), "Production");
assert.equal(versionLabel("1.2.3", true), "v1.2.3");
assert.equal(versionLabel("dev", false), "Dev build");

assert.equal(modeLabel("fs"), "FS");
assert.equal(modeLabel("erasure"), "Erasure");
assert.equal(modeLabel("backend"), "Backend");
assert.equal(modeLabel(""), "");

// LocalTime formats object dates on client
assert.equal("formatDate" in formatModule, false);

// Whole uptime minutes, bad or future input stays empty
const up = Date.parse("2026-10-09T12:00:00Z");
assert.equal(formatUptime("", up), "");
assert.equal(formatUptime("nope", up), "");
assert.equal(formatUptime("2026-10-09T12:00:01Z", up), "");
assert.equal(formatUptime("2026-10-09T11:00:00Z", Number.NaN), "");
assert.equal(formatUptime("2026-10-09T11:00:00Z", Number.POSITIVE_INFINITY), "");
assert.equal(formatUptime("2026-10-09T12:00:00Z", up), "under a minute");
assert.equal(formatUptime("2026-10-09T11:59:30Z", up), "under a minute");
assert.equal(formatUptime("2026-10-09T11:59:00Z", up), "1m");
assert.equal(formatUptime("2026-10-09T11:48:00Z", up), "12m");
assert.equal(formatUptime("2026-10-09T11:00:00Z", up), "1h 0m");
assert.equal(formatUptime("2026-10-09T09:45:00Z", up), "2h 15m");
assert.equal(formatUptime("2026-10-08T12:00:30Z", up), "23h 59m");
assert.equal(formatUptime("2026-10-08T12:00:00Z", up), "1d 0h");
assert.equal(formatUptime("2026-10-06T08:00:00Z", up), "3d 4h");

// Release shows hex hash, dev shows no commit
const sha = "0123456789abcdef0123456789abcdef01234567";
assert.equal(sha.length, 40);
assert.equal(commitLabel(sha), "0123456789ab");
assert.equal(commitLabel("abc1234"), "abc1234");
assert.equal(commitLabel("abc123"), "");
assert.equal(commitLabel(`${sha}8`), "");
assert.equal(commitLabel("ABCDEF1"), "");
assert.equal(commitLabel("DEVELOPMENT.GOGET"), "");
assert.equal(commitLabel(""), "");

console.log("format tests passed");

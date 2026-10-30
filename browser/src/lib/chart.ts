// Chart math, time ranges and node health

export const TIME_RANGES = [
  { value: "24h", hours: 24, label: "Last 24 hours", short: "24h" },
  { value: "1w", hours: 168, label: "Last week", short: "7d" },
  { value: "1m", hours: 720, label: "Last 30 days", short: "30d" },
] as const;

export type TimeRange = (typeof TIME_RANGES)[number];

export function getTimeRange(value: string | string[] | undefined): TimeRange {
  return TIME_RANGES.find((r) => r.value === value) ?? TIME_RANGES[0];
}

export function rangeDescription(hours: number): string {
  return hours <= 24 ? `${hours} hours` : `${hours / 24} days`;
}

// Empty, too few samples for line, or usable plot
export type PlotMode = "empty" | "trace" | "data";

// Requests have a small floor, bytes use real peak
export const OPS_FLOOR = 10;

// Shared viewBox stretched by chart frame
export const PLOT_W = 600;
export const PLOT_H = 200;

export function plotScale(series: number[][], floor = 0): { mode: PlotMode; domain: number } {
  const all = series.flat();
  const max = Math.max(...all, 0);
  if (max <= 0) return { mode: "empty", domain: floor };
  const samples = all.filter((v) => v > 0).length;
  return { mode: samples < 3 ? "trace" : "data", domain: Math.max(max, floor) };
}

// Trace stub on y axis scale, at least 2 units tall
export function traceY(v: number, top: number): number {
  return Math.min(PLOT_H - (v / top) * PLOT_H, PLOT_H - 2);
}

export function bucketHours(hours: number): number {
  return hours <= 24 ? 1 : hours <= 168 ? 6 : 24;
}

// Average longer ranges into 6h or 24h buckets
export function hourlyAverages(series: number[], hours: number): number[] {
  const step = bucketHours(hours);
  const out: number[] = [];
  for (let i = 0; i < series.length; i += step) {
    const slice = series.slice(i, i + step);
    out.push(slice.reduce((a, b) => a + b, 0) / slice.length);
  }
  return out;
}

// Round 1/2/5 ticks from zero through max
export function niceTicks(max: number, count = 4): number[] {
  if (!(max > 0)) return [0];
  const raw = max / count;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? 10 * mag;
  const ticks: number[] = [];
  for (let i = 0; ; i++) {
    const v = Number((i * step).toPrecision(12));
    ticks.push(v);
    if (v >= max) return ticks;
  }
}

export interface Tick {
  value: number;
  label: string;
  short: string; // Compact phone axis label
}

const BYTE_UNITS = ["B", "KB", "MB", "GB", "TB", "PB"];
const BYTE_SHORT = ["B", "K", "M", "G", "T", "P"];

// Round ticks in display units
export function byteTicks(max: number, count = 4): Tick[] {
  let p = 0;
  while (p < BYTE_UNITS.length - 1 && max >= 1024 ** (p + 1)) p++;
  const div = 1024 ** p;
  return niceTicks(max / div, count).map((t) => ({
    value: t * div,
    label: t === 0 ? "0 B" : `${t} ${BYTE_UNITS[p]}`,
    short: t === 0 ? "0" : `${t}${BYTE_SHORT[p]}`,
  }));
}

// Round ticks need one decimal at most
function compactCount(v: number): string {
  if (v >= 1e9) return `${Number((v / 1e9).toFixed(1))}B`;
  if (v >= 1e6) return `${Number((v / 1e6).toFixed(1))}M`;
  if (v >= 1e3) return `${Number((v / 1e3).toFixed(1))}K`;
  return String(v);
}

export function countTicks(max: number, count = 4): Tick[] {
  return niceTicks(max, count).map((v) => ({
    value: v,
    label: v.toLocaleString("en-US"),
    short: compactCount(v),
  }));
}

// Even x axis labels ending now
export function timeLabels(
  hours: number,
  now: number,
  count = 7,
): { key: number; label: string }[] {
  return Array.from({ length: count }, (_, i) => {
    const ms = now - (1 - i / (count - 1)) * hours * 3_600_000;
    const d = new Date(ms);
    const label =
      i === count - 1
        ? "now"
        : hours <= 24
          ? `${String(d.getHours()).padStart(2, "0")}:00`
          : d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
    return { key: ms, label };
  });
}

// Start time of point i of n
export function pointLabel(i: number, n: number, hours: number, now: number): string {
  const step = bucketHours(hours);
  const d = new Date(now - ((n - 1 - i) * step + step - 1) * 3_600_000);
  const hh = `${String(d.getHours()).padStart(2, "0")}:00`;
  return hours <= 24
    ? hh
    : `${d.toLocaleDateString("en-US", { month: "short", day: "numeric" })} ${hh}`;
}

// Sparkline fills box height with 2 unit pad
export function sparkPath(
  values: number[],
  w: number,
  h: number,
): { line: string; area: string } | null {
  const n = values.length;
  if (n < 2) return null;
  const vs = values.map((v) => (Number.isFinite(v) && v > 0 ? v : 0));
  const min = Math.min(...vs);
  const max = Math.max(...vs);
  const round = (v: number) => Number(v.toFixed(2));
  const points = vs.map((v, i) => {
    const x = round((i * w) / (n - 1));
    const y = round(max === min ? h / 2 : h - 2 - ((v - min) / (max - min)) * (h - 4));
    return `${x} ${y}`;
  });
  const line = `M ${points.join(" L ")}`;
  return { line, area: `${line} L ${w} ${h} L 0 ${h} Z` };
}

// History ending at card's live value
export function sparkValues(values: number[], live: number | null): number[] {
  const out = values.slice();
  if (live !== null && Number.isFinite(live) && out.length > 0) out[out.length - 1] = live;
  return out;
}

// Total less free per point, null if series do not match
export function rawUsed(
  total: number[] | null | undefined,
  free: number[] | null | undefined,
): number[] | null {
  if (!total || !free || total.length !== free.length) return null;
  return total.map((t, i) => Math.max(0, t - free[i]));
}

// Drive usage percent for up to four regions by name, zero if total unknown
export function regionShares(
  regions: { region: string; total: number; free: number }[],
): { region: string; pct: number }[] {
  return [...regions]
    .sort((a, b) => (a.region < b.region ? -1 : a.region > b.region ? 1 : 0))
    .slice(0, 4)
    .map((r) => ({
      region: r.region,
      pct: r.total > 0 ? Math.min(100, Math.max(0, ((r.total - r.free) / r.total) * 100)) : 0,
    }));
}

// ISO UTC times spaced step seconds from since
export function sparkTimes(since: string, step: number, n: number): string[] {
  const start = Date.parse(since);
  if (Number.isNaN(start) || !(step > 0)) return [];
  return Array.from({ length: n }, (_, i) => new Date(start + i * step * 1000).toISOString());
}

export interface Health {
  nodesOnline: number;
  nodesTotal: number;
  nodesOffline: number;
  drivesOnline: number;
  drivesTotal: number;
  drivesOffline: number;
}

export function health(
  nodes: { online: boolean; drivesOnline: number; drivesTotal: number }[],
): Health {
  let nodesOnline = 0;
  let drivesOnline = 0;
  let drivesTotal = 0;
  for (const n of nodes) {
    if (n.online) nodesOnline++;
    drivesOnline += n.drivesOnline;
    drivesTotal += n.drivesTotal;
  }
  return {
    nodesOnline,
    nodesTotal: nodes.length,
    nodesOffline: nodes.length - nodesOnline,
    drivesOnline,
    drivesTotal,
    drivesOffline: drivesTotal - drivesOnline,
  };
}

const HOUR = 3_600_000;

// Leading points before first traffic report
export function collectingFrom(since: string, hours: number, points: number, now: number): number {
  const t = since ? Date.parse(since) : Number.NaN;
  if (!Number.isFinite(t) || points <= 0) return 0;
  const step = bucketHours(hours);
  const hourStart = Math.floor(now / HOUR) * HOUR;
  let n = 0;
  while (n < points && hourStart - ((points - 1 - n) * step + step - 1) * HOUR < t) n++;
  return n;
}

export interface Capacity {
  pct: number; // Raw usage fraction, (total - free) / total
  obstorRaw: number; // Object bytes including parity
  other: number; // Used space outside object data
  showOther: boolean;
  free: number;
  total: number;
}

// Three raw space segments that sum to total
export function capacityBreakdown(s: {
  total: number;
  free: number;
  used: number;
  parityRatio: number;
}): Capacity | null {
  const total = s.total;
  if (!Number.isFinite(total) || total <= 0) return null;
  const free = Number.isFinite(s.free) ? Math.min(Math.max(s.free, 0), total) : 0;
  const inUse = total - free;
  const ratio = Number.isFinite(s.parityRatio) && s.parityRatio > 0 ? s.parityRatio : 1;
  const used = Number.isFinite(s.used) ? Math.max(s.used, 0) : 0;
  const obstorRaw = Math.min(used * ratio, inUse);
  const other = Math.max(0, inUse - obstorRaw);
  return { pct: inUse / total, obstorRaw, other, showOther: other > 0.005 * total, free, total };
}

const GAUGE_START = 135; // Lower left, clockwise degrees from 3 o'clock
const GAUGE_SWEEP = 270;

// 270 degree arc between fractional endpoints, gap at bottom
export function gaugeArc(from: number, to: number, size: number, stroke: number): string {
  const a = Math.min(Math.max(from, 0), 1);
  const b = Math.min(Math.max(to, 0), 1);
  if (!(b > a)) return "";
  const r = (size - stroke) / 2;
  const c = size / 2;
  const point = (f: number) => {
    const rad = ((GAUGE_START + GAUGE_SWEEP * f) * Math.PI) / 180;
    const xy = [c + r * Math.cos(rad), c + r * Math.sin(rad)];
    return xy.map((v) => Number(v.toFixed(2))).join(" ");
  };
  const large = (b - a) * GAUGE_SWEEP > 180 ? 1 : 0;
  return `M ${point(a)} A ${r} ${r} 0 ${large} 1 ${point(b)}`;
}

// Caption until first scan, backend mode has its own caption
export function usageNote(
  usage: boolean | undefined,
  usageAsOf: string | undefined,
): string | undefined {
  if (usage === false) return "Usage scanning is not available in backend mode.";
  if (usageAsOf === "") return "Waiting for the first usage scan.";
  return undefined;
}

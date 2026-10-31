// Pure formatters, safe in server and client components

const SIZE_UNITS = ["B", "KB", "MB", "GB", "TB", "PB"];
const COUNT_UNITS = ["K", "M", "B"];

// No decimals for bytes or values at least 100
function sizeText(v: number, unit: number): string {
  if (unit === 0) return v.toFixed(0);
  const one = v.toFixed(1);
  return Number(one) >= 100 ? v.toFixed(0) : one;
}

export function humanSize(bytes: number): string {
  if (!(bytes > 0)) return "0 B";
  let i = 0;
  let v = bytes;
  while (i < SIZE_UNITS.length - 1 && Number(sizeText(v, i)) >= 1024) {
    v /= 1024;
    i++;
  }
  return `${sizeText(v, i)} ${SIZE_UNITS[i]}`;
}

export function humanCount(n: number): string {
  if (!(n >= 1000)) return String(n);
  let i = 0;
  let v = n / 1000;
  while (i < COUNT_UNITS.length - 1 && Number(v.toFixed(1)) >= 1000) {
    v /= 1000;
    i++;
  }
  return `${v.toFixed(1).replace(/\.0$/, "")}${COUNT_UNITS[i]}`;
}

function parse(iso: string): Date | null {
  const d = new Date(iso);
  return iso && !Number.isNaN(d.getTime()) ? d : null;
}

// Date label, UTC matches server and client
export function formatDay(iso: string, timeZone?: string): string {
  const d = parse(iso);
  return d
    ? d.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric", timeZone })
    : "";
}

// 24 hour time
export function formatClock(iso: string, timeZone?: string): string {
  const d = parse(iso);
  return d
    ? d.toLocaleTimeString("en-US", {
        hour: "2-digit",
        minute: "2-digit",
        hourCycle: "h23",
        timeZone,
      })
    : "";
}

// Date and 24 hour time
export function formatStamp(iso: string, timeZone?: string): string {
  const d = parse(iso);
  return d
    ? d.toLocaleString("en-US", {
        month: "short",
        day: "numeric",
        year: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        hourCycle: "h23",
        timeZone,
      })
    : "";
}

// Daily points keep UTC date
export function formatSparkTime(iso: string, step: number, timeZone?: string): string {
  return step >= 86400 ? formatDay(iso, "UTC") : formatStamp(iso, timeZone);
}

export function isStale(iso: string, now: number, maxMs: number): boolean {
  const d = parse(iso);
  return d !== null && now - d.getTime() > maxMs;
}

const SEMVER = /^v?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.+-]+)?)$/;

export function versionLabel(tag: string, production = false): string {
  const m = SEMVER.exec(tag);
  if (m) return `v${m[1]}`;
  return tag.startsWith("RELEASE.") ? tag : production ? "Production" : "Dev build";
}

const MODES: Record<string, string> = { fs: "FS", erasure: "Erasure", backend: "Backend" };

export function modeLabel(mode: string): string {
  return MODES[mode] ?? "";
}

export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n.toLocaleString("en-US")} ${n === 1 ? one : many}`;
}

// Uptime in days, hours and minutes
export function formatUptime(startedAt: string, now: number): string {
  const d = parse(startedAt);
  if (!d || !Number.isFinite(now) || now < d.getTime()) return "";
  const minutes = Math.floor((now - d.getTime()) / 60_000);
  if (minutes < 1) return "under a minute";
  const days = Math.floor(minutes / 1440);
  const hours = Math.floor((minutes % 1440) / 60);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes % 60}m`;
  return `${minutes}m`;
}

const COMMIT = /^[0-9a-f]{7,40}$/;

// First 12 hex digits of git hash
export function commitLabel(commit: string): string {
  return COMMIT.test(commit) ? commit.slice(0, 12) : "";
}

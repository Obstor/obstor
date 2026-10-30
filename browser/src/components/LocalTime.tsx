"use client";

import { useEffect, useState, useSyncExternalStore } from "react";
import { relativeTime } from "@/lib/activity";
import { formatClock, formatDay, formatStamp, formatUptime, isStale } from "@/lib/format";

const STALE_MS = 600_000;
const UPTIME_TICK_MS = 60_000;
const subscribe = () => () => {};

// False until mounted so hydration matches
function useMounted(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => true,
    () => false,
  );
}

function timeText(iso: string, format: "clock" | "stamp", mounted: boolean): string {
  if (!mounted) return format === "stamp" ? formatDay(iso, "UTC") : "";
  return format === "stamp" ? formatStamp(iso) : formatClock(iso);
}

export function LocalTime({ iso, format }: { iso: string; format: "clock" | "stamp" }) {
  const mounted = useMounted();
  if (!iso) return null;
  return (
    <time dateTime={iso} title={iso}>
      {timeText(iso, format, mounted)}
    </time>
  );
}

// Scan time over 10 minutes old, hidden on server
export function AsOf({ iso }: { iso: string }) {
  const mounted = useMounted();
  if (!mounted || !isStale(iso, Date.now(), STALE_MS)) return null;
  return (
    <time dateTime={iso} title={iso}>
      as of {formatClock(iso)}
    </time>
  );
}

// Uptime after mount, refreshes each minute
export function Uptime({ iso }: { iso: string }) {
  const mounted = useMounted();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), UPTIME_TICK_MS);
    return () => window.clearInterval(id);
  }, []);
  if (!iso) return null;
  return (
    <time dateTime={iso} title={iso}>
      {mounted ? formatUptime(iso, now) : ""}
    </time>
  );
}

// 30 days local date time, UTC default
export function RelativeTime({ iso }: { iso: string }) {
  const mounted = useMounted();
  const text = mounted
    ? relativeTime(iso, Date.now()) || formatStamp(iso)
    : formatStamp(iso, "UTC");
  if (!text) return <span className="text-stone-600">N/A</span>;
  return (
    <time dateTime={iso} title={iso}>
      {text}
    </time>
  );
}

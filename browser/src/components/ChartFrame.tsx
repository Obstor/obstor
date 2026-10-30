"use client";

import { type CSSProperties, type ReactNode, useEffect, useState } from "react";
import { PLOT_H, PLOT_W, pointLabel, timeLabels } from "@/lib/chart";

// HTML frame around SVG, local times after mount
export function ChartFrame({
  ticks,
  columns,
  names,
  hours,
  now,
  height,
  empty,
  label,
  shade,
  halves,
  children,
}: {
  ticks: { pct: number; label: string; short: string }[]; // Percent from top
  columns: { x: number; w: number; values: string[] }[]; // x and w use plot_w units
  names: string[]; // One name per column value
  hours: number;
  now: number;
  height: number;
  empty?: string;
  label: string;
  shade: number; // Leading columns before collection started
  halves?: { up: string; down: string }; // Labels above and below zero
  children: ReactNode;
}) {
  const [mounted, setMounted] = useState(false);
  const [active, setActive] = useState<number | null>(null);
  useEffect(() => setMounted(true), []);

  const n = columns.length;
  const pctX = (v: number) => `${(v / PLOT_W) * 100}%`;
  const time = (i: number) => (mounted ? pointLabel(i, n, hours, now) : "");
  const tip = (i: number) =>
    [time(i), ...names.map((name, k) => `${name} ${columns[i]?.values[k] ?? ""}`)]
      .filter(Boolean)
      .join(" | ");
  const shaded = columns[Math.min(shade, n) - 1];
  const cur = active === null ? undefined : columns[active];

  // Invisible widest label sizes the gutter
  const widest = (key: "label" | "short") =>
    ticks.reduce((w, t) => (t[key].length > w.length ? t[key] : w), "");

  return (
    <div
      className={`grid min-h-0 flex-1 grid-rows-[minmax(180px,1fr)_auto] gap-x-3 sm:grid-rows-[minmax(var(--chart-h),1fr)_auto] ${empty ? "grid-cols-1" : "grid-cols-[auto_minmax(0,1fr)]"}`}
      style={{ "--chart-h": `${height}px` } as CSSProperties}
    >
      {!empty && (
        <div className="relative font-mono text-[11px] text-stone-400">
          <span aria-hidden="true" className="invisible whitespace-nowrap">
            <span className="sm:hidden">{widest("short")}</span>
            <span className="hidden sm:inline">{widest("label")}</span>
          </span>
          {ticks.map((t) => (
            <span
              key={`${t.pct}-${t.label}`}
              className="absolute right-0 -translate-y-1/2 whitespace-nowrap"
              style={{ top: `${t.pct}%` }}
            >
              <span className="sm:hidden">{t.short}</span>
              <span className="hidden sm:inline">{t.label}</span>
            </span>
          ))}
        </div>
      )}
      <div className={`relative min-w-0 border-b border-stone-700 ${empty ? "" : "border-l"}`}>
        {!empty &&
          ticks
            .filter((t) => t.pct < 100)
            .map((t) => (
              <div
                key={`${t.pct}-${t.label}`}
                className="absolute inset-x-0 border-t border-dashed border-stone-800"
                style={{ top: `${t.pct}%` }}
              />
            ))}
        {shaded && (
          <div
            aria-hidden="true"
            className="absolute inset-y-0 left-0 bg-stone-400/[0.06]"
            style={{ width: pctX(shaded.x + shaded.w) }}
          />
        )}
        <svg
          role="img"
          aria-label={label}
          viewBox={`0 0 ${PLOT_W} ${PLOT_H}`}
          preserveAspectRatio="none"
          className="absolute inset-0 h-full w-full overflow-visible"
        >
          {children}
        </svg>
        {halves && !empty && (
          <>
            <span className="pointer-events-none absolute top-1 left-2 text-[11px] text-stone-500">
              {halves.up}
            </span>
            <span className="pointer-events-none absolute bottom-1 left-2 text-[11px] text-stone-500">
              {halves.down}
            </span>
          </>
        )}
        {columns.map((c, i) => (
          <button
            key={c.x}
            type="button"
            aria-label={tip(i)}
            onClick={() => setActive(i)}
            onFocus={() => setActive(i)}
            onBlur={() => setActive(null)}
            onPointerEnter={(e) => {
              if (e.pointerType === "mouse") setActive(i);
            }}
            onPointerLeave={(e) => {
              if (e.pointerType === "mouse") setActive(null);
            }}
            className={`absolute inset-y-0 outline-none ${active === i ? "bg-white/5" : ""}`}
            style={{ left: pctX(c.x), width: pctX(c.w) }}
          />
        ))}
        {cur && active !== null && (
          <div
            aria-hidden="true"
            className="bg-surface-overlay pointer-events-none absolute top-2 z-10 w-44 rounded-lg border border-stone-800/70 px-3 py-2 text-xs shadow-lg"
            style={{
              left: `clamp(0px, calc(${pctX(cur.x + cur.w / 2)} - 5.5rem), calc(100% - 11rem))`,
            }}
          >
            {mounted && <p className="font-mono text-stone-400">{time(active)}</p>}
            {names.map((name, k) => (
              <p key={name} className="mt-1 flex justify-between gap-3">
                <span className="text-stone-300">{name}</span>
                <span className="font-mono text-stone-100">{cur.values[k]}</span>
              </p>
            ))}
          </div>
        )}
        {empty && (
          <div className="absolute inset-0 flex items-center justify-center">
            <span className="px-2 text-sm text-stone-400">{empty}</span>
          </div>
        )}
      </div>
      <div className={`mt-2 font-mono text-[11px] text-stone-400 ${empty ? "" : "col-start-2"}`}>
        <div className="flex h-4 items-end justify-between sm:hidden">
          {mounted && timeLabels(hours, now, 4).map((t) => <span key={t.key}>{t.label}</span>)}
        </div>
        <div className="hidden h-4 items-end justify-between sm:flex">
          {mounted && timeLabels(hours, now).map((t) => <span key={t.key}>{t.label}</span>)}
        </div>
      </div>
    </div>
  );
}

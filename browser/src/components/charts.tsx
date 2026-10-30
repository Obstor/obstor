import { useId } from "react";
import {
  byteTicks,
  type Capacity,
  collectingFrom,
  countTicks,
  gaugeArc,
  OPS_FLOOR,
  PLOT_H,
  PLOT_W,
  plotScale,
  type Tick,
  traceY,
} from "@/lib/chart";
import { humanSize } from "@/lib/format";
import { COUNTRIES, WORLD_VIEWBOX } from "@/lib/worldmap";
import { ChartFrame } from "./ChartFrame";
import { LocalTime } from "./LocalTime";

export interface Series {
  name: string;
  data: number[];
  color: string;
}

const perHour = (v: number) => `${Number(v.toFixed(1)).toLocaleString("en-US")}/h`;

// Shade hours before first traffic report
function CollectingNote({ since, hours }: { since: string; hours: number }) {
  return (
    <p className="mt-2 flex shrink-0 items-center gap-2 text-[12px] text-stone-400">
      <span>
        Collecting since <LocalTime iso={since} format={hours > 24 ? "stamp" : "clock"} />
      </span>
    </p>
  );
}

export function AreaChart({
  series,
  hours,
  kind,
  empty,
  label,
  height = 170,
  since = "",
}: {
  series: Series[];
  hours: number;
  kind: "bytes" | "count";
  empty: string;
  label: string;
  height?: number;
  since?: string;
}) {
  const now = Date.now();
  const fillId = `area-${useId().replace(/[^\w-]/g, "")}`;
  const { mode, domain } = plotScale(
    series.map((s) => s.data),
    kind === "bytes" ? 0 : OPS_FLOOR,
  );
  const ticks: Tick[] = kind === "bytes" ? byteTicks(domain) : countTicks(domain);
  const top = ticks[ticks.length - 1]?.value || domain;
  const n = Math.max(0, ...series.map((s) => s.data.length));
  const step = PLOT_W / Math.max(n - 1, 1);
  const x = (i: number) => i * step;
  const y = (v: number) => PLOT_H - (v / top) * PLOT_H;
  const fmt = (v: number) => (kind === "bytes" ? `${humanSize(v)}/h` : perHour(v));
  const columns =
    mode === "empty"
      ? []
      : Array.from({ length: n }, (_, i) => {
          const x0 = Math.max(0, x(i) - step / 2);
          return {
            x: x0,
            w: Math.min(PLOT_W, x(i) + step / 2) - x0,
            values: series.map((s) => fmt(s.data[i] ?? 0)),
          };
        });
  const shade = collectingFrom(since, hours, n, now);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <ChartFrame
        ticks={ticks.map((t) => ({
          pct: 100 - (t.value / top) * 100,
          label: t.label,
          short: t.short,
        }))}
        columns={columns}
        names={series.map((s) => s.name)}
        hours={hours}
        now={now}
        height={height}
        empty={mode === "empty" ? empty : undefined}
        label={label}
        shade={shade}
      >
        {mode === "data" &&
          series
            .filter((s) => s.data.length > 1)
            .map((s, k) => {
              const pts = s.data.map((v, i) => `${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(" ");
              return (
                <g key={s.name}>
                  <defs>
                    <linearGradient id={`${fillId}-${k}`} x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0" stopColor={s.color} stopOpacity={0.35} />
                      <stop offset="1" stopColor={s.color} stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <polygon
                    points={`0,${PLOT_H} ${pts} ${x(s.data.length - 1).toFixed(1)},${PLOT_H}`}
                    fill={`url(#${fillId}-${k})`}
                  />
                  <polyline
                    points={pts}
                    fill="none"
                    stroke={s.color}
                    strokeWidth={1.5}
                    strokeLinejoin="round"
                    vectorEffect="non-scaling-stroke"
                  />
                </g>
              );
            })}
        {mode === "trace" &&
          series.map((s) => (
            <path
              key={s.name}
              d={s.data
                .map((v, i) =>
                  v > 0 ? `M${x(i).toFixed(1)} ${PLOT_H}V${traceY(v, top).toFixed(1)}` : "",
                )
                .join("")}
              stroke={s.color}
              strokeWidth={2}
              vectorEffect="non-scaling-stroke"
            />
          ))}
      </ChartFrame>
      {shade > 0 && <CollectingNote since={since} hours={hours} />}
    </div>
  );
}

// Reads above center, writes below
export function DivergingBars({
  up,
  down,
  upName,
  downName,
  upColor,
  downColor,
  hours,
  empty,
  label,
  height = 170,
  since = "",
}: {
  up: number[];
  down: number[];
  upName: string;
  downName: string;
  upColor: string;
  downColor: string;
  hours: number;
  empty: string;
  label: string;
  height?: number;
  since?: string;
}) {
  const now = Date.now();
  const { mode, domain } = plotScale([up, down], OPS_FLOOR);
  const ticks = countTicks(domain);
  const peak = ticks[ticks.length - 1];
  const top = peak?.value || domain;
  const n = Math.max(up.length, down.length);
  const bw = PLOT_W / Math.max(n, 1);
  const mid = PLOT_H / 2;
  const bars = (values: number[], dir: 1 | -1) =>
    values
      .map((v, i) => {
        const h = (v / top) * (mid - 2);
        return h > 0
          ? `M${(i * bw + 1).toFixed(1)} ${mid}v${(-dir * h).toFixed(1)}h${Math.max(bw - 2, 1).toFixed(1)}v${(dir * h).toFixed(1)}Z`
          : "";
      })
      .join("");
  const columns =
    mode === "empty"
      ? []
      : Array.from({ length: n }, (_, i) => ({
          x: i * bw,
          w: bw,
          values: [perHour(up[i] ?? 0), perHour(down[i] ?? 0)],
        }));
  const shade = collectingFrom(since, hours, n, now);

  return (
    <div>
      <ChartFrame
        ticks={[
          { pct: 0, label: peak?.label ?? "", short: peak?.short ?? "" },
          { pct: 50, label: "0", short: "0" },
          { pct: 100, label: peak?.label ?? "", short: peak?.short ?? "" },
        ]}
        columns={columns}
        names={[upName, downName]}
        hours={hours}
        now={now}
        height={height}
        empty={mode === "empty" ? empty : undefined}
        label={label}
        shade={shade}
        halves={{ up: upName, down: downName }}
      >
        {mode !== "empty" && (
          <>
            <path d={bars(up, 1)} fill={upColor} />
            <path d={bars(down, -1)} fill={downColor} />
          </>
        )}
      </ChartFrame>
      {shade > 0 && <CollectingNote since={since} hours={hours} />}
    </div>
  );
}

// Gauge chart for used vs free disk
export function Gauge({ capacity, size = 152 }: { capacity: Capacity; size?: number }) {
  const stroke = 16;
  const pct = Math.round(capacity.pct * 100);
  const arcs = [
    { d: gaugeArc(0, 1, size, stroke), color: "var(--color-stone-800)" },
    { d: gaugeArc(0, capacity.pct, size, stroke), color: "var(--color-amber-500)" },
  ].filter((a) => a.d);
  return (
    <div
      className="relative shrink-0"
      style={{ width: size, height: size }}
      title={`${pct}% of raw drive space is in use.`}
    >
      <svg
        role="img"
        aria-label={`${pct}% of raw drive space used`}
        width={size}
        height={size}
        viewBox={`0 0 ${size} ${size}`}
      >
        {arcs.map((a) => (
          <path key={a.color} d={a.d} fill="none" stroke={a.color} strokeWidth={stroke} />
        ))}
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        <span className="font-display text-3xl font-bold">{pct}%</span>
        <span className="text-xs text-stone-400">Drive used</span>
      </div>
    </div>
  );
}

export function Legend({
  items,
  className = "mb-4",
}: {
  items: { color: string; label: string; value: string }[];
  className?: string;
}) {
  return (
    <div className={`flex shrink-0 flex-wrap items-center gap-y-1 text-sm ${className}`}>
      {items.map((it, i) => (
        <span
          key={it.label}
          className={`flex items-center gap-2 ${i > 0 ? "ml-4 border-l border-stone-800 pl-4" : ""}`}
        >
          <span className={`h-2.5 w-2.5 rounded-full ${it.color}`} />
          <span className="text-stone-400">{it.label}</span>
          <span className="font-mono text-stone-100">{it.value}</span>
        </span>
      ))}
    </div>
  );
}

export function UsageBar({ pct }: { pct: number }) {
  const v = Number.isFinite(pct) ? Math.min(Math.max(pct, 0), 100) : 0;
  return (
    <div className="h-2 min-w-16 flex-1 overflow-hidden rounded-full bg-stone-800">
      <div
        className="h-full rounded-full bg-amber-500"
        style={{ width: `${v >= 0.5 ? Math.max(v, 2) : v}%` }}
      />
    </div>
  );
}

export function RegionRow({
  region,
  used,
  total,
  free,
}: {
  region: string;
  used: number;
  total: number;
  free?: number;
}) {
  const inUse =
    free !== undefined && total > 0 ? ((total - Math.min(free, total)) / total) * 100 : null;
  return (
    <div className="flex items-center gap-3 text-sm">
      <span className="icon-[tabler--world] shrink-0 text-lg text-stone-400" />
      <span className="w-28 shrink-0 truncate font-mono">{region}</span>
      <span
        className="shrink-0 text-stone-400"
        title="Bytes stored by objects in this region, before parity"
      >
        Stored <span className="font-mono text-stone-200">{humanSize(used)}</span>
      </span>
      {inUse !== null && (
        <span
          className="flex min-w-16 flex-1"
          title={`${Math.round(inUse)}% of this region's raw drive space is in use`}
        >
          <UsageBar pct={inUse} />
        </span>
      )}
    </div>
  );
}

// Map fill by magnitude
export function shareOpacity(pct: number): number {
  return pct >= 75 ? 0.9 : pct >= 50 ? 0.65 : pct >= 25 ? 0.4 : 0.2;
}

export function WorldHeatmap({
  usedByCountry,
  regionsUsed,
}: {
  usedByCountry: Map<string, number>;
  regionsUsed: number;
}) {
  return (
    <svg aria-hidden="true" width="100%" viewBox={WORLD_VIEWBOX} className="h-auto w-full">
      {COUNTRIES.map(([id, d]) => {
        const bytes = usedByCountry.get(id);
        const share = bytes !== undefined && regionsUsed > 0 ? (bytes / regionsUsed) * 100 : 0;
        return (
          <path
            key={id}
            d={d}
            fill={bytes === undefined ? "var(--color-surface-overlay)" : "var(--color-amber-500)"}
            fillOpacity={bytes === undefined ? 1 : shareOpacity(share)}
            stroke="var(--color-abyss)"
            strokeWidth={0.5}
          />
        );
      })}
    </svg>
  );
}

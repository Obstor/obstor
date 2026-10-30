import type { ReactNode } from "react";
import { Sparkline, SparkPlaceholder } from "@/components/Sparkline";
import { sparkValues } from "@/lib/chart";
import type { CapacityHistory } from "@/lib/fleet";

export function Card({
  title,
  icon,
  action,
  flush = false,
  className = "",
  bodyClassName = "",
  children,
}: {
  title: string;
  icon: string;
  action?: ReactNode;
  flush?: boolean; // Unpadded body for tables
  className?: string;
  bodyClassName?: string;
  children: ReactNode;
}) {
  return (
    <section className={`bg-abyss flex min-w-0 flex-col rounded-xl ${className}`}>
      <header className="flex items-center justify-between gap-3 px-5 pt-5 pb-4">
        <h2 className="font-display flex items-center gap-2.5 text-lg font-semibold">
          <span className={`${icon} text-xl text-amber-500`} />
          {title}
        </h2>
        {action}
      </header>
      <div className={`flex-1 ${flush ? "" : "px-5 pb-5"} ${bodyClassName}`}>{children}</div>
    </section>
  );
}

const PARTIAL_NOTE = " (partial: buckets past the first 1000 are not tracked)";

// History to live, placeholder for low data
export function cardSparkline(
  hist: CapacityHistory | null,
  label: string,
  series: number[] | null | undefined,
  live: number | null,
  kind: "bytes" | "count",
) {
  if (!hist || !series || series.length < 2) return undefined;
  return (
    <Sparkline
      values={sparkValues(series, live)}
      label={`${label}${hist.partial ? PARTIAL_NOTE : ""}`}
      since={hist.since}
      step={hist.step}
      kind={kind}
    />
  );
}

// Value hover text, one detail per line
export function hoverLines(...lines: (string | false | null | undefined)[]): string {
  return lines.filter(Boolean).join("\n");
}

// Label beside icon, value and chart below
export function StatCard({
  icon,
  label,
  value,
  title,
  aside,
}: {
  icon: string;
  label: string;
  value: string;
  title?: string; // Figure details shown on hover
  aside?: ReactNode; // Chart slot, dashed baseline when absent
}) {
  return (
    <div className="bg-abyss @container min-w-0 rounded-xl p-4 sm:p-5">
      <div className="flex h-full items-start gap-2 @[24rem]:gap-4">
        <span className={`${icon} shrink-0 text-2xl text-amber-500`} />
        <div className="min-w-0 flex-1">
          <p className="font-body flex h-6 items-center text-[12px] font-medium tracking-[0.08em] whitespace-nowrap text-stone-400 uppercase">
            {label}
          </p>
          <div className="mt-1.5 flex items-end gap-3">
            <p
              title={title}
              className="font-display shrink-0 truncate text-[22px] leading-none font-bold @[13rem]:text-[26px] @[24rem]:text-[28px]"
            >
              {value}
            </p>
            <div className="ml-auto flex w-[45%] min-w-0 items-center">
              {aside ?? <SparkPlaceholder />}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

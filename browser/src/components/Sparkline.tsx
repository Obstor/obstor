"use client";

import { useEffect, useId, useState } from "react";
import { sparkPath, sparkTimes } from "@/lib/chart";
import { formatSparkTime, humanCount, humanSize } from "@/lib/format";

const W = 120;
const H = 40;

// Baseline for no history
export function SparkPlaceholder() {
  return (
    <div aria-hidden="true" className="flex h-8 w-full items-end sm:h-10">
      <div className="w-full border-t border-dashed border-stone-700" />
    </div>
  );
}

// Local point titles render after mount
export function Sparkline({
  values,
  label,
  since,
  step,
  kind,
}: {
  values: number[];
  label: string;
  since: string;
  step: number;
  kind: "bytes" | "count";
}) {
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  const fillId = `spark-${useId().replace(/[^\w-]/g, "")}`;
  const path = sparkPath(values, W, H);
  if (!path) return <SparkPlaceholder />;
  const times = sparkTimes(since, step, values.length);
  const gap = W / (values.length - 1);
  const fmt = kind === "bytes" ? humanSize : humanCount;
  const cells = values.map((v, i) => {
    const x = Math.max(0, (i - 0.5) * gap);
    const end = Math.min(W, (i + 0.5) * gap);
    const at = times[i];
    return { x, width: end - x, title: at ? `${fmt(v)} at ${formatSparkTime(at, step)}` : fmt(v) };
  });
  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio="none"
      aria-hidden="true"
      className="block h-8 w-full sm:h-10"
    >
      <title>{label}</title>
      <defs>
        <linearGradient id={fillId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="var(--color-amber-500)" stopOpacity={0.35} />
          <stop offset="1" stopColor="var(--color-amber-500)" stopOpacity={0} />
        </linearGradient>
      </defs>
      <path d={path.area} fill={`url(#${fillId})`} />
      <path
        d={path.line}
        fill="none"
        stroke="var(--color-amber-500)"
        strokeWidth={1.5}
        vectorEffect="non-scaling-stroke"
      />
      {cells.map((c) => (
        <rect key={c.x} x={c.x} y={0} width={c.width} height={H} fill="transparent">
          {mounted && <title>{c.title}</title>}
        </rect>
      ))}
    </svg>
  );
}

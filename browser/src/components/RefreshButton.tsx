"use client";

import { type ReactNode, useState, useTransition } from "react";

export function RefreshButton({
  onRefresh,
  label = "Refresh",
  className,
  children,
}: {
  onRefresh: () => void;
  label?: string;
  className?: string;
  children?: ReactNode;
}) {
  const [turns, setTurns] = useState(0);
  const [pending, startTransition] = useTransition();

  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      aria-busy={pending}
      disabled={pending}
      onClick={() => {
        setTurns((current) => current + 1);
        startTransition(onRefresh);
      }}
      className={
        className ??
        "bg-surface hover:bg-surface-overlay inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-stone-800 text-lg text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
      }
    >
      <span
        aria-hidden="true"
        className={`icon-[tabler--refresh] transition-transform duration-500 ease-in-out motion-reduce:transition-none ${children ? "text-base" : ""}`}
        style={{ transform: `rotate(${turns * 360}deg)` }}
      />
      {children}
    </button>
  );
}

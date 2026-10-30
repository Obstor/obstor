"use client";

import { useSyncExternalStore } from "react";
import { CopyButton } from "./CopyButton";

const subscribe = () => () => {};

// Browser console address, available after mount
export function ConsoleAddress() {
  const origin = useSyncExternalStore(
    subscribe,
    () => window.location.origin,
    () => "",
  );
  if (!origin) return null;
  return (
    <span className="flex min-w-0 items-center gap-1">
      <span className="min-w-0 font-mono break-all">{origin}</span>
      <CopyButton value={origin} label="Copy console address" />
    </span>
  );
}

"use client";

import { useState } from "react";

export function CopyButton({ value, label = "Copy" }: { value: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        } catch {
          // clipboard denied: the value stays selectable on screen
        }
      }}
      className="hover:bg-surface inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-base text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
    >
      <span className={copied ? "icon-[tabler--check] text-green-500" : "icon-[tabler--copy]"} />
    </button>
  );
}

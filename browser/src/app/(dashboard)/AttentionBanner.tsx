"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useAttention } from "@/components/AttentionProvider";
import { BELL_MENU_ID } from "@/components/Bell";
import { bannerState } from "@/lib/attention";
import { safeDisplayName } from "@/lib/safe-name";

// Displays critical announcements
export function AttentionBanner() {
  const state = bannerState(useAttention().items);
  const key = state?.key ?? "";
  // One key dismiss as read
  const [read, setRead] = useState<{ key: string; dismissed: boolean } | null>(null);

  useEffect(() => {
    if (!key) return;
    let dismissed = false;
    try {
      dismissed = sessionStorage.getItem(key) === "1";
    } catch {
      // storage blocked, banner shows on every visit
    }
    setRead({ key, dismissed });
  }, [key]);

  if (!state || !read || read.key !== state.key || read.dismissed) return null;
  const { first, more } = state;
  const dismiss = () => {
    setRead({ key: state.key, dismissed: true });
    try {
      sessionStorage.setItem(state.key, "1");
    } catch {
      // storage blocked, dismiss lasts until page reloads
    }
  };

  return (
    <div
      role="status"
      className="flex min-h-11 flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-amber-500/40 bg-amber-500/5 px-4 py-1.5 text-sm"
    >
      <span className="icon-[tabler--alert-triangle] shrink-0 text-lg text-amber-500" />
      <span className="min-w-0 font-semibold wrap-anywhere text-amber-400">
        <bdi>{safeDisplayName(first.title)}</bdi>
      </span>
      {(first.href || more > 0) && (
        <span aria-hidden="true" className="h-4 border-l border-amber-500/30" />
      )}
      {first.href && (
        <Link
          href={first.href}
          className="inline-flex items-center gap-1.5 text-amber-400 hover:text-amber-300"
        >
          Review
          <span className="icon-[tabler--arrow-right]" />
        </Link>
      )}
      {more > 0 && (
        <button
          type="button"
          onClick={() => document.getElementById(BELL_MENU_ID)?.showPopover()}
          className="text-amber-400 hover:text-amber-300"
        >
          {more} more
        </button>
      )}
      <button
        type="button"
        aria-label="Dismiss"
        title="Dismiss"
        onClick={dismiss}
        className="hover:bg-surface ml-auto inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-lg text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
      >
        <span className="icon-[tabler--x]" />
      </button>
    </div>
  );
}

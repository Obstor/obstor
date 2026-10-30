"use client";

import { type ReactNode, useRef } from "react";
import { BUCKET_TABS, type BucketTab, rovingIndex, tabHref } from "@/lib/bucket-tabs";
import { useLeaveGuard } from "./LeaveGuard";

const ICONS: Record<BucketTab, string> = {
  objects: "icon-[tabler--files]",
  settings: "icon-[tabler--settings]",
  access: "icon-[tabler--shield]",
  activity: "icon-[tabler--activity]",
};

// Arrow keys, enter, and space selection
export function BucketTabs({
  bucket,
  prefix,
  tab,
  children,
}: {
  bucket: string;
  prefix: string;
  tab: BucketTab;
  children: ReactNode;
}) {
  const { navigate } = useLeaveGuard();
  const refs = useRef<(HTMLButtonElement | null)[]>([]);

  return (
    <>
      <div className="shrink-0 border-b border-stone-800/70">
        <div
          role="tablist"
          aria-label="Bucket sections"
          className="-mb-px flex gap-6 overflow-x-auto"
        >
          {BUCKET_TABS.map((t, i) => {
            const selected = t.id === tab;
            return (
              <button
                key={t.id}
                ref={(el) => {
                  refs.current[i] = el;
                }}
                type="button"
                role="tab"
                id={`bucket-tab-${t.id}`}
                aria-selected={selected}
                aria-controls="bucket-tabpanel"
                tabIndex={selected ? 0 : -1}
                onClick={() => {
                  if (!selected) navigate(tabHref(bucket, t.id, prefix));
                }}
                onKeyDown={(e) => {
                  const next = rovingIndex(e.key, i, BUCKET_TABS.length);
                  if (next === null) return;
                  e.preventDefault();
                  refs.current[next]?.focus();
                }}
                className={`flex items-center gap-2 border-b-2 px-1 pb-3 text-[15px] font-medium whitespace-nowrap transition-colors focus-visible:-outline-offset-2! ${
                  selected
                    ? "border-amber-500 text-stone-100"
                    : "border-transparent text-stone-400 hover:text-stone-200"
                }`}
              >
                <span className={`${ICONS[t.id]} text-lg max-sm:hidden`} />
                {t.label}
              </button>
            );
          })}
        </div>
      </div>
      <div
        role="tabpanel"
        id="bucket-tabpanel"
        aria-labelledby={`bucket-tab-${tab}`}
        className={tab === "objects" ? "flex min-h-0 flex-1 flex-col gap-4" : undefined}
      >
        {children}
      </div>
    </>
  );
}

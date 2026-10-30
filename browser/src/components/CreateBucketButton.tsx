"use client";

import { useBuckets } from "./BucketProvider";

export function CreateBucketButton({
  className = "inline-flex h-9 shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-lg px-3.5 font-body text-sm transition-colors disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 bg-amber-500 font-semibold text-black hover:bg-amber-400",
}: {
  className?: string;
}) {
  const { openCreate } = useBuckets();
  return (
    <button type="button" onClick={openCreate} className={className}>
      <span className="icon-[tabler--plus] text-base" />
      Create bucket
    </button>
  );
}
